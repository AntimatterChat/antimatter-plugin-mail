// Copyright (c) 2026-present Antimatter contributors.
// See LICENSE.txt for license information.

package main

import (
	"errors"
	"io"
	"net"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/emersion/go-sasl"
	"github.com/emersion/go-smtp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildMessage(t *testing.T) {
	account := testAccount()
	now := time.Date(2026, 9, 30, 9, 0, 0, 0, time.UTC)

	raw, err := buildMessage(account, &OutgoingMessage{
		To:         []Address{{Name: "Marie Laurent", Email: "marie@example.com"}, {Email: "Lena <lena@example.com>"}},
		Cc:         []Address{{Email: "kenji@example.com"}},
		Bcc:        []Address{{Email: "secret@example.com"}},
		Subject:    "Re: Beam schedule\r\nBcc: injected@example.com",
		Text:       "Wednesday works — see you there.\n\n> Attached the draft",
		InReplyTo:  "draft-2@example.com",
		References: []string{"draft-1@example.com", "bad id>", "draft-2@example.com"},
	}, now)
	require.NoError(t, err)
	assert.NotContains(t, string(raw), "secret@example.com", "Bcc recipients aren't written")
	assert.NotContains(t, string(raw), "\r\nBcc:")

	msg, err := parseMessage(raw, false)
	require.NoError(t, err)
	assert.Equal(t, []Address{{Name: "Ada", Email: "ada@example.com"}}, msg.From)
	assert.Equal(t, []Address{{Name: "Marie Laurent", Email: "marie@example.com"}, {Name: "Lena", Email: "lena@example.com"}}, msg.To)
	assert.Equal(t, []Address{{Email: "kenji@example.com"}}, msg.Cc)
	assert.Equal(t, "Re: Beam schedule  Bcc: injected@example.com", msg.Subject)
	assert.Equal(t, "Wednesday works — see you there.\r\n\r\n> Attached the draft", msg.Text)
	assert.Equal(t, now.UnixMilli(), msg.Date)
	assert.Equal(t, []string{"draft-2@example.com"}, msg.InReplyTo)
	assert.Equal(t, []string{"draft-1@example.com", "draft-2@example.com"}, msg.References)
	assert.Contains(t, msg.MessageID, "@example.com")

	_, err = buildMessage(account, &OutgoingMessage{Subject: "nobody"}, now)
	assert.Equal(t, 400, statusFor(err))
	_, err = buildMessage(account, &OutgoingMessage{To: []Address{{Email: "not an address"}}}, now)
	assert.Equal(t, 400, statusFor(err))
}

// testSMTPBackend is an SMTP server that keeps the messages it receives.
type testSMTPBackend struct {
	mu       sync.Mutex
	received []receivedMail
}

type receivedMail struct {
	from string
	to   []string
	data string
}

type testSMTPSession struct {
	backend *testSMTPBackend
	authed  bool
	current receivedMail
}

func (b *testSMTPBackend) NewSession(*smtp.Conn) (smtp.Session, error) {
	return &testSMTPSession{backend: b}, nil
}

func (s *testSMTPSession) AuthMechanisms() []string {
	return []string{sasl.Plain}
}

func (s *testSMTPSession) Auth(string) (sasl.Server, error) {
	return sasl.NewPlainServer(func(_, username, password string) error {
		if username != "ada" || password != "secret" {
			return errors.New("invalid credentials")
		}
		s.authed = true
		return nil
	}), nil
}

func (s *testSMTPSession) Mail(from string, _ *smtp.MailOptions) error {
	if !s.authed {
		return smtp.ErrAuthRequired
	}
	s.current.from = from
	return nil
}

func (s *testSMTPSession) Rcpt(to string, _ *smtp.RcptOptions) error {
	s.current.to = append(s.current.to, to)
	return nil
}

func (s *testSMTPSession) Data(r io.Reader) error {
	data, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	s.current.data = string(data)
	s.backend.mu.Lock()
	s.backend.received = append(s.backend.received, s.current)
	s.backend.mu.Unlock()
	return nil
}

func (s *testSMTPSession) Reset()        { s.current = receivedMail{} }
func (s *testSMTPSession) Logout() error { return nil }

func startSMTPServer(t *testing.T) (*testSMTPBackend, ServerSettings) {
	t.Helper()
	backend := &testSMTPBackend{}
	server := smtp.NewServer(backend)
	server.AllowInsecureAuth = true
	server.Domain = "localhost"
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() { _ = server.Close() })

	host, port, _ := net.SplitHostPort(listener.Addr().String())
	portNumber, _ := strconv.Atoi(port)
	return backend, ServerSettings{Host: host, Port: portNumber, Security: SecurityNone, Username: "ada"}
}

var testSMTPOptions = smtpOptions{dialer: newDialer(true), allowInsecure: true}

func TestSendSMTP(t *testing.T) {
	backend, settings := startSMTPServer(t)
	account := testAccount()
	account.SMTP = settings
	account.SMTPPassword = "secret"

	require.NoError(t, checkSMTP(account, testSMTPOptions))
	require.NoError(t, sendSMTP(account, []string{"marie@example.com", "secret@example.com"}, crlf("Subject: hi\n\nhello\n"), testSMTPOptions))

	require.Len(t, backend.received, 1)
	assert.Equal(t, "ada@example.com", backend.received[0].from)
	assert.Equal(t, []string{"marie@example.com", "secret@example.com"}, backend.received[0].to)
	assert.Contains(t, backend.received[0].data, "hello")

	account.SMTPPassword = "wrong"
	err := checkSMTP(account, testSMTPOptions)
	require.Error(t, err)
	assert.Equal(t, 400, statusFor(err))

	account.SMTPPassword = "secret"
	err = checkSMTP(account, smtpOptions{dialer: newDialer(true)})
	assert.Equal(t, 403, statusFor(err), "insecure connections need to be allowed")
}
