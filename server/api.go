// Copyright (c) 2026-present Antimatter contributors.
// See LICENSE.txt for license information.

package main

import (
	"encoding/json"
	"mime"
	"net/http"
	"strconv"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/gorilla/mux"
	"github.com/pkg/errors"
)

const (
	requestBodyMaxSizeBytes = 2 << 20
	defaultPerPage          = 50
	maxPerPage              = 200
	maxUIDsPerRequest       = 500
)

// ClientConfig is the part of the plugin configuration clients need.
type ClientConfig struct {
	AllowInsecureConnections bool `json:"allow_insecure_connections"`
}

// accountView is an account as shown to its user, without the passwords.
type accountView struct {
	Email string         `json:"email"`
	Name  string         `json:"name"`
	IMAP  ServerSettings `json:"imap"`
	SMTP  ServerSettings `json:"smtp"`
}

func newAccountView(a *Account) *accountView {
	if a == nil {
		return nil
	}
	return &accountView{Email: a.Email, Name: a.Name, IMAP: a.IMAP, SMTP: a.SMTP}
}

// accountRequest saves an account. Empty passwords keep the stored ones.
type accountRequest struct {
	accountView
	IMAPPassword string `json:"imap_password"`
	SMTPPassword string `json:"smtp_password"`
}

// messagesRequest acts on messages of a folder.
type messagesRequest struct {
	Folder string   `json:"folder"`
	UIDs   []uint32 `json:"uids"`

	// Flag and Value set or clear a flag ("seen" or "flagged").
	Flag  string `json:"flag,omitempty"`
	Value bool   `json:"value,omitempty"`

	// Destination is the folder to move the messages to.
	Destination string `json:"destination,omitempty"`
}

// sendRequest sends a message.
type sendRequest struct {
	OutgoingMessage

	// ReplyTo is the message replied to, flagged as answered once the reply is sent.
	ReplyTo *messageRef `json:"reply_to_message,omitempty"`
}

type messageRef struct {
	Folder string `json:"folder"`
	UID    uint32 `json:"uid"`
}

// newRouter returns the router of the plugin's REST API, served under
// /plugins/com.antimatterchat.mail/api/v1.
func (p *Plugin) newRouter() *mux.Router {
	router := mux.NewRouter()
	router.Use(p.requireUser)

	api := router.PathPrefix("/api/v1").Subrouter()
	api.HandleFunc("/config", p.handleGetConfig).Methods(http.MethodGet)
	api.HandleFunc("/account", p.handleGetAccount).Methods(http.MethodGet)
	api.HandleFunc("/account", p.handleSaveAccount).Methods(http.MethodPut)
	api.HandleFunc("/account", p.handleDeleteAccount).Methods(http.MethodDelete)
	api.HandleFunc("/folders", p.handleListFolders).Methods(http.MethodGet)
	api.HandleFunc("/messages", p.handleListMessages).Methods(http.MethodGet)
	api.HandleFunc("/message", p.handleGetMessage).Methods(http.MethodGet)
	api.HandleFunc("/attachment", p.handleGetAttachment).Methods(http.MethodGet)
	api.HandleFunc("/messages/flag", p.handleSetFlag).Methods(http.MethodPost)
	api.HandleFunc("/messages/move", p.handleMove).Methods(http.MethodPost)
	api.HandleFunc("/messages/delete", p.handleDelete).Methods(http.MethodPost)
	api.HandleFunc("/send", p.handleSend).Methods(http.MethodPost)
	api.HandleFunc("/unread", p.handleGetUnread).Methods(http.MethodGet)
	api.HandleFunc("/share", p.handleShare).Methods(http.MethodPost)

	return router
}

func (p *Plugin) requireUser(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Mattermost-User-Id") == "" {
			http.Error(w, "Not authorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (p *Plugin) writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		p.API.LogWarn("Failed to write response", "err", err.Error())
	}
}

func (p *Plugin) writeError(w http.ResponseWriter, err error) {
	status := statusFor(err)
	message := err.Error()
	if status >= http.StatusInternalServerError && status != http.StatusBadGateway {
		p.API.LogError("Mail request failed", "err", err.Error())
		message = "something went wrong, try again later"
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": message})
}

func readJSON(w http.ResponseWriter, r *http.Request, v any) error {
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, requestBodyMaxSizeBytes)).Decode(v); err != nil {
		return newAPIError(http.StatusBadRequest, "invalid request body")
	}
	return nil
}

func userID(r *http.Request) string {
	return r.Header.Get("Mattermost-User-Id")
}

var errNoAccount = newAPIError(http.StatusConflict, "connect a mail account first")

// withIMAP runs f with a connection to the user's IMAP server.
func (p *Plugin) withIMAP(userID string, f func(account *Account, session *imapSession) error) error {
	account, err := p.store.GetAccount(userID)
	if err != nil {
		return err
	}
	if account == nil {
		return errNoAccount
	}

	conn, err := p.imapConn(userID, account)
	if err != nil {
		return err
	}
	if !conn.session.begin(operationTimeout) {
		conn.close()
		return connectionError("IMAP", errors.New("the connection was closed"))
	}
	err = f(account, conn.session)
	conn.session.end()
	if reusable(err) {
		p.pool.put(userID, conn)
	} else {
		conn.close()
	}
	return err
}

// imapConn returns a connection to the user's IMAP server: an unused one if there's one, or a
// new one.
func (p *Plugin) imapConn(userID string, account *Account) (*pooledConn, error) {
	key := account.connectionKey()
	for {
		conn := p.pool.take(userID, key)
		if conn == nil {
			break
		}
		if time.Since(conn.lastUsed) < poolCheckAfter {
			return conn, nil
		}
		// The server may have dropped a connection unused for a while
		conn.session.begin(poolCheckTimeout)
		err := conn.session.client.Noop().Wait()
		conn.session.end()
		if err == nil {
			return conn, nil
		}
		conn.close()
	}

	release, err := p.limiter.acquire(userID)
	if err != nil {
		return nil, err
	}
	session, err := dialIMAP(account.IMAP, account.IMAPPassword, p.imapOptions())
	if err != nil {
		release()
		return nil, err
	}
	return &pooledConn{session: session, release: release, key: key}, nil
}

func (p *Plugin) imapOptions() imapOptions {
	cfg := p.getConfiguration()
	return imapOptions{dialer: newDialer(cfg.AllowPrivateNetworks), allowInsecure: cfg.AllowInsecureConnections, tlsConfig: p.tlsConfig}
}

func (p *Plugin) smtpOptions() smtpOptions {
	cfg := p.getConfiguration()
	return smtpOptions{dialer: newDialer(cfg.AllowPrivateNetworks), allowInsecure: cfg.AllowInsecureConnections, tlsConfig: p.tlsConfig}
}

func (p *Plugin) handleGetConfig(w http.ResponseWriter, _ *http.Request) {
	p.writeJSON(w, ClientConfig{AllowInsecureConnections: p.getConfiguration().AllowInsecureConnections})
}

func (p *Plugin) handleGetAccount(w http.ResponseWriter, r *http.Request) {
	account, err := p.store.GetAccount(userID(r))
	if err != nil {
		p.writeError(w, err)
		return
	}
	p.writeJSON(w, map[string]any{"account": newAccountView(account)})
}

func (p *Plugin) handleSaveAccount(w http.ResponseWriter, r *http.Request) {
	var req accountRequest
	if err := readJSON(w, r, &req); err != nil {
		p.writeError(w, err)
		return
	}

	existing, err := p.store.GetAccount(userID(r))
	if err != nil {
		// An account that can't be decrypted any more is replaced
		existing = nil
	}

	account := &Account{
		Email:        req.Email,
		Name:         req.Name,
		IMAP:         req.IMAP,
		SMTP:         req.SMTP,
		IMAPPassword: req.IMAPPassword,
		SMTPPassword: req.SMTPPassword,
	}
	if account.SMTP.Username == "" {
		account.SMTP.Username = account.IMAP.Username
	}
	if account.IMAPPassword == "" && existing != nil {
		account.IMAPPassword = existing.IMAPPassword
	}
	if account.SMTPPassword == "" {
		if existing != nil {
			account.SMTPPassword = existing.SMTPPassword
		} else {
			account.SMTPPassword = account.IMAPPassword
		}
	}
	if err := account.Validate(); err != nil {
		p.writeError(w, err)
		return
	}

	// Check that both servers accept the account before saving it
	release, err := p.limiter.acquire(userID(r))
	if err != nil {
		p.writeError(w, err)
		return
	}
	defer release()
	session, err := dialIMAP(account.IMAP, account.IMAPPassword, p.imapOptions())
	if err != nil {
		p.writeError(w, err)
		return
	}
	session.Close()
	if err := checkSMTP(account, p.smtpOptions()); err != nil {
		p.writeError(w, err)
		return
	}

	if err := p.store.SaveAccount(userID(r), account); err != nil {
		p.writeError(w, err)
		return
	}
	p.pool.drop(userID(r))
	p.unread.Forget(userID(r))
	p.writeJSON(w, map[string]any{"account": newAccountView(account)})
}

func (p *Plugin) handleDeleteAccount(w http.ResponseWriter, r *http.Request) {
	if err := p.store.DeleteAccount(userID(r)); err != nil {
		p.writeError(w, err)
		return
	}
	p.pool.drop(userID(r))
	p.unread.Forget(userID(r))
	p.writeJSON(w, map[string]any{"account": nil})
}

func (p *Plugin) handleListFolders(w http.ResponseWriter, r *http.Request) {
	var folders []Folder
	err := p.withIMAP(userID(r), func(_ *Account, session *imapSession) error {
		var err error
		folders, err = session.ListFolders()
		return err
	})
	if err != nil {
		p.writeError(w, err)
		return
	}
	p.writeJSON(w, folders)
}

func queryInt(r *http.Request, name string, def, maxValue int) int {
	value, err := strconv.Atoi(r.URL.Query().Get(name))
	if err != nil || value < 0 {
		return def
	}
	return min(value, maxValue)
}

func queryFolder(r *http.Request) (string, error) {
	folder := r.URL.Query().Get("folder")
	if folder == "" {
		return "", newAPIError(http.StatusBadRequest, "a folder is required")
	}
	return folder, nil
}

func queryUID(r *http.Request) (uint32, error) {
	uid, err := strconv.ParseUint(r.URL.Query().Get("uid"), 10, 32)
	if err != nil || uid == 0 {
		return 0, newAPIError(http.StatusBadRequest, "invalid message uid")
	}
	return uint32(uid), nil
}

func (p *Plugin) handleListMessages(w http.ResponseWriter, r *http.Request) {
	folder, err := queryFolder(r)
	if err != nil {
		p.writeError(w, err)
		return
	}
	page := queryInt(r, "page", 0, 1<<20)
	perPage := queryInt(r, "per_page", defaultPerPage, maxPerPage)
	if perPage == 0 {
		perPage = defaultPerPage
	}

	var result *MessagePage
	err = p.withIMAP(userID(r), func(_ *Account, session *imapSession) error {
		var err error
		result, err = session.ListMessages(folder, page, perPage)
		return err
	})
	if err != nil {
		p.writeError(w, err)
		return
	}
	p.writeJSON(w, result)
}

func (p *Plugin) handleGetMessage(w http.ResponseWriter, r *http.Request) {
	folder, err := queryFolder(r)
	if err != nil {
		p.writeError(w, err)
		return
	}
	uid, err := queryUID(r)
	if err != nil {
		p.writeError(w, err)
		return
	}
	remoteImages := r.URL.Query().Get("remote_images") == "true"
	markRead := r.URL.Query().Get("mark_read") == "true"

	var msg *FullMessage
	err = p.withIMAP(userID(r), func(_ *Account, session *imapSession) error {
		data, err := session.GetMessage(folder, uid, p.getConfiguration().maxMessageSize())
		if err != nil {
			return err
		}
		if msg, err = parseMessage(data.Raw, remoteImages); err != nil {
			return err
		}
		msg.Folder, msg.UID, msg.Unread, msg.Flagged = folder, uid, data.Unread, data.Flagged
		if markRead && data.Unread {
			if err := session.SetFlag(folder, []uint32{uid}, imap.FlagSeen, true); err != nil {
				return err
			}
			msg.Unread = false
		}
		return nil
	})
	if err != nil {
		p.writeError(w, err)
		return
	}
	p.writeJSON(w, msg)
}

func (p *Plugin) handleGetAttachment(w http.ResponseWriter, r *http.Request) {
	folder, err := queryFolder(r)
	if err != nil {
		p.writeError(w, err)
		return
	}
	uid, err := queryUID(r)
	if err != nil {
		p.writeError(w, err)
		return
	}
	id := r.URL.Query().Get("id")

	var attachment *Attachment
	var content []byte
	err = p.withIMAP(userID(r), func(_ *Account, session *imapSession) error {
		data, err := session.GetMessage(folder, uid, p.getConfiguration().maxMessageSize())
		if err != nil {
			return err
		}
		attachment, content, err = openAttachment(data.Raw, id)
		return err
	})
	if err != nil {
		p.writeError(w, err)
		return
	}

	// Always download: attachments are untrusted content that mustn't run in the app's origin
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": attachment.Filename}))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "sandbox")
	w.Header().Set("Content-Length", strconv.Itoa(len(content)))
	_, _ = w.Write(content)
}

func readMessagesRequest(w http.ResponseWriter, r *http.Request) (*messagesRequest, error) {
	var req messagesRequest
	if err := readJSON(w, r, &req); err != nil {
		return nil, err
	}
	if req.Folder == "" {
		return nil, newAPIError(http.StatusBadRequest, "a folder is required")
	}
	if len(req.UIDs) == 0 || len(req.UIDs) > maxUIDsPerRequest {
		return nil, newAPIError(http.StatusBadRequest, "invalid message uids")
	}
	for _, uid := range req.UIDs {
		if uid == 0 {
			return nil, newAPIError(http.StatusBadRequest, "invalid message uids")
		}
	}
	return &req, nil
}

func (p *Plugin) handleSetFlag(w http.ResponseWriter, r *http.Request) {
	req, err := readMessagesRequest(w, r)
	if err != nil {
		p.writeError(w, err)
		return
	}
	var flag imap.Flag
	switch req.Flag {
	case "seen":
		flag = imap.FlagSeen
	case "flagged":
		flag = imap.FlagFlagged
	default:
		p.writeError(w, newAPIError(http.StatusBadRequest, "invalid flag"))
		return
	}

	err = p.withIMAP(userID(r), func(_ *Account, session *imapSession) error {
		return session.SetFlag(req.Folder, req.UIDs, flag, req.Value)
	})
	if err != nil {
		p.writeError(w, err)
		return
	}
	p.writeJSON(w, map[string]string{"status": "ok"})
}

func (p *Plugin) handleMove(w http.ResponseWriter, r *http.Request) {
	req, err := readMessagesRequest(w, r)
	if err != nil {
		p.writeError(w, err)
		return
	}
	if req.Destination == "" || req.Destination == req.Folder {
		p.writeError(w, newAPIError(http.StatusBadRequest, "invalid destination folder"))
		return
	}

	err = p.withIMAP(userID(r), func(_ *Account, session *imapSession) error {
		return session.Move(req.Folder, req.UIDs, req.Destination)
	})
	if err != nil {
		p.writeError(w, err)
		return
	}
	p.writeJSON(w, map[string]string{"status": "ok"})
}

func (p *Plugin) handleDelete(w http.ResponseWriter, r *http.Request) {
	req, err := readMessagesRequest(w, r)
	if err != nil {
		p.writeError(w, err)
		return
	}

	err = p.withIMAP(userID(r), func(_ *Account, session *imapSession) error {
		return session.Delete(req.Folder, req.UIDs)
	})
	if err != nil {
		p.writeError(w, err)
		return
	}
	p.writeJSON(w, map[string]string{"status": "ok"})
}

func (p *Plugin) handleSend(w http.ResponseWriter, r *http.Request) {
	var req sendRequest
	if err := readJSON(w, r, &req); err != nil {
		p.writeError(w, err)
		return
	}
	account, err := p.store.GetAccount(userID(r))
	if err != nil {
		p.writeError(w, err)
		return
	}
	if account == nil {
		p.writeError(w, errNoAccount)
		return
	}

	raw, err := buildMessage(account, &req.OutgoingMessage, time.Now())
	if err != nil {
		p.writeError(w, err)
		return
	}
	recipients, err := req.Recipients()
	if err != nil {
		p.writeError(w, err)
		return
	}

	release, err := p.limiter.acquire(userID(r))
	if err != nil {
		p.writeError(w, err)
		return
	}
	err = sendSMTP(account, recipients, raw, p.smtpOptions())
	release()
	if err != nil {
		p.writeError(w, err)
		return
	}

	// The message is sent: keeping a copy and flagging the original are best effort
	savedCopy := false
	err = p.withIMAP(userID(r), func(_ *Account, session *imapSession) error {
		var err error
		if savedCopy, err = session.AppendToRole(RoleSent, raw, []imap.Flag{imap.FlagSeen}); err != nil {
			return err
		}
		if req.ReplyTo != nil && req.ReplyTo.Folder != "" && req.ReplyTo.UID != 0 {
			return session.SetFlag(req.ReplyTo.Folder, []uint32{req.ReplyTo.UID}, imap.FlagAnswered, true)
		}
		return nil
	})
	if err != nil {
		p.API.LogWarn("Failed to store a sent message", "user_id", userID(r), "err", err.Error())
	}
	p.writeJSON(w, map[string]any{"status": "ok", "saved_copy": savedCopy})
}

// handleGetUnread returns the unread count of the user's inbox. The webapp asks for it every few
// minutes while it's open, which keeps the inbox watched: the user is then told when the count
// changes. refresh=true skips the cached count, after the user read or moved messages.
func (p *Plugin) handleGetUnread(w http.ResponseWriter, r *http.Request) {
	p.unread.Touch(userID(r))
	if r.URL.Query().Get("refresh") != "true" {
		if unread, ok := p.unread.Cached(userID(r)); ok {
			p.writeJSON(w, map[string]int{"unread": unread})
			return
		}
	}

	var unread int
	err := p.withIMAP(userID(r), func(_ *Account, session *imapSession) error {
		var err error
		unread, err = session.Unread("INBOX")
		return err
	})
	if err != nil {
		p.writeError(w, err)
		return
	}
	p.unread.Record(userID(r), unread)
	p.writeJSON(w, map[string]int{"unread": unread})
}
