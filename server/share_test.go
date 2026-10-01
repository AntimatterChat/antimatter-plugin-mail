// Copyright (c) 2026-present Antimatter contributors.
// See LICENSE.txt for license information.

package main

import (
	"net/http"
	"strings"
	"testing"

	"github.com/mattermost/mattermost/server/public/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestExcerpt(t *testing.T) {
	assert.Equal(t, "> Hello,\n> \n> The run is at 11:00.", excerpt("\r\nHello,\r\n\r\n\r\nThe run is at 11:00.\r\n> quoted reply\r\n"))
	assert.Equal(t, "", excerpt("> only a quote"))
	long := excerpt(strings.Repeat("a", 1000))
	assert.Equal(t, "> "+strings.Repeat("a", shareExcerptLength)+"…", long)
}

func TestShareAttachment(t *testing.T) {
	msg, err := parseMessage(multipartMessage, false)
	require.NoError(t, err)
	attachment := shareAttachment(msg)
	assert.Equal(t, "Marie Laurent <marie@\u200bexample.com>", attachment.AuthorName)
	assert.Equal(t, "Beam schedule draft — v2", attachment.Title)
	assert.True(t, strings.HasPrefix(attachment.Text, "> Attached the draft"))
	assert.Equal(t, int64(1790670660), attachment.Timestamp)
	require.Len(t, attachment.Fields, 1)
	assert.Equal(t, "schedule.pdf", attachment.Fields[0].Value)

	// Mail can't mention people in the channel
	assert.Equal(t, "Ping @\u200bchannel", neutralizeMentions("Ping @channel"))
}

func TestAPIShare(t *testing.T) {
	e := newTestEnv(t)
	e.imap.add(t, "INBOX", multipartMessage)
	e.connect(t)
	channel := &model.Channel{Id: model.NewId(), Type: model.ChannelTypeOpen}

	e.api.On("HasPermissionToChannel", e.userID, channel.Id, model.PermissionCreatePost).Return(true)
	e.api.On("GetChannel", channel.Id).Return(channel, nil)
	e.api.On("CreatePost", mock.MatchedBy(func(post *model.Post) bool {
		attachments := post.Attachments()
		return post.UserId == e.userID && post.ChannelId == channel.Id && post.Message == "For the review" &&
			len(attachments) == 1 && attachments[0].Title == "Beam schedule draft — v2"
	})).Return(&model.Post{Id: "post1"}, nil).Once()

	w := e.request(t, http.MethodPost, "/api/v1/share", shareRequest{Folder: "INBOX", UID: 1, ChannelID: channel.Id, Comment: " For the review "})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.JSONEq(t, `{"post_id": "post1"}`, w.Body.String())

	other := model.NewId()
	e.api.On("HasPermissionToChannel", e.userID, other, model.PermissionCreatePost).Return(false)
	w = e.request(t, http.MethodPost, "/api/v1/share", shareRequest{Folder: "INBOX", UID: 1, ChannelID: other})
	assert.Equal(t, http.StatusForbidden, w.Code)

	w = e.request(t, http.MethodPost, "/api/v1/share", shareRequest{Folder: "INBOX", UID: 1, ChannelID: "bad"})
	assert.Equal(t, http.StatusBadRequest, w.Code)
}
