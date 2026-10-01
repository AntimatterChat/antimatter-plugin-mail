// Copyright (c) 2026-present Antimatter contributors.
// See LICENSE.txt for license information.

package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/mattermost/mattermost/server/public/model"
	"github.com/mattermost/mattermost/server/public/plugin"
	"github.com/mattermost/mattermost/server/public/plugin/plugintest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

type testEnv struct {
	p    *Plugin
	api  *plugintest.API
	kv   *fakeKV
	imap *testIMAPServer
	smtp *testSMTPBackend

	userID  string
	account accountRequest
}

func newTestEnv(t *testing.T) *testEnv {
	t.Helper()

	api := &plugintest.API{}
	t.Cleanup(func() { api.AssertExpectations(t) })
	// Allow logging with any number of key/value pairs
	for _, level := range []string{"LogDebug", "LogInfo", "LogWarn", "LogError"} {
		for n := 1; n <= 7; n += 2 {
			args := make([]any, n)
			for i := range args {
				args[i] = mock.Anything
			}
			api.On(level, args...).Maybe()
		}
	}

	kv := newFakeKV()
	key, err := loadEncryptionKey(kv)
	require.NoError(t, err)
	box, err := newSecretBox(key)
	require.NoError(t, err)

	p := &Plugin{
		MattermostPlugin: plugin.MattermostPlugin{API: api},
		configuration:    &configuration{AllowPrivateNetworks: true, AllowInsecureConnections: true},
		store:            NewStore(kv, box),
		limiter:          newConnLimiter(maxConnections, maxConnectionsPerUser, time.Second),
		pool:             newIMAPPool(),
		tokens:           newTokenCache(),
		providers:        builtinProviders,
	}
	p.limiter.reclaim = p.pool.evictOldest
	t.Cleanup(p.pool.closeAll)
	p.router = p.newRouter()
	p.unread = newUnreadWatcher(p.checkUnread, p.publishUnread, func() time.Duration { return time.Minute })

	imapServer := startIMAPServer(t, imap.CapSet{imap.CapIMAP4rev1: {}, imap.CapIMAP4rev2: {}}, "Sent", "Trash", "Lab")
	smtpBackend, smtpSettings := startSMTPServer(t)

	return &testEnv{
		p:      p,
		api:    api,
		kv:     kv,
		imap:   imapServer,
		smtp:   smtpBackend,
		userID: model.NewId(),
		account: accountRequest{
			accountView:  accountView{Email: "ada@example.com", Name: "Ada", IMAP: imapServer.settings, SMTP: smtpSettings},
			IMAPPassword: "secret",
		},
	}
}

func (e *testEnv) request(t *testing.T, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()

	var reqBody []byte
	if body != nil {
		var err error
		reqBody, err = json.Marshal(body)
		require.NoError(t, err)
	}
	r := httptest.NewRequest(method, path, bytes.NewReader(reqBody))
	if e.userID != "" {
		r.Header.Set("Mattermost-User-Id", e.userID)
	}
	w := httptest.NewRecorder()
	e.p.ServeHTTP(nil, w, r)
	return w
}

func (e *testEnv) connect(t *testing.T) {
	t.Helper()
	w := e.request(t, http.MethodPut, "/api/v1/account", e.account)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
}

func decode[T any](t *testing.T, w *httptest.ResponseRecorder) T {
	t.Helper()
	var v T
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &v), w.Body.String())
	return v
}

func TestAPIRequiresUser(t *testing.T) {
	e := newTestEnv(t)
	e.userID = ""
	w := e.request(t, http.MethodGet, "/api/v1/account", nil)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestAPIAccount(t *testing.T) {
	e := newTestEnv(t)

	w := e.request(t, http.MethodGet, "/api/v1/account", nil)
	require.Equal(t, http.StatusOK, w.Code)
	assert.JSONEq(t, `{"account": null}`, w.Body.String())

	w = e.request(t, http.MethodGet, "/api/v1/folders", nil)
	assert.Equal(t, http.StatusConflict, w.Code, "there's no account yet")

	// Wrong passwords are refused
	wrong := e.account
	wrong.IMAPPassword = "wrong"
	w = e.request(t, http.MethodPut, "/api/v1/account", wrong)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "refused to log in")

	// The SMTP password and username default to the IMAP ones
	e.connect(t)
	w = e.request(t, http.MethodGet, "/api/v1/account", nil)
	require.Equal(t, http.StatusOK, w.Code)
	assert.NotContains(t, w.Body.String(), "secret")
	view := decode[map[string]*accountView](t, w)["account"]
	assert.Equal(t, e.account.accountView, *view)

	stored, err := e.p.store.GetAccount(e.userID)
	require.NoError(t, err)
	assert.Equal(t, "secret", stored.SMTPPassword)

	// Saving without passwords keeps them
	update := e.account
	update.Name = "Ada L."
	update.IMAPPassword = ""
	w = e.request(t, http.MethodPut, "/api/v1/account", update)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	stored, err = e.p.store.GetAccount(e.userID)
	require.NoError(t, err)
	assert.Equal(t, "Ada L.", stored.Name)
	assert.Equal(t, "secret", stored.IMAPPassword)

	// Unencrypted connections need an admin's approval
	e.p.configuration = &configuration{AllowPrivateNetworks: true}
	w = e.request(t, http.MethodPut, "/api/v1/account", e.account)
	assert.Equal(t, http.StatusForbidden, w.Code)
	e.p.configuration = &configuration{AllowPrivateNetworks: true, AllowInsecureConnections: true}

	w = e.request(t, http.MethodDelete, "/api/v1/account", nil)
	require.Equal(t, http.StatusOK, w.Code)
	stored, err = e.p.store.GetAccount(e.userID)
	require.NoError(t, err)
	assert.Nil(t, stored)
}

func TestAPIReadMail(t *testing.T) {
	e := newTestEnv(t)
	e.imap.add(t, "INBOX", simpleMessage("Hall B access Friday", "Badges will be re-issued."))
	e.imap.add(t, "INBOX", multipartMessage)
	e.connect(t)

	w := e.request(t, http.MethodGet, "/api/v1/folders", nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	folders := decode[[]Folder](t, w)
	require.Len(t, folders, 4)
	assert.Equal(t, Folder{Name: "INBOX", Delimiter: "/", Role: RoleInbox, Total: 2, Unread: 2}, folders[0])

	w = e.request(t, http.MethodGet, "/api/v1/unread", nil)
	require.Equal(t, http.StatusOK, w.Code)
	assert.JSONEq(t, `{"unread": 2}`, w.Body.String())

	w = e.request(t, http.MethodGet, "/api/v1/messages?folder=INBOX&per_page=1", nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	page := decode[MessagePage](t, w)
	assert.Equal(t, 2, page.Total)
	require.Len(t, page.Messages, 1)
	assert.Equal(t, "Beam schedule draft — v2", page.Messages[0].Subject)

	w = e.request(t, http.MethodGet, "/api/v1/messages", nil)
	assert.Equal(t, http.StatusBadRequest, w.Code)

	// Reading a message marks it as read when asked to
	w = e.request(t, http.MethodGet, "/api/v1/message?folder=INBOX&uid=2&mark_read=true", nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	msg := decode[FullMessage](t, w)
	assert.Equal(t, "INBOX", msg.Folder)
	assert.Equal(t, uint32(2), msg.UID)
	assert.False(t, msg.Unread)
	assert.Equal(t, 1, msg.RemoteImages)
	require.Len(t, msg.Attachments, 1)

	// The cached count is refreshed after reading
	w = e.request(t, http.MethodGet, "/api/v1/unread", nil)
	assert.JSONEq(t, `{"unread": 2}`, w.Body.String())
	e.api.On("PublishWebSocketEvent", wsEventUnread, map[string]any{"unread": 1}, &model.WebsocketBroadcast{UserId: e.userID}).Once()
	w = e.request(t, http.MethodGet, "/api/v1/unread?refresh=true", nil)
	assert.JSONEq(t, `{"unread": 1}`, w.Body.String())

	w = e.request(t, http.MethodGet, "/api/v1/message?folder=INBOX&uid=2&remote_images=true", nil)
	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, 0, decode[FullMessage](t, w).RemoteImages)

	w = e.request(t, http.MethodGet, "/api/v1/message?folder=INBOX&uid=9", nil)
	assert.Equal(t, http.StatusNotFound, w.Code)
	w = e.request(t, http.MethodGet, "/api/v1/message?folder=INBOX&uid=x", nil)
	assert.Equal(t, http.StatusBadRequest, w.Code)

	// Attachments are always downloaded
	w = e.request(t, http.MethodGet, "/api/v1/attachment?folder=INBOX&uid=2&id="+msg.Attachments[0].ID, nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Equal(t, "%PDF-1.4 fake", w.Body.String())
	assert.Equal(t, "application/octet-stream", w.Header().Get("Content-Type"))
	assert.Equal(t, `attachment; filename=schedule.pdf`, w.Header().Get("Content-Disposition"))
}

func TestAPIOrganizeMail(t *testing.T) {
	e := newTestEnv(t)
	e.imap.add(t, "INBOX", simpleMessage("one", "first"))
	e.imap.add(t, "INBOX", simpleMessage("two", "second"))
	e.connect(t)

	w := e.request(t, http.MethodPost, "/api/v1/messages/flag", messagesRequest{Folder: "INBOX", UIDs: []uint32{1, 2}, Flag: "seen", Value: true})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	w = e.request(t, http.MethodGet, "/api/v1/unread?refresh=true", nil)
	assert.JSONEq(t, `{"unread": 0}`, w.Body.String())

	w = e.request(t, http.MethodPost, "/api/v1/messages/flag", messagesRequest{Folder: "INBOX", UIDs: []uint32{1}, Flag: "deleted", Value: true})
	assert.Equal(t, http.StatusBadRequest, w.Code)
	w = e.request(t, http.MethodPost, "/api/v1/messages/flag", messagesRequest{Folder: "INBOX", Flag: "seen"})
	assert.Equal(t, http.StatusBadRequest, w.Code)

	w = e.request(t, http.MethodPost, "/api/v1/messages/move", messagesRequest{Folder: "INBOX", UIDs: []uint32{1}, Destination: "Lab"})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	w = e.request(t, http.MethodPost, "/api/v1/messages/move", messagesRequest{Folder: "INBOX", UIDs: []uint32{2}, Destination: "Nope"})
	assert.Equal(t, http.StatusBadGateway, w.Code, "the IMAP server's error is shown")

	w = e.request(t, http.MethodPost, "/api/v1/messages/delete", messagesRequest{Folder: "INBOX", UIDs: []uint32{2}})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	w = e.request(t, http.MethodGet, "/api/v1/folders", nil)
	counts := map[string]int{}
	for _, f := range decode[[]Folder](t, w) {
		counts[f.Name] = f.Total
	}
	assert.Equal(t, map[string]int{"INBOX": 0, "Lab": 1, "Trash": 1, "Sent": 0}, counts)
}

func TestAPISend(t *testing.T) {
	e := newTestEnv(t)
	e.imap.add(t, "INBOX", multipartMessage)
	e.connect(t)

	w := e.request(t, http.MethodPost, "/api/v1/send", sendRequest{
		OutgoingMessage: OutgoingMessage{
			To:        []Address{{Name: "Marie Laurent", Email: "marie@example.com"}},
			Bcc:       []Address{{Email: "kenji@example.com"}},
			Subject:   "Re: Beam schedule draft — v2",
			Text:      "Wednesday works.",
			InReplyTo: "draft-2@example.com",
		},
		ReplyTo: &messageRef{Folder: "INBOX", UID: 1},
	})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.JSONEq(t, `{"status": "ok", "saved_copy": true}`, w.Body.String())

	require.Len(t, e.smtp.received, 1)
	assert.Equal(t, []string{"marie@example.com", "kenji@example.com"}, e.smtp.received[0].to)
	assert.Contains(t, e.smtp.received[0].data, "Wednesday works.")

	session := e.imap.dial(t)
	page, err := session.ListMessages("Sent", 0, 10)
	require.NoError(t, err)
	require.Len(t, page.Messages, 1)
	assert.Equal(t, "Re: Beam schedule draft — v2", page.Messages[0].Subject)
	page, err = session.ListMessages("INBOX", 0, 10)
	require.NoError(t, err)
	assert.True(t, page.Messages[0].Answered)

	w = e.request(t, http.MethodPost, "/api/v1/send", sendRequest{OutgoingMessage: OutgoingMessage{Subject: "nobody"}})
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestAPISearch(t *testing.T) {
	e := newTestEnv(t)
	e.imap.add(t, "INBOX", multipartMessage)
	e.imap.add(t, "INBOX", simpleMessage("Lunch", "Pizza?"))
	e.connect(t)

	w := e.request(t, http.MethodGet, "/api/v1/messages?folder=INBOX&q=pizza", nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	page := decode[MessagePage](t, w)
	assert.Equal(t, 1, page.Total)
	require.Len(t, page.Messages, 1)
	assert.Equal(t, "Lunch", page.Messages[0].Subject)

	w = e.request(t, http.MethodGet, "/api/v1/messages?folder=INBOX&q="+strings.Repeat("x", maxQueryLength+1), nil)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestAPIDrafts(t *testing.T) {
	e := newTestEnv(t)
	e.connect(t)

	type savedDraft struct {
		Folder    string `json:"folder"`
		UID       uint32 `json:"uid"`
		MessageID string `json:"message_id"`
	}
	drafts := func() []MessageSummary {
		page, err := e.imap.dial(t).ListMessages("Drafts", 0, 10)
		require.NoError(t, err)
		return page.Messages
	}

	// A new draft, without recipients yet: the server has no drafts folder, it's created
	w := e.request(t, http.MethodPut, "/api/v1/draft", OutgoingMessage{Subject: "Beam", Text: "First words"})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	first := decode[savedDraft](t, w)
	assert.Equal(t, "Drafts", first.Folder)
	assert.NotEmpty(t, first.MessageID)
	require.Len(t, drafts(), 1)
	assert.True(t, drafts()[0].Draft)
	assert.False(t, drafts()[0].Unread)

	// Saving it again replaces it
	w = e.request(t, http.MethodPut, "/api/v1/draft", OutgoingMessage{
		To:        []Address{{Email: "marie@example.com"}},
		Bcc:       []Address{{Email: "kenji@example.com"}},
		Subject:   "Beam schedule",
		Text:      "More words",
		MessageID: first.MessageID,
	})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	second := decode[savedDraft](t, w)
	assert.Equal(t, first.MessageID, second.MessageID)
	assert.NotEqual(t, first.UID, second.UID)
	list := drafts()
	require.Len(t, list, 1)
	assert.Equal(t, second.UID, list[0].UID)

	// Another draft is kept apart
	w = e.request(t, http.MethodPut, "/api/v1/draft", OutgoingMessage{Text: "Other"})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	other := decode[savedDraft](t, w)
	require.Len(t, drafts(), 2)

	// Reopened with its Bcc recipients
	w = e.request(t, http.MethodGet, "/api/v1/message?folder=Drafts&uid="+strconv.Itoa(int(second.UID)), nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	msg := decode[FullMessage](t, w)
	assert.True(t, msg.Draft)
	assert.Equal(t, first.MessageID, msg.MessageID)
	assert.Equal(t, []Address{{Email: "kenji@example.com"}}, msg.Bcc)
	assert.Equal(t, "More words", msg.Text)

	// Sending it deletes it, and doesn't send the Bcc header
	w = e.request(t, http.MethodPost, "/api/v1/send", sendRequest{OutgoingMessage: OutgoingMessage{
		To:        msg.To,
		Bcc:       msg.Bcc,
		Subject:   msg.Subject,
		Text:      msg.Text,
		MessageID: msg.MessageID,
	}})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Len(t, e.smtp.received, 1)
	assert.Equal(t, []string{"marie@example.com", "kenji@example.com"}, e.smtp.received[0].to)
	assert.NotContains(t, e.smtp.received[0].data, "kenji@example.com")
	assert.NotContains(t, e.smtp.received[0].data, first.MessageID, "the sent message has its own Message-ID")
	list = drafts()
	require.Len(t, list, 1)
	assert.Equal(t, other.UID, list[0].UID)

	// Discarding a draft
	w = e.request(t, http.MethodDelete, "/api/v1/draft?message_id="+url.QueryEscape(other.MessageID), nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Empty(t, drafts())

	w = e.request(t, http.MethodDelete, "/api/v1/draft?message_id=bad%3Eid", nil)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestAPIForwardAttachments(t *testing.T) {
	e := newTestEnv(t)
	e.imap.add(t, "INBOX", multipartMessage)
	e.connect(t)
	pdf := AttachmentRef{Folder: "INBOX", UID: 1, ID: "2"}

	// Kept in drafts
	w := e.request(t, http.MethodPut, "/api/v1/draft", OutgoingMessage{Subject: "Fwd: Beam", Attachments: []AttachmentRef{pdf}})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	draft := decode[struct {
		UID         uint32       `json:"uid"`
		Attachments []Attachment `json:"attachments"`
	}](t, w)
	require.Len(t, draft.Attachments, 1)
	assert.Equal(t, "schedule.pdf", draft.Attachments[0].Filename)
	w = e.request(t, http.MethodGet, "/api/v1/message?folder=Drafts&uid="+strconv.Itoa(int(draft.UID)), nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	msg := decode[FullMessage](t, w)
	assert.Equal(t, draft.Attachments, msg.Attachments)
	assert.Equal(t, "application/pdf", msg.Attachments[0].ContentType)

	// Sent from the draft
	w = e.request(t, http.MethodPost, "/api/v1/send", sendRequest{OutgoingMessage: OutgoingMessage{
		To:          []Address{{Email: "marie@example.com"}},
		Subject:     "Fwd: Beam",
		Text:        "See attached.",
		MessageID:   msg.MessageID,
		Attachments: []AttachmentRef{{Folder: "Drafts", UID: msg.UID, ID: msg.Attachments[0].ID}},
	}})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Len(t, e.smtp.received, 1)
	sent, err := parseMessage([]byte(e.smtp.received[0].data), false)
	require.NoError(t, err)
	assert.Equal(t, "See attached.", sent.Text)
	require.Len(t, sent.Attachments, 1)
	assert.Equal(t, "schedule.pdf", sent.Attachments[0].Filename)
	_, content, err := openAttachment([]byte(e.smtp.received[0].data), sent.Attachments[0].ID)
	require.NoError(t, err)
	_, original, err := openAttachment(multipartMessage, "2")
	require.NoError(t, err)
	assert.Equal(t, original, content)

	// Missing or too large attachments
	w = e.request(t, http.MethodPost, "/api/v1/send", sendRequest{OutgoingMessage: OutgoingMessage{
		To: []Address{{Email: "marie@example.com"}}, Attachments: []AttachmentRef{{Folder: "INBOX", UID: 1, ID: "9"}},
	}})
	assert.Equal(t, http.StatusNotFound, w.Code)
	session := e.imap.dial(t)
	_, err = session.fetchAttachments([]AttachmentRef{pdf}, 100)
	assert.Equal(t, http.StatusRequestEntityTooLarge, statusFor(err))
	many := make([]AttachmentRef, maxAttachments+1)
	for i := range many {
		many[i] = pdf
	}
	_, err = session.fetchAttachments(many, 1<<20)
	assert.Equal(t, http.StatusBadRequest, statusFor(err))
	attachments, err := session.fetchAttachments([]AttachmentRef{pdf, pdf}, 1<<20)
	require.NoError(t, err)
	assert.Len(t, attachments, 2)
}
