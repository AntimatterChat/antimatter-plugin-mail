// Copyright (c) 2026-present Antimatter contributors.
// See LICENSE.txt for license information.

package main

import (
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
	"github.com/pkg/errors"
)

const (
	// sessionLifetime bounds how long an IMAP connection stays open, so that a stuck server
	// can't hold a request forever.
	sessionLifetime = 2 * time.Minute

	// maxStatusFolders is the number of folders whose counts are fetched one by one on servers
	// that can't return them with the folder list.
	maxStatusFolders = 50

	// snippetBytes is how much of the text of each message is fetched for its snippet.
	snippetBytes = 2048
)

// Folder roles, from the folder's special use (RFC 6154) or its name.
const (
	RoleInbox   = "inbox"
	RoleDrafts  = "drafts"
	RoleSent    = "sent"
	RoleArchive = "archive"
	RoleJunk    = "junk"
	RoleTrash   = "trash"
)

// Folder is an IMAP mailbox.
type Folder struct {
	Name      string `json:"name"`
	Delimiter string `json:"delimiter,omitempty"`
	Role      string `json:"role,omitempty"`
	Total     int    `json:"total"`
	Unread    int    `json:"unread"`
}

// MessageSummary is a message as shown in a message list.
type MessageSummary struct {
	UID            uint32    `json:"uid"`
	From           []Address `json:"from"`
	To             []Address `json:"to"`
	Subject        string    `json:"subject"`
	Date           int64     `json:"date"`
	Size           int64     `json:"size"`
	Unread         bool      `json:"unread"`
	Flagged        bool      `json:"flagged"`
	Answered       bool      `json:"answered"`
	HasAttachments bool      `json:"has_attachments"`
	Snippet        string    `json:"snippet"`
}

// MessagePage is a page of the messages of a folder, newest first.
type MessagePage struct {
	Folder   string           `json:"folder"`
	Total    int              `json:"total"`
	Page     int              `json:"page"`
	PerPage  int              `json:"per_page"`
	Messages []MessageSummary `json:"messages"`
}

// imapOptions say how to connect to IMAP servers.
type imapOptions struct {
	dialer        *net.Dialer
	allowInsecure bool
	// tlsConfig is nil for the default TLS configuration (tests set their own CA).
	tlsConfig *tls.Config
}

// imapSession is a logged in IMAP connection.
type imapSession struct {
	client    *imapclient.Client
	closeOnce sync.Once
	timer     *time.Timer
}

func connectionError(what string, err error) error {
	var apiErr *apiError
	if errors.As(err, &apiErr) {
		return apiErr
	}
	return &apiError{status: http.StatusBadGateway, err: errors.Wrapf(err, "couldn't connect to the %s server", what)}
}

// dialIMAP connects and logs in to an IMAP server.
func dialIMAP(settings ServerSettings, password string, opts imapOptions) (*imapSession, error) {
	address := net.JoinHostPort(settings.Host, strconv.Itoa(settings.Port))
	clientOptions := &imapclient.Options{Dialer: opts.dialer, TLSConfig: opts.tlsConfig}

	var client *imapclient.Client
	var err error
	switch settings.Security {
	case SecurityTLS:
		if clientOptions.TLSConfig == nil {
			clientOptions.TLSConfig = &tls.Config{ServerName: settings.Host, MinVersion: tls.VersionTLS12}
		}
		client, err = imapclient.DialTLS(address, clientOptions)
	case SecurityStartTLS:
		client, err = imapclient.DialStartTLS(address, clientOptions)
	case SecurityNone:
		if !opts.allowInsecure {
			return nil, newAPIError(http.StatusForbidden, "unencrypted connections to mail servers aren't allowed on this server")
		}
		client, err = imapclient.DialInsecure(address, clientOptions)
	default:
		return nil, newAPIError(http.StatusBadRequest, "invalid connection security")
	}
	if err != nil {
		return nil, connectionError("IMAP", err)
	}

	session := &imapSession{client: client}
	session.timer = time.AfterFunc(sessionLifetime, session.close)

	if err := client.WaitGreeting(); err != nil {
		session.close()
		return nil, connectionError("IMAP", err)
	}
	if err := client.Login(settings.Username, password).Wait(); err != nil {
		session.close()
		var imapErr *imap.Error
		if errors.As(err, &imapErr) {
			return nil, &apiError{status: http.StatusBadRequest, err: errors.Errorf("the IMAP server refused to log in: %s", imapErr.Text)}
		}
		return nil, connectionError("IMAP", err)
	}
	return session, nil
}

func (s *imapSession) close() {
	s.closeOnce.Do(func() {
		s.timer.Stop()
		_ = s.client.Close()
	})
}

// Close logs out and closes the connection.
func (s *imapSession) Close() {
	_ = s.client.Logout().Wait()
	s.close()
}

func folderRole(name string, attrs []imap.MailboxAttr) string {
	if strings.EqualFold(name, "INBOX") {
		return RoleInbox
	}
	for _, attr := range attrs {
		switch attr {
		case imap.MailboxAttrDrafts:
			return RoleDrafts
		case imap.MailboxAttrSent:
			return RoleSent
		case imap.MailboxAttrArchive:
			return RoleArchive
		case imap.MailboxAttrJunk:
			return RoleJunk
		case imap.MailboxAttrTrash:
			return RoleTrash
		}
	}
	return ""
}

// guessRoles gives roles to folders with the usual names, on servers without special-use
// attributes.
func guessRoles(folders []Folder) {
	names := map[string][]string{
		RoleDrafts:  {"drafts", "draft"},
		RoleSent:    {"sent", "sent items", "sent messages", "sent mail"},
		RoleArchive: {"archive", "archives"},
		RoleJunk:    {"junk", "spam", "junk e-mail", "junk email"},
		RoleTrash:   {"trash", "deleted items", "deleted messages", "bin"},
	}
	for role, candidates := range names {
		if slices.ContainsFunc(folders, func(f Folder) bool { return f.Role == role }) {
			continue
		}
		for i := range folders {
			if folders[i].Role != "" {
				continue
			}
			name := folders[i].Name
			if folders[i].Delimiter != "" {
				name = name[strings.LastIndex(name, folders[i].Delimiter)+1:]
			}
			if slices.Contains(candidates, strings.ToLower(name)) {
				folders[i].Role = role
				break
			}
		}
	}
}

var roleOrder = []string{RoleInbox, RoleDrafts, RoleSent, RoleArchive, RoleJunk, RoleTrash}

func sortFolders(folders []Folder) {
	rank := func(f Folder) int {
		if i := slices.Index(roleOrder, f.Role); i >= 0 {
			return i
		}
		return len(roleOrder)
	}
	sort.SliceStable(folders, func(i, j int) bool {
		ri, rj := rank(folders[i]), rank(folders[j])
		if ri != rj {
			return ri < rj
		}
		return strings.ToLower(folders[i].Name) < strings.ToLower(folders[j].Name)
	})
}

// ListFolders returns the selectable folders, with their message counts.
func (s *imapSession) ListFolders() ([]Folder, error) {
	options := &imap.ListOptions{}
	withStatus := s.client.Caps().Has(imap.CapListStatus)
	if withStatus {
		options.ReturnStatus = &imap.StatusOptions{NumMessages: true, NumUnseen: true}
	}
	list, err := s.client.List("", "*", options).Collect()
	if err != nil {
		return nil, errors.Wrap(err, "failed to list the folders")
	}

	folders := make([]Folder, 0, len(list))
	for _, data := range list {
		if slices.Contains(data.Attrs, imap.MailboxAttrNoSelect) || slices.Contains(data.Attrs, imap.MailboxAttrNonExistent) {
			continue
		}
		folder := Folder{Name: data.Mailbox, Role: folderRole(data.Mailbox, data.Attrs)}
		if data.Delim != 0 {
			folder.Delimiter = string(data.Delim)
		}
		if data.Status != nil {
			setCounts(&folder, data.Status)
		}
		folders = append(folders, folder)
	}
	guessRoles(folders)
	sortFolders(folders)

	if !withStatus {
		// Pipeline the STATUS commands
		count := min(len(folders), maxStatusFolders)
		commands := make([]*imapclient.StatusCommand, count)
		for i := range count {
			commands[i] = s.client.Status(folders[i].Name, &imap.StatusOptions{NumMessages: true, NumUnseen: true})
		}
		for i, cmd := range commands {
			if status, err := cmd.Wait(); err == nil {
				setCounts(&folders[i], status)
			}
		}
	}
	return folders, nil
}

func setCounts(folder *Folder, status *imap.StatusData) {
	if status.NumMessages != nil {
		folder.Total = int(*status.NumMessages)
	}
	if status.NumUnseen != nil {
		folder.Unread = int(*status.NumUnseen)
	}
}

// Unread returns the number of unread messages of a folder.
func (s *imapSession) Unread(folder string) (int, error) {
	status, err := s.client.Status(folder, &imap.StatusOptions{NumUnseen: true}).Wait()
	if err != nil {
		return 0, errors.Wrap(err, "failed to get the folder status")
	}
	if status.NumUnseen == nil {
		return 0, nil
	}
	return int(*status.NumUnseen), nil
}

func (s *imapSession) selectFolder(folder string, readOnly bool) (*imap.SelectData, error) {
	data, err := s.client.Select(folder, &imap.SelectOptions{ReadOnly: readOnly}).Wait()
	if err != nil {
		var imapErr *imap.Error
		if errors.As(err, &imapErr) && imapErr.Type == imap.StatusResponseTypeNo {
			return nil, &apiError{status: http.StatusNotFound, err: errors.Errorf("couldn't open the folder %q: %s", folder, imapErr.Text)}
		}
		return nil, errors.Wrap(err, "failed to open the folder")
	}
	return data, nil
}

func convertAddresses(list []imap.Address) []Address {
	addresses := make([]Address, 0, len(list))
	for _, a := range list {
		if a.IsGroupStart() || a.IsGroupEnd() {
			continue
		}
		addresses = append(addresses, Address{Name: a.Name, Email: a.Addr()})
	}
	return addresses
}

// textPart finds the part of a message to make a snippet from: its first text part that isn't an
// attachment, plain text preferred.
func textPart(bs imap.BodyStructure) (path []int, part *imap.BodyStructureSinglePart, hasAttachments bool) {
	var html []int
	var htmlPart *imap.BodyStructureSinglePart
	bs.Walk(func(p []int, node imap.BodyStructure) bool {
		single, ok := node.(*imap.BodyStructureSinglePart)
		if !ok {
			return true
		}
		if d := single.Disposition(); d != nil && strings.EqualFold(d.Value, "attachment") || single.Filename() != "" {
			hasAttachments = true
			return true
		}
		switch single.MediaType() {
		case "text/plain":
			if part == nil {
				path, part = slices.Clone(p), single
			}
		case "text/html":
			if htmlPart == nil {
				html, htmlPart = slices.Clone(p), single
			}
		}
		return true
	})
	if part == nil {
		path, part = html, htmlPart
	}
	return path, part, hasAttachments
}

// ListMessages returns a page of the messages of a folder, newest first. Pages start at 0.
func (s *imapSession) ListMessages(folder string, page, perPage int) (*MessagePage, error) {
	data, err := s.selectFolder(folder, true)
	if err != nil {
		return nil, err
	}

	result := &MessagePage{Folder: folder, Total: int(data.NumMessages), Page: page, PerPage: perPage, Messages: []MessageSummary{}}
	total := int(data.NumMessages)
	last := total - page*perPage
	if last < 1 {
		return result, nil
	}
	first := max(last-perPage+1, 1)

	var seqSet imap.SeqSet
	seqSet.AddRange(uint32(first), uint32(last))
	messages, err := s.client.Fetch(seqSet, &imap.FetchOptions{
		UID:           true,
		Flags:         true,
		Envelope:      true,
		InternalDate:  true,
		RFC822Size:    true,
		BodyStructure: &imap.FetchItemBodyStructure{},
	}).Collect()
	if err != nil {
		return nil, errors.Wrap(err, "failed to fetch the messages")
	}
	sort.Slice(messages, func(i, j int) bool { return messages[i].SeqNum > messages[j].SeqNum })

	// Parts to fetch for the snippets, by part path
	type snippetTarget struct {
		index int
		part  *imap.BodyStructureSinglePart
	}
	snippetParts := map[string][]snippetTarget{}

	for i, msg := range messages {
		summary := MessageSummary{
			UID:      uint32(msg.UID),
			Size:     msg.RFC822Size,
			Unread:   !slices.Contains(msg.Flags, imap.FlagSeen),
			Flagged:  slices.Contains(msg.Flags, imap.FlagFlagged),
			Answered: slices.Contains(msg.Flags, imap.FlagAnswered),
			From:     []Address{},
			To:       []Address{},
		}
		if env := msg.Envelope; env != nil {
			summary.Subject = env.Subject
			summary.From = convertAddresses(env.From)
			summary.To = convertAddresses(env.To)
			if !env.Date.IsZero() {
				summary.Date = env.Date.UnixMilli()
			}
		}
		if summary.Date == 0 && !msg.InternalDate.IsZero() {
			summary.Date = msg.InternalDate.UnixMilli()
		}
		if msg.BodyStructure != nil {
			path, part, hasAttachments := textPart(msg.BodyStructure)
			summary.HasAttachments = hasAttachments
			if part != nil {
				key := fmt.Sprint(path)
				snippetParts[key] = append(snippetParts[key], snippetTarget{index: i, part: part})
			}
		}
		result.Messages = append(result.Messages, summary)
	}

	// Fetch the beginning of the text parts, one FETCH per part path
	for _, targets := range snippetParts {
		var uids imap.UIDSet
		byUID := map[imap.UID]snippetTarget{}
		for _, target := range targets {
			uid := imap.UID(result.Messages[target.index].UID)
			uids.AddNum(uid)
			byUID[uid] = target
		}
		path, _, _ := textPart(messages[targets[0].index].BodyStructure)
		section := &imap.FetchItemBodySection{Part: path, Peek: true, Partial: &imap.SectionPartial{Offset: 0, Size: snippetBytes}}
		bodies, err := s.client.Fetch(uids, &imap.FetchOptions{UID: true, BodySection: []*imap.FetchItemBodySection{section}}).Collect()
		if err != nil {
			// Snippets are optional
			continue
		}
		for _, body := range bodies {
			target, ok := byUID[body.UID]
			if !ok {
				continue
			}
			raw := body.FindBodySection(section)
			result.Messages[target.index].Snippet = snippetFromPart(raw, target.part.Encoding, target.part.Params["charset"], target.part.MediaType() == "text/html")
		}
	}

	return result, nil
}

// MessageData is a whole message.
type MessageData struct {
	UID     uint32
	Flags   []imap.Flag
	Raw     []byte
	Unread  bool
	Flagged bool
}

var errMessageNotFound = newAPIError(http.StatusNotFound, "message not found")

// GetMessage returns a whole message, without marking it as read.
func (s *imapSession) GetMessage(folder string, uid uint32, maxSize int64) (*MessageData, error) {
	if _, err := s.selectFolder(folder, true); err != nil {
		return nil, err
	}

	uidSet := imap.UIDSetNum(imap.UID(uid))
	info, err := s.client.Fetch(uidSet, &imap.FetchOptions{UID: true, Flags: true, RFC822Size: true}).Collect()
	if err != nil {
		return nil, errors.Wrap(err, "failed to fetch the message")
	}
	if len(info) == 0 {
		return nil, errMessageNotFound
	}
	if info[0].RFC822Size > maxSize {
		return nil, newAPIError(http.StatusRequestEntityTooLarge, "this message is too large to be opened here")
	}

	section := &imap.FetchItemBodySection{Peek: true}
	bodies, err := s.client.Fetch(uidSet, &imap.FetchOptions{UID: true, BodySection: []*imap.FetchItemBodySection{section}}).Collect()
	if err != nil {
		return nil, errors.Wrap(err, "failed to fetch the message")
	}
	if len(bodies) == 0 {
		return nil, errMessageNotFound
	}
	return &MessageData{
		UID:     uid,
		Flags:   info[0].Flags,
		Raw:     bodies[0].FindBodySection(section),
		Unread:  !slices.Contains(info[0].Flags, imap.FlagSeen),
		Flagged: slices.Contains(info[0].Flags, imap.FlagFlagged),
	}, nil
}

func uidSet(uids []uint32) imap.UIDSet {
	var set imap.UIDSet
	for _, uid := range uids {
		set.AddNum(imap.UID(uid))
	}
	return set
}

// SetFlag adds or removes a flag (such as \Seen) on messages.
func (s *imapSession) SetFlag(folder string, uids []uint32, flag imap.Flag, on bool) error {
	if _, err := s.selectFolder(folder, false); err != nil {
		return err
	}
	op := imap.StoreFlagsDel
	if on {
		op = imap.StoreFlagsAdd
	}
	if err := s.client.Store(uidSet(uids), &imap.StoreFlags{Op: op, Silent: true, Flags: []imap.Flag{flag}}, nil).Close(); err != nil {
		return errors.Wrap(err, "failed to update the messages")
	}
	return nil
}

// Move moves messages to another folder.
func (s *imapSession) Move(folder string, uids []uint32, dest string) error {
	if _, err := s.selectFolder(folder, false); err != nil {
		return err
	}
	if _, err := s.client.Move(uidSet(uids), dest).Wait(); err != nil {
		return errors.Wrap(err, "failed to move the messages")
	}
	return nil
}

// Delete moves messages to the trash, or deletes them for good when they are in the trash or
// there's no trash folder.
func (s *imapSession) Delete(folder string, uids []uint32) error {
	folders, err := s.ListFolders()
	if err != nil {
		return err
	}
	trash := ""
	for _, f := range folders {
		if f.Role == RoleTrash {
			trash = f.Name
		}
	}
	if trash != "" && trash != folder {
		return s.Move(folder, uids, trash)
	}

	if _, err := s.selectFolder(folder, false); err != nil {
		return err
	}
	set := uidSet(uids)
	if err := s.client.Store(set, &imap.StoreFlags{Op: imap.StoreFlagsAdd, Silent: true, Flags: []imap.Flag{imap.FlagDeleted}}, nil).Close(); err != nil {
		return errors.Wrap(err, "failed to delete the messages")
	}
	var expunge *imapclient.ExpungeCommand
	if s.client.Caps().Has(imap.CapUIDPlus) {
		expunge = s.client.UIDExpunge(set)
	} else {
		expunge = s.client.Expunge()
	}
	if err := expunge.Close(); err != nil {
		return errors.Wrap(err, "failed to delete the messages")
	}
	return nil
}

// AppendToRole stores a message in the folder with the given role (such as the sent folder), if
// there's one. It returns whether the message was stored.
func (s *imapSession) AppendToRole(role string, raw []byte, flags []imap.Flag) (bool, error) {
	folders, err := s.ListFolders()
	if err != nil {
		return false, err
	}
	for _, f := range folders {
		if f.Role != role {
			continue
		}
		cmd := s.client.Append(f.Name, int64(len(raw)), &imap.AppendOptions{Flags: flags, Time: time.Now()})
		if _, err := cmd.Write(raw); err != nil {
			_ = cmd.Close()
			return false, errors.Wrap(err, "failed to store the message")
		}
		if err := cmd.Close(); err != nil {
			return false, errors.Wrap(err, "failed to store the message")
		}
		if _, err := cmd.Wait(); err != nil {
			return false, errors.Wrap(err, "failed to store the message")
		}
		return true, nil
	}
	return false, nil
}
