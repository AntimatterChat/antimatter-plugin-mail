// Copyright (c) 2026-present Antimatter contributors.
// See LICENSE.txt for license information.

package main

import (
	"bytes"
	"net"
	"strconv"
	"testing"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapserver"
	"github.com/emersion/go-imap/v2/imapserver/imapmemserver"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type literal struct {
	*bytes.Reader
}

func (l literal) Size() int64 {
	return l.Reader.Size()
}

// testIMAPServer is an in-memory IMAP server with one user ("ada", password "secret").
type testIMAPServer struct {
	user     *imapmemserver.User
	settings ServerSettings
}

func startIMAPServer(t *testing.T, caps imap.CapSet, folders ...string) *testIMAPServer {
	t.Helper()

	memServer := imapmemserver.New()
	user := imapmemserver.NewUser("ada", "secret")
	memServer.AddUser(user)
	for _, name := range append([]string{"INBOX"}, folders...) {
		require.NoError(t, user.Create(name, nil))
	}

	server := imapserver.New(&imapserver.Options{
		NewSession: func(*imapserver.Conn) (imapserver.Session, *imapserver.GreetingData, error) {
			return memServer.NewSession(), nil, nil
		},
		Caps:         caps,
		InsecureAuth: true,
		Logger:       discardLogger{},
	})
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() { _ = server.Close() })

	host, port, _ := net.SplitHostPort(listener.Addr().String())
	portNumber, _ := strconv.Atoi(port)
	return &testIMAPServer{
		user:     user,
		settings: ServerSettings{Host: host, Port: portNumber, Security: SecurityNone, Username: "ada"},
	}
}

type discardLogger struct{}

func (discardLogger) Printf(string, ...any) {}

func (s *testIMAPServer) add(t *testing.T, folder string, raw []byte, flags ...imap.Flag) {
	t.Helper()
	_, err := s.user.Append(folder, literal{bytes.NewReader(raw)}, &imap.AppendOptions{Flags: flags, Time: time.Now()})
	require.NoError(t, err)
}

var testIMAPOptions = imapOptions{dialer: newDialer(true), allowInsecure: true}

func (s *testIMAPServer) dial(t *testing.T) *imapSession {
	t.Helper()
	session, err := dialIMAP(s.settings, "secret", testIMAPOptions)
	require.NoError(t, err)
	t.Cleanup(session.Close)
	return session
}

func simpleMessage(subject, body string) []byte {
	return crlf("From: Marie Laurent <marie@example.com>\nTo: ada@example.com\nSubject: " + subject +
		"\nDate: Tue, 29 Sep 2026 10:31:00 +0200\nContent-Type: text/plain; charset=utf-8\n\n" + body + "\n")
}

func TestDialIMAP(t *testing.T) {
	server := startIMAPServer(t, imap.CapSet{imap.CapIMAP4rev1: {}})

	_, err := dialIMAP(server.settings, "wrong", testIMAPOptions)
	require.Error(t, err)
	assert.Equal(t, 400, statusFor(err))

	_, err = dialIMAP(server.settings, "secret", imapOptions{dialer: newDialer(true)})
	assert.Equal(t, 403, statusFor(err), "insecure connections need to be allowed")

	_, err = dialIMAP(server.settings, "secret", imapOptions{dialer: newDialer(false), allowInsecure: true})
	assert.Equal(t, 403, statusFor(err), "private networks need to be allowed")

	server.dial(t)
}

func TestListFolders(t *testing.T) {
	for name, caps := range map[string]imap.CapSet{
		"IMAP4rev1": {imap.CapIMAP4rev1: {}},
		"IMAP4rev2": {imap.CapIMAP4rev1: {}, imap.CapIMAP4rev2: {}},
	} {
		t.Run(name, func(t *testing.T) {
			server := startIMAPServer(t, caps, "Archive/2025", "Trash", "Sent Items", "Drafts", "Lab")
			server.add(t, "INBOX", simpleMessage("one", "first"))
			server.add(t, "INBOX", simpleMessage("two", "second"), imap.FlagSeen)
			server.add(t, "INBOX", simpleMessage("three", "third"))

			folders, err := server.dial(t).ListFolders()
			require.NoError(t, err)
			assert.Equal(t, []Folder{
				{Name: "INBOX", Delimiter: "/", Role: RoleInbox, Total: 3, Unread: 2},
				{Name: "Drafts", Delimiter: "/", Role: RoleDrafts},
				{Name: "Sent Items", Delimiter: "/", Role: RoleSent},
				{Name: "Trash", Delimiter: "/", Role: RoleTrash},
				{Name: "Archive/2025", Delimiter: "/"},
				{Name: "Lab", Delimiter: "/"},
			}, folders)
		})
	}
}

func TestListMessages(t *testing.T) {
	server := startIMAPServer(t, imap.CapSet{imap.CapIMAP4rev1: {}})
	for i := 1; i <= 5; i++ {
		server.add(t, "INBOX", simpleMessage("Message "+strconv.Itoa(i), "Body of message "+strconv.Itoa(i)))
	}
	server.add(t, "INBOX", multipartMessage, imap.FlagSeen, imap.FlagFlagged)
	session := server.dial(t)

	page, err := session.ListMessages("INBOX", 0, 4)
	require.NoError(t, err)
	assert.Equal(t, 6, page.Total)
	require.Len(t, page.Messages, 4)

	newest := page.Messages[0]
	assert.Equal(t, uint32(6), newest.UID)
	assert.Equal(t, "Beam schedule draft — v2", newest.Subject)
	assert.Equal(t, []Address{{Name: "Marie Laurent", Email: "marie@example.com"}}, newest.From)
	assert.False(t, newest.Unread)
	assert.True(t, newest.Flagged)
	assert.True(t, newest.HasAttachments)
	assert.Equal(t, "Attached the draft for Wed and Fri, tell me if it works — Marie", newest.Snippet)
	assert.Equal(t, int64(1790670660000), newest.Date)

	assert.Equal(t, "Message 5", page.Messages[1].Subject)
	assert.True(t, page.Messages[1].Unread)
	assert.False(t, page.Messages[1].HasAttachments)
	assert.Equal(t, "Body of message 5", page.Messages[1].Snippet)

	page, err = session.ListMessages("INBOX", 1, 4)
	require.NoError(t, err)
	require.Len(t, page.Messages, 2)
	assert.Equal(t, "Message 2", page.Messages[0].Subject)
	assert.Equal(t, "Message 1", page.Messages[1].Subject)

	page, err = session.ListMessages("INBOX", 2, 4)
	require.NoError(t, err)
	assert.Empty(t, page.Messages)

	_, err = session.ListMessages("Nope", 0, 4)
	assert.Equal(t, 404, statusFor(err))
}

func TestMessageOperations(t *testing.T) {
	server := startIMAPServer(t, imap.CapSet{imap.CapIMAP4rev1: {}, imap.CapIMAP4rev2: {}}, "Trash", "Lab", "Sent")
	server.add(t, "INBOX", multipartMessage)
	server.add(t, "INBOX", simpleMessage("Second", "body"))
	session := server.dial(t)

	data, err := session.GetMessage("INBOX", 1, 1<<20)
	require.NoError(t, err)
	assert.True(t, data.Unread)
	assert.Equal(t, multipartMessage, data.Raw)

	_, err = session.GetMessage("INBOX", 1, 100)
	assert.Equal(t, 413, statusFor(err))
	_, err = session.GetMessage("INBOX", 99, 1<<20)
	assert.Equal(t, 404, statusFor(err))

	// Reading doesn't mark as read
	unread, err := session.Unread("INBOX")
	require.NoError(t, err)
	assert.Equal(t, 2, unread)

	require.NoError(t, session.SetFlag("INBOX", []uint32{1}, imap.FlagSeen, true))
	unread, err = session.Unread("INBOX")
	require.NoError(t, err)
	assert.Equal(t, 1, unread)

	require.NoError(t, session.SetFlag("INBOX", []uint32{1}, imap.FlagSeen, false))
	unread, err = session.Unread("INBOX")
	require.NoError(t, err)
	assert.Equal(t, 2, unread)

	// Move, then delete to the trash, then delete for good
	require.NoError(t, session.Move("INBOX", []uint32{2}, "Lab"))
	page, err := session.ListMessages("Lab", 0, 10)
	require.NoError(t, err)
	require.Len(t, page.Messages, 1)
	assert.Equal(t, "Second", page.Messages[0].Subject)

	require.NoError(t, session.Delete("Lab", []uint32{page.Messages[0].UID}))
	page, err = session.ListMessages("Trash", 0, 10)
	require.NoError(t, err)
	require.Len(t, page.Messages, 1)

	require.NoError(t, session.Delete("Trash", []uint32{page.Messages[0].UID}))
	page, err = session.ListMessages("Trash", 0, 10)
	require.NoError(t, err)
	assert.Empty(t, page.Messages)

	// Store a sent message
	stored, err := session.AppendToRole(RoleSent, simpleMessage("Sent", "hello"), []imap.Flag{imap.FlagSeen})
	require.NoError(t, err)
	assert.True(t, stored)
	page, err = session.ListMessages("Sent", 0, 10)
	require.NoError(t, err)
	require.Len(t, page.Messages, 1)
	assert.False(t, page.Messages[0].Unread)

	stored, err = session.AppendToRole(RoleJunk, simpleMessage("Junk", "spam"), nil)
	require.NoError(t, err)
	assert.False(t, stored, "there is no junk folder")
}
