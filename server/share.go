// Copyright (c) 2026-present Antimatter contributors.
// See LICENSE.txt for license information.

package main

import (
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/mattermost/mattermost/server/public/model"
)

const (
	// shareExcerptLength is the length of the excerpt of a message shared to a channel.
	shareExcerptLength = 600
	maxShareComment    = 4000
)

// shareRequest posts a summary of a message into a channel.
type shareRequest struct {
	Folder    string `json:"folder"`
	UID       uint32 `json:"uid"`
	ChannelID string `json:"channel_id"`
	Comment   string `json:"comment"`
}

// neutralizeMentions keeps the text of a message from mentioning anyone in the channel it's
// shared to.
func neutralizeMentions(s string) string {
	return strings.ReplaceAll(s, "@", "@​")
}

func formatAddress(a Address) string {
	if a.Name == "" {
		return a.Email
	}
	return a.Name + " <" + a.Email + ">"
}

// excerpt returns the beginning of the text of a message, without its quotes, as a Markdown quote.
func excerpt(text string) string {
	var lines []string
	blank := false
	for line := range strings.SplitSeq(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		line = strings.TrimRight(line, " \t")
		if strings.HasPrefix(strings.TrimSpace(line), ">") {
			continue
		}
		if line == "" {
			if blank || len(lines) == 0 {
				continue
			}
			blank = true
		} else {
			blank = false
		}
		lines = append(lines, line)
	}
	short := strings.TrimSpace(strings.Join(lines, "\n"))
	if utf8.RuneCountInString(short) > shareExcerptLength {
		short = strings.TrimSpace(string([]rune(short)[:shareExcerptLength])) + "…"
	}
	if short == "" {
		return ""
	}
	return "> " + strings.ReplaceAll(short, "\n", "\n> ")
}

// shareAttachment is the card that sums up a message shared to a channel.
func shareAttachment(msg *FullMessage) *model.SlackAttachment {
	from := make([]string, 0, len(msg.From))
	for _, a := range msg.From {
		from = append(from, formatAddress(a))
	}
	subject := msg.Subject
	if subject == "" {
		subject = "(no subject)"
	}

	attachment := &model.SlackAttachment{
		Fallback:   neutralizeMentions("Email: " + subject),
		Color:      "#2DD4BF",
		AuthorName: neutralizeMentions(strings.Join(from, ", ")),
		Title:      neutralizeMentions(subject),
		Text:       neutralizeMentions(excerpt(msg.Text)),
		Footer:     "Email",
	}
	if msg.Date != 0 {
		attachment.Timestamp = time.UnixMilli(msg.Date).Unix()
	}
	if len(msg.Attachments) > 0 {
		names := make([]string, 0, len(msg.Attachments))
		for _, a := range msg.Attachments {
			names = append(names, a.Filename)
		}
		attachment.Fields = []*model.SlackAttachmentField{{Title: "Attachments", Value: neutralizeMentions(strings.Join(names, ", "))}}
	}
	return attachment
}

func (p *Plugin) handleShare(w http.ResponseWriter, r *http.Request) {
	var req shareRequest
	if err := readJSON(w, r, &req); err != nil {
		p.writeError(w, err)
		return
	}
	if req.Folder == "" || req.UID == 0 || !model.IsValidId(req.ChannelID) {
		p.writeError(w, newAPIError(http.StatusBadRequest, "invalid request body"))
		return
	}
	if len(req.Comment) > maxShareComment {
		p.writeError(w, newAPIError(http.StatusBadRequest, "the comment is too long"))
		return
	}

	if !p.API.HasPermissionToChannel(userID(r), req.ChannelID, model.PermissionCreatePost) {
		p.writeError(w, newAPIError(http.StatusForbidden, "you can't post in this channel"))
		return
	}
	channel, appErr := p.API.GetChannel(req.ChannelID)
	if appErr != nil || channel.DeleteAt != 0 {
		p.writeError(w, newAPIError(http.StatusNotFound, "channel not found"))
		return
	}

	var msg *FullMessage
	err := p.withIMAP(userID(r), func(_ *Account, session *imapSession) error {
		data, err := session.GetMessage(req.Folder, req.UID, p.getConfiguration().maxMessageSize())
		if err != nil {
			return err
		}
		msg, err = parseMessage(data.Raw, false)
		return err
	})
	if err != nil {
		p.writeError(w, err)
		return
	}

	post := &model.Post{UserId: userID(r), ChannelId: channel.Id, Message: strings.TrimSpace(req.Comment)}
	model.ParseSlackAttachment(post, []*model.SlackAttachment{shareAttachment(msg)})
	created, appErr := p.API.CreatePost(post)
	if appErr != nil {
		p.writeError(w, appErr)
		return
	}
	p.writeJSON(w, map[string]string{"post_id": created.Id})
}
