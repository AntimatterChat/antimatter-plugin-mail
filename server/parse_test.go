// Copyright (c) 2026-present Antimatter contributors.
// See LICENSE.txt for license information.

package main

import (
	"encoding/base64"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func crlf(s string) []byte {
	return []byte(strings.ReplaceAll(s, "\n", "\r\n"))
}

const pixelPNG = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg=="

var multipartMessage = crlf(`From: =?utf-8?q?Marie_Laurent?= <marie@example.com>
To: Ada <ada@example.com>, lena@example.com
Cc: Kenji <kenji@example.com>
Subject: =?utf-8?q?Beam_schedule_draft_=E2=80=94_v2?=
Date: Tue, 29 Sep 2026 10:31:00 +0200
Message-ID: <draft-2@example.com>
In-Reply-To: <draft-1@example.com>
References: <draft-0@example.com> <draft-1@example.com>
MIME-Version: 1.0
Content-Type: multipart/mixed; boundary="outer"

--outer
Content-Type: multipart/alternative; boundary="alt"

--alt
Content-Type: text/plain; charset=utf-8
Content-Transfer-Encoding: quoted-printable

Attached the draft for Wed and Fri, tell me if it works =E2=80=94 Marie

> Earlier message
--alt
Content-Type: multipart/related; boundary="rel"

--rel
Content-Type: text/html; charset=utf-8

<html><head><title>Hidden title</title><style>p { color: red; }</style></head>
<body><p style="color: #333; position: fixed">Attached the <b>draft</b>.</p>
<script>alert(1)</script>
<img src="cid:logo@example.com" alt="logo">
<img src="https://tracker.example.com/open.gif">
<a href="https://example.com/schedule" onclick="steal()">schedule</a>
<a href="javascript:alert(1)">bad</a>
<form action="https://evil.example.com"><input name="password"></form>
<table background="https://tracker.example.com/bg.png" bgcolor="#ffffff" width="100%"><tr><td>cell</td></tr></table>
</body></html>
--rel
Content-Type: image/png
Content-ID: <logo@example.com>
Content-Transfer-Encoding: base64

` + pixelPNG + `
--rel--
--alt--
--outer
Content-Type: application/pdf; name="schedule.pdf"
Content-Disposition: attachment; filename="schedule.pdf"
Content-Transfer-Encoding: base64

` + base64.StdEncoding.EncodeToString([]byte("%PDF-1.4 fake")) + `
--outer--
`)

func TestParseMessage(t *testing.T) {
	msg, err := parseMessage(multipartMessage, false)
	require.NoError(t, err)

	assert.Equal(t, []Address{{Name: "Marie Laurent", Email: "marie@example.com"}}, msg.From)
	assert.Equal(t, []Address{{Name: "Ada", Email: "ada@example.com"}, {Email: "lena@example.com"}}, msg.To)
	assert.Equal(t, []Address{{Name: "Kenji", Email: "kenji@example.com"}}, msg.Cc)
	assert.Equal(t, "Beam schedule draft — v2", msg.Subject)
	assert.Equal(t, int64(1790670660000), msg.Date)
	assert.Equal(t, "draft-2@example.com", msg.MessageID)
	assert.Equal(t, []string{"draft-1@example.com"}, msg.InReplyTo)
	assert.Equal(t, []string{"draft-0@example.com", "draft-1@example.com"}, msg.References)

	assert.Contains(t, msg.Text, "tell me if it works — Marie")

	// The inline image is embedded, the remote one blocked
	assert.Contains(t, msg.HTML, `src="data:image/png;base64,`)
	assert.NotContains(t, msg.HTML, "tracker.example.com")
	assert.Equal(t, 1, msg.RemoteImages)

	// Scripts, handlers, forms, style sheets and unsafe styles are dropped
	assert.NotContains(t, msg.HTML, "alert")
	assert.NotContains(t, msg.HTML, "steal")
	assert.NotContains(t, msg.HTML, "<form")
	assert.NotContains(t, msg.HTML, "<input")
	assert.NotContains(t, msg.HTML, "<style")
	assert.NotContains(t, msg.HTML, "Hidden title")
	assert.NotContains(t, msg.HTML, "position")
	assert.Contains(t, msg.HTML, "color: #333")
	assert.Contains(t, msg.HTML, `bgcolor="#ffffff"`)
	assert.Contains(t, msg.HTML, `<b>draft</b>`)

	// Links open in a new tab, without a referrer
	assert.Contains(t, msg.HTML, `href="https://example.com/schedule"`)
	assert.Contains(t, msg.HTML, `target="_blank"`)
	assert.Contains(t, msg.HTML, `noreferrer`)

	// The inline image isn't listed as an attachment, the PDF is
	require.Len(t, msg.Attachments, 1)
	assert.Equal(t, Attachment{ID: "2", Filename: "schedule.pdf", ContentType: "application/pdf", Size: 13}, msg.Attachments[0])

	attachment, data, err := openAttachment(multipartMessage, "2")
	require.NoError(t, err)
	assert.Equal(t, "schedule.pdf", attachment.Filename)
	assert.Equal(t, "%PDF-1.4 fake", string(data))

	_, _, err = openAttachment(multipartMessage, "9")
	assert.Equal(t, 404, statusFor(err))
}

func TestParseMessageAllowsRemoteImages(t *testing.T) {
	msg, err := parseMessage(multipartMessage, true)
	require.NoError(t, err)
	assert.Contains(t, msg.HTML, `src="https://tracker.example.com/open.gif"`)
	assert.Equal(t, 0, msg.RemoteImages)

	// Background images are always dropped
	assert.NotContains(t, msg.HTML, "bg.png")
}

func TestParseHTMLOnlyMessage(t *testing.T) {
	raw := crlf(`From: shop@example.com
Subject: Order 7731 shipped
Content-Type: text/html; charset=iso-8859-1
Content-Transfer-Encoding: quoted-printable

<p>Your order of 4 CF flanges has shipped.</p><ul><li>Caf=E9</li></ul>
`)
	msg, err := parseMessage(raw, false)
	require.NoError(t, err)
	assert.Equal(t, "Your order of 4 CF flanges has shipped.\n\n- Café", msg.Text)
	assert.Contains(t, msg.HTML, "Café")
	assert.Empty(t, msg.Attachments)
}

func TestParsePlainMessage(t *testing.T) {
	raw := crlf(`From: Facility Office <facility@example.com>
Subject: Hall B access Friday

Badges will be re-issued at the front desk from 8:00.
`)
	msg, err := parseMessage(raw, false)
	require.NoError(t, err)
	assert.Equal(t, "Badges will be re-issued at the front desk from 8:00.\r\n", msg.Text)
	assert.Empty(t, msg.HTML)
	assert.Equal(t, "Hall B access Friday", msg.Subject)
}

func TestSnippetFromPart(t *testing.T) {
	assert.Equal(t, "Tell me if it works — Marie", snippetFromPart([]byte("Tell me if it works =E2=80=94 Marie\r\n> quoted =\r\nline\r\n"), "quoted-printable", "utf-8", false))

	// Cut in the middle of a base64 quantum and of a character
	encoded := base64.StdEncoding.EncodeToString([]byte("Résumé attached, see the résumé"))
	assert.Equal(t, "Résumé attached, see the", snippetFromPart([]byte(encoded[:37]), "base64", "", false))
	assert.Equal(t, "caf", snippetFromPart([]byte("caf\xc3"), "", "", false))

	assert.Equal(t, "Café au lait", snippetFromPart([]byte("Caf\xe9 au lait"), "8bit", "iso-8859-1", false))
	assert.Equal(t, "Hello world", snippetFromPart([]byte("<html><head><style>x{}</style></head><body><p>Hello</p><p>world</p><img src="), "7bit", "", true))

	long := strings.Repeat("word ", 100)
	snippet := snippetFromPart([]byte(long), "", "", false)
	assert.True(t, strings.HasSuffix(snippet, "…"))
	assert.LessOrEqual(t, len([]rune(snippet)), snippetLength+1)
}

func TestHTMLToText(t *testing.T) {
	assert.Equal(t, "Title\n\nFirst paragraph with bold.\n\n- one\n- two\n\na b", htmlToText(`<h1>Title</h1><p>First   paragraph
with <b>bold</b>.</p><ul><li>one</li><li>two</li></ul><table><tr><td>a</td><td>b</td></tr></table><script>x()</script>`))
}
