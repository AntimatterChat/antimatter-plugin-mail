// Copyright (c) 2026-present Antimatter contributors.
// See LICENSE.txt for license information.

package main

import (
	"bytes"
	"encoding/base64"
	"io"
	"mime"
	"mime/quotedprintable"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/emersion/go-message"
	"github.com/emersion/go-message/charset"
	"github.com/emersion/go-message/mail"
	"github.com/pkg/errors"
)

const (
	snippetLength = 200

	// maxTextSize bounds the text and HTML of a message that are shown.
	maxTextSize = 2 << 20
)

// Address is a sender or recipient.
type Address struct {
	Name  string `json:"name,omitempty"`
	Email string `json:"email"`
}

// Attachment is a file attached to a message.
type Attachment struct {
	// ID identifies the part of the message, like the IMAP part path ("1.2").
	ID          string `json:"id"`
	Filename    string `json:"filename"`
	ContentType string `json:"content_type"`
	Size        int64  `json:"size"`
}

// FullMessage is a message as shown in the reading view.
type FullMessage struct {
	Folder     string    `json:"folder"`
	UID        uint32    `json:"uid"`
	MessageID  string    `json:"message_id"`
	InReplyTo  []string  `json:"in_reply_to"`
	References []string  `json:"references"`
	From       []Address `json:"from"`
	To         []Address `json:"to"`
	Cc         []Address `json:"cc"`
	Bcc        []Address `json:"bcc"`
	ReplyTo    []Address `json:"reply_to"`
	Subject    string    `json:"subject"`
	Date       int64     `json:"date"`
	Unread     bool      `json:"unread"`
	Flagged    bool      `json:"flagged"`
	Draft      bool      `json:"draft"`

	// Text is the plain text of the message, made from its HTML if it has no text part.
	Text string `json:"text"`
	// HTML is the sanitised HTML of the message, if it has some.
	HTML string `json:"html,omitempty"`
	// RemoteImages is the number of remote images removed from the HTML.
	RemoteImages int `json:"remote_images"`

	Attachments []Attachment `json:"attachments"`
}

func partID(path []int) string {
	if len(path) == 0 {
		return "1"
	}
	ids := make([]string, len(path))
	for i, n := range path {
		ids[i] = strconv.Itoa(n + 1)
	}
	return strings.Join(ids, ".")
}

func headerAddresses(h mail.Header, key string) []Address {
	list, err := h.AddressList(key)
	addresses := make([]Address, 0, len(list))
	if err != nil {
		// Keep what can be read of malformed headers
		if raw := strings.TrimSpace(h.Get(key)); raw != "" {
			addresses = append(addresses, Address{Email: raw})
		}
		return addresses
	}
	for _, a := range list {
		addresses = append(addresses, Address{Name: a.Name, Email: a.Address})
	}
	return addresses
}

// partFilename returns the name of an attached file, if it has one.
func partFilename(h message.Header) string {
	if _, params, err := h.ContentDisposition(); err == nil && params["filename"] != "" {
		return params["filename"]
	}
	if _, params, err := h.ContentType(); err == nil && params["name"] != "" {
		return params["name"]
	}
	return ""
}

func isAttachment(h message.Header, mediaType string) bool {
	disposition, _, _ := h.ContentDisposition()
	if strings.EqualFold(disposition, "attachment") {
		return true
	}
	switch mediaType {
	case "text/plain", "text/html":
		return partFilename(h) != ""
	default:
		return true
	}
}

// walkLeaves calls f for each part of a message that isn't a multipart, with its media type.
func walkLeaves(entity *message.Entity, f func(id, mediaType string, part *message.Entity) error) error {
	return entity.Walk(func(path []int, part *message.Entity, err error) error {
		if err != nil && !message.IsUnknownCharset(err) && !message.IsUnknownEncoding(err) {
			return err
		}
		if part.MultipartReader() != nil {
			return nil
		}
		mediaType, _, _ := part.Header.ContentType()
		if mediaType == "" {
			mediaType = "text/plain"
		}
		return f(partID(path), strings.ToLower(mediaType), part)
	})
}

// parseMessage parses a message for the reading view.
func parseMessage(raw []byte, allowRemoteImages bool) (*FullMessage, error) {
	entity, err := message.Read(bytes.NewReader(raw))
	if err != nil && !message.IsUnknownCharset(err) && !message.IsUnknownEncoding(err) {
		return nil, &apiError{status: http.StatusUnprocessableEntity, err: errors.Wrap(err, "couldn't read the message")}
	}

	header := mail.Header{Header: entity.Header}
	msg := &FullMessage{
		From:        headerAddresses(header, "From"),
		To:          headerAddresses(header, "To"),
		Cc:          headerAddresses(header, "Cc"),
		Bcc:         headerAddresses(header, "Bcc"),
		ReplyTo:     headerAddresses(header, "Reply-To"),
		InReplyTo:   []string{},
		References:  []string{},
		Attachments: []Attachment{},
	}
	if subject, err := header.Subject(); err == nil {
		msg.Subject = subject
	} else {
		msg.Subject = header.Get("Subject")
	}
	if date, err := header.Date(); err == nil {
		msg.Date = date.UnixMilli()
	}
	msg.MessageID, _ = header.MessageID()
	if ids, err := header.MsgIDList("In-Reply-To"); err == nil {
		msg.InReplyTo = append(msg.InReplyTo, ids...)
	}
	if ids, err := header.MsgIDList("References"); err == nil {
		msg.References = append(msg.References, ids...)
	}

	var text, htmlSource string
	var haveText, haveHTML bool
	inline := map[string]inlineImage{}

	err = walkLeaves(entity, func(id, mediaType string, part *message.Entity) error {
		if !isAttachment(part.Header, mediaType) {
			body, err := io.ReadAll(io.LimitReader(part.Body, maxTextSize))
			if err != nil && !haveText && !haveHTML {
				return errors.Wrap(err, "failed to read the message text")
			}
			switch {
			case mediaType == "text/plain" && !haveText:
				text, haveText = string(body), true
			case mediaType == "text/html" && !haveHTML:
				htmlSource, haveHTML = string(body), true
			}
			return nil
		}

		// Inline images referenced by the HTML
		if contentID := part.Header.Get("Content-ID"); contentID != "" && strings.HasPrefix(mediaType, "image/") {
			data, err := io.ReadAll(io.LimitReader(part.Body, maxInlineImageSize+1))
			if err == nil && len(data) <= maxInlineImageSize {
				inline[normalizeContentID(contentID)] = inlineImage{contentType: mediaType, data: data}
				disposition, _, _ := part.Header.ContentDisposition()
				if !strings.EqualFold(disposition, "attachment") {
					return nil
				}
			}
		}

		size, _ := io.Copy(io.Discard, part.Body)
		filename := partFilename(part.Header)
		if filename == "" {
			filename = defaultFilename(id, mediaType)
		}
		msg.Attachments = append(msg.Attachments, Attachment{ID: id, Filename: filename, ContentType: mediaType, Size: size})
		return nil
	})
	if err != nil {
		return nil, &apiError{status: http.StatusUnprocessableEntity, err: errors.Wrap(err, "couldn't read the message")}
	}

	if haveHTML {
		msg.HTML, msg.RemoteImages = sanitizeHTML(htmlSource, inline, allowRemoteImages)
	}
	if haveText {
		msg.Text = strings.ToValidUTF8(text, "\uFFFD")
	} else if haveHTML {
		msg.Text = htmlToText(htmlSource)
	}
	return msg, nil
}

// defaultFilename names attachments that have no name.
func defaultFilename(id, mediaType string) string {
	ext := ".bin"
	if mediaType == "message/rfc822" {
		ext = ".eml"
	} else if exts, _ := mime.ExtensionsByType(mediaType); len(exts) > 0 {
		ext = exts[0]
	}
	return "part-" + id + ext
}

// openAttachment returns an attachment of a message and its content.
func openAttachment(raw []byte, id string) (*Attachment, []byte, error) {
	entity, err := message.Read(bytes.NewReader(raw))
	if err != nil && !message.IsUnknownCharset(err) && !message.IsUnknownEncoding(err) {
		return nil, nil, errors.Wrap(err, "couldn't read the message")
	}

	var found *Attachment
	var data []byte
	errFound := errors.New("found")
	err = walkLeaves(entity, func(partID, mediaType string, part *message.Entity) error {
		if partID != id {
			return nil
		}
		content, err := io.ReadAll(part.Body)
		if err != nil {
			return errors.Wrap(err, "failed to read the attachment")
		}
		filename := partFilename(part.Header)
		if filename == "" {
			filename = defaultFilename(partID, mediaType)
		}
		found = &Attachment{ID: partID, Filename: filename, ContentType: mediaType, Size: int64(len(content))}
		data = content
		return errFound
	})
	if err != nil && !errors.Is(err, errFound) {
		return nil, nil, err
	}
	if found == nil {
		return nil, nil, newAPIError(http.StatusNotFound, "attachment not found")
	}
	return found, data, nil
}

// snippetFromPart makes a snippet from the beginning of a text part, as fetched from the IMAP
// server: still in its transfer encoding and charset, and possibly cut in the middle of a
// character or a tag.
func snippetFromPart(raw []byte, encoding, charsetName string, isHTML bool) string {
	var r io.Reader = bytes.NewReader(raw)
	switch strings.ToLower(encoding) {
	case "base64":
		clean := bytes.Map(func(r rune) rune {
			if r == '\r' || r == '\n' || r == ' ' || r == '\t' {
				return -1
			}
			return r
		}, raw)
		clean = clean[:len(clean)/4*4]
		r = base64.NewDecoder(base64.StdEncoding, bytes.NewReader(clean))
	case "quoted-printable":
		r = quotedprintable.NewReader(r)
	}
	if charsetName != "" && !strings.EqualFold(charsetName, "utf-8") && !strings.EqualFold(charsetName, "us-ascii") {
		if converted, err := charset.Reader(charsetName, r); err == nil {
			r = converted
		}
	}

	// Reading stops at the first error, which the cut at the end of the data may cause
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	data := buf.Bytes()
	for len(data) > 0 && !utf8.Valid(data) {
		data = data[:len(data)-1]
	}

	text := string(data)
	if isHTML {
		text = htmlToText(text)
	}
	return makeSnippet(text)
}

// makeSnippet makes a one line summary of a text, without its quotes.
func makeSnippet(text string) string {
	lines := strings.Split(text, "\n")
	kept := make([]string, 0, len(lines))
	for _, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), ">") {
			continue
		}
		kept = append(kept, line)
	}
	snippet := strings.Join(strings.Fields(strings.Join(kept, " ")), " ")
	if utf8.RuneCountInString(snippet) > snippetLength {
		runes := []rune(snippet)
		snippet = strings.TrimSpace(string(runes[:snippetLength])) + "…"
	}
	return snippet
}
