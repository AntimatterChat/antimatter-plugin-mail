// Copyright (c) 2026-present Antimatter contributors.
// See LICENSE.txt for license information.

package main

import (
	"bytes"
	"crypto/tls"
	"io"
	"net"
	"net/http"
	gomail "net/mail"
	"strconv"
	"strings"
	"time"

	"github.com/emersion/go-message/mail"
	"github.com/emersion/go-sasl"
	"github.com/emersion/go-smtp"
	"github.com/pkg/errors"
)

const (
	maxRecipients  = 100
	maxSubjectSize = 998
	maxBodySize    = 1 << 20
	smtpTimeout    = time.Minute
)

// OutgoingMessage is a message the user writes.
type OutgoingMessage struct {
	To      []Address `json:"to"`
	Cc      []Address `json:"cc"`
	Bcc     []Address `json:"bcc"`
	Subject string    `json:"subject"`
	Text    string    `json:"text"`

	// InReplyTo is the Message-ID of the message replied to, if any.
	InReplyTo string `json:"in_reply_to"`
	// References are the Message-IDs of the thread, if any.
	References []string `json:"references"`

	// MessageID identifies a draft across its saves. Sending the message deletes the draft.
	MessageID string `json:"message_id,omitempty"`
}

// cleanHeader removes the line breaks of a header value.
func cleanHeader(s string) string {
	return strings.TrimSpace(strings.NewReplacer("\r", " ", "\n", " ").Replace(s))
}

func toMailAddresses(list []Address) ([]*mail.Address, error) {
	addresses := make([]*mail.Address, 0, len(list))
	for _, a := range list {
		parsed, err := gomail.ParseAddress(strings.TrimSpace(a.Email))
		if err != nil {
			return nil, newAPIError(http.StatusBadRequest, "invalid email address: "+a.Email)
		}
		// Addresses typed as "Name <address>" keep their name
		name := a.Name
		if name == "" {
			name = parsed.Name
		}
		addresses = append(addresses, &mail.Address{Name: cleanHeader(name), Address: parsed.Address})
	}
	return addresses, nil
}

func validMessageID(id string) bool {
	return id != "" && len(id) < 250 && !strings.ContainsAny(id, "<> \r\n\t")
}

// Recipients returns the addresses the message goes to.
func (m *OutgoingMessage) Recipients() ([]string, error) {
	var recipients []string
	for _, list := range [][]Address{m.To, m.Cc, m.Bcc} {
		addresses, err := toMailAddresses(list)
		if err != nil {
			return nil, err
		}
		for _, a := range addresses {
			recipients = append(recipients, a.Address)
		}
	}
	return recipients, nil
}

// buildMessage writes a message in the Internet Message Format, returning it with its Message-ID.
// Bcc recipients are only written in drafts, which keep the Message-ID they were given and can
// have no recipient.
func buildMessage(account *Account, msg *OutgoingMessage, now time.Time, draft bool) ([]byte, string, error) {
	count := len(msg.To) + len(msg.Cc) + len(msg.Bcc)
	if count == 0 && !draft {
		return nil, "", newAPIError(http.StatusBadRequest, "add at least one recipient")
	}
	if count > maxRecipients {
		return nil, "", newAPIError(http.StatusBadRequest, "too many recipients")
	}
	if len(msg.Subject) > maxSubjectSize {
		return nil, "", newAPIError(http.StatusBadRequest, "the subject is too long")
	}
	if len(msg.Text) > maxBodySize {
		return nil, "", newAPIError(http.StatusBadRequest, "the message is too long")
	}

	to, err := toMailAddresses(msg.To)
	if err != nil {
		return nil, "", err
	}
	cc, err := toMailAddresses(msg.Cc)
	if err != nil {
		return nil, "", err
	}
	bcc, err := toMailAddresses(msg.Bcc)
	if err != nil {
		return nil, "", err
	}

	var header mail.Header
	header.SetDate(now)
	header.SetAddressList("From", []*mail.Address{{Name: cleanHeader(account.Name), Address: account.Email}})
	if len(to) > 0 {
		header.SetAddressList("To", to)
	}
	if len(cc) > 0 {
		header.SetAddressList("Cc", cc)
	}
	if len(bcc) > 0 && draft {
		header.SetAddressList("Bcc", bcc)
	}
	header.SetSubject(cleanHeader(msg.Subject))

	if draft && validMessageID(msg.MessageID) {
		header.SetMessageID(msg.MessageID)
	} else {
		domain := account.Email[strings.LastIndex(account.Email, "@")+1:]
		if err := header.GenerateMessageIDWithHostname(domain); err != nil {
			return nil, "", errors.Wrap(err, "failed to generate a message ID")
		}
	}
	messageID, _ := header.MessageID()
	if validMessageID(msg.InReplyTo) {
		header.SetMsgIDList("In-Reply-To", []string{msg.InReplyTo})
	}
	var references []string
	for _, id := range msg.References {
		if validMessageID(id) {
			references = append(references, id)
		}
	}
	if len(references) > 0 {
		header.SetMsgIDList("References", references)
	}
	header.Set("User-Agent", "Antimatter Mail")
	header.SetContentType("text/plain", map[string]string{"charset": "utf-8"})

	var buf bytes.Buffer
	w, err := mail.CreateSingleInlineWriter(&buf, header)
	if err != nil {
		return nil, "", errors.Wrap(err, "failed to write the message")
	}
	text := strings.ReplaceAll(strings.ReplaceAll(msg.Text, "\r\n", "\n"), "\n", "\r\n")
	if _, err := io.WriteString(w, text); err != nil {
		return nil, "", errors.Wrap(err, "failed to write the message")
	}
	if err := w.Close(); err != nil {
		return nil, "", errors.Wrap(err, "failed to write the message")
	}
	return buf.Bytes(), messageID, nil
}

// smtpOptions say how to connect to SMTP servers.
type smtpOptions struct {
	dialer        *net.Dialer
	allowInsecure bool
	// tlsConfig is nil for the default TLS configuration (tests set their own CA).
	tlsConfig *tls.Config
}

func dialSMTP(settings ServerSettings, creds credentials, opts smtpOptions) (*smtp.Client, error) {
	address := net.JoinHostPort(settings.Host, strconv.Itoa(settings.Port))
	tlsConfig := opts.tlsConfig
	if tlsConfig == nil {
		tlsConfig = &tls.Config{ServerName: settings.Host, MinVersion: tls.VersionTLS12}
	}

	var client *smtp.Client
	switch settings.Security {
	case SecurityTLS:
		conn, err := tls.DialWithDialer(opts.dialer, "tcp", address, tlsConfig)
		if err != nil {
			return nil, connectionError("SMTP", err)
		}
		client = smtp.NewClient(conn)
	case SecurityStartTLS:
		conn, err := opts.dialer.Dial("tcp", address)
		if err != nil {
			return nil, connectionError("SMTP", err)
		}
		if client, err = smtp.NewClientStartTLS(conn, tlsConfig); err != nil {
			return nil, connectionError("SMTP", err)
		}
	case SecurityNone:
		if !opts.allowInsecure {
			return nil, newAPIError(http.StatusForbidden, "unencrypted connections to mail servers aren't allowed on this server")
		}
		conn, err := opts.dialer.Dial("tcp", address)
		if err != nil {
			return nil, connectionError("SMTP", err)
		}
		client = smtp.NewClient(conn)
	default:
		return nil, newAPIError(http.StatusBadRequest, "invalid connection security")
	}
	client.CommandTimeout = smtpTimeout
	client.SubmissionTimeout = smtpTimeout

	var auth sasl.Client
	switch {
	case creds.token != "":
		if auth = oauthClient(settings.Username, creds.token, client.SupportsAuth); auth == nil {
			_ = client.Close()
			return nil, newAPIError(http.StatusBadGateway, "the SMTP server doesn't offer OAuth sign in")
		}
	case client.SupportsAuth(sasl.Plain):
		auth = sasl.NewPlainClient("", settings.Username, creds.password)
	case client.SupportsAuth(sasl.Login):
		auth = sasl.NewLoginClient(settings.Username, creds.password)
	default:
		_ = client.Close()
		return nil, newAPIError(http.StatusBadRequest, "the SMTP server doesn't offer password authentication")
	}
	if err := client.Auth(auth); err != nil {
		_ = client.Close()
		var smtpErr *smtp.SMTPError
		if errors.As(err, &smtpErr) {
			return nil, &apiError{status: http.StatusBadRequest, err: errors.Errorf("the SMTP server refused to log in: %s", smtpErr.Message)}
		}
		return nil, connectionError("SMTP", err)
	}
	return client, nil
}

// checkSMTP checks that the SMTP server accepts the account's credentials.
func checkSMTP(account *Account, creds credentials, opts smtpOptions) error {
	client, err := dialSMTP(account.SMTP, creds, opts)
	if err != nil {
		return err
	}
	_ = client.Quit()
	return nil
}

// sendSMTP sends a message.
func sendSMTP(account *Account, creds credentials, recipients []string, raw []byte, opts smtpOptions) error {
	client, err := dialSMTP(account.SMTP, creds, opts)
	if err != nil {
		return err
	}
	defer client.Close()

	if err := client.SendMail(account.Email, recipients, bytes.NewReader(raw)); err != nil {
		var smtpErr *smtp.SMTPError
		if errors.As(err, &smtpErr) {
			return &apiError{status: http.StatusBadRequest, err: errors.Errorf("the SMTP server refused the message: %s", smtpErr.Message)}
		}
		return connectionError("SMTP", err)
	}
	_ = client.Quit()
	return nil
}
