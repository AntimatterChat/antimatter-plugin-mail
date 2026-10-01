// Copyright (c) 2026-present Antimatter contributors.
// See LICENSE.txt for license information.

package main

import (
	"bytes"
	"encoding/base64"
	"regexp"
	"strings"

	"github.com/microcosm-cc/bluemonday"
	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// maxInlineImageSize is the size of the largest inline (cid:) image embedded in a message's HTML.
const maxInlineImageSize = 2 << 20

var (
	colorPattern    = regexp.MustCompile(`^(#[0-9a-fA-F]{3,8}|[a-zA-Z]{3,20})$`)
	sizePattern     = regexp.MustCompile(`^[0-9]{1,4}(%|px)?$`)
	numberPattern   = regexp.MustCompile(`^[0-9]{1,3}$`)
	alignPattern    = regexp.MustCompile(`^(?i)(left|right|center|justify)$`)
	valignPattern   = regexp.MustCompile(`^(?i)(top|middle|bottom|baseline)$`)
	fontFacePattern = regexp.MustCompile(`^[a-zA-Z0-9 ,'"-]{1,100}$`)
	fontSizePattern = regexp.MustCompile(`^[+-]?[1-7]$`)
)

// htmlPolicy is what's kept of the HTML of messages: the formatting of user generated content,
// plus the table layouts and presentational attributes mail commonly uses. Scripts, forms,
// frames, embedded objects, style sheets and remote resources other than images are dropped.
var htmlPolicy = newHTMLPolicy()

func newHTMLPolicy() *bluemonday.Policy {
	p := bluemonday.UGCPolicy()

	// Links open in a new tab, without telling the target where they come from. Relative links
	// mean nothing out of the message.
	p.AllowRelativeURLs(false)
	p.RequireNoReferrerOnLinks(true)
	p.AddTargetBlankToFullyQualifiedLinks(true)

	// Inline (cid:) images are embedded as data URIs.
	p.AllowDataURIImages()

	p.AllowElements("font", "center")
	p.AllowAttrs("color").Matching(colorPattern).OnElements("font")
	p.AllowAttrs("face").Matching(fontFacePattern).OnElements("font")
	p.AllowAttrs("size").Matching(fontSizePattern).OnElements("font")
	p.AllowAttrs("bgcolor").Matching(colorPattern).OnElements("table", "tr", "td", "th")
	p.AllowAttrs("width", "height").Matching(sizePattern).OnElements("table", "tr", "td", "th", "col", "img")
	p.AllowAttrs("align").Matching(alignPattern).OnElements("table", "tr", "td", "th", "p", "div", "h1", "h2", "h3", "h4", "h5", "h6")
	p.AllowAttrs("valign").Matching(valignPattern).OnElements("tr", "td", "th")
	p.AllowAttrs("border", "cellpadding", "cellspacing").Matching(numberPattern).OnElements("table")
	p.AllowAttrs("colspan", "rowspan").Matching(numberPattern).OnElements("td", "th")

	// Styles that can't load anything
	p.AllowStyles(
		"color", "background-color",
		"font-family", "font-size", "font-style", "font-weight", "line-height", "letter-spacing",
		"text-align", "text-decoration", "text-transform", "vertical-align", "white-space",
		"margin", "margin-top", "margin-right", "margin-bottom", "margin-left",
		"padding", "padding-top", "padding-right", "padding-bottom", "padding-left",
		"border", "border-top", "border-right", "border-bottom", "border-left",
		"border-color", "border-style", "border-width", "border-radius", "border-collapse",
		"width", "max-width", "min-width", "height", "display",
	).Globally()

	// The content of the head (title, style sheets) isn't part of the message's text.
	p.SkipElementsContent("head", "title")
	return p
}

// inlineImage is an image a message embeds, referenced from its HTML by a cid: URL.
type inlineImage struct {
	contentType string
	data        []byte
}

// prepareHTML rewrites the image references of a message's HTML before it's sanitised: inline
// images become data URIs, and remote images are dropped unless allowed. It returns the number of
// remote images that were dropped.
func prepareHTML(source string, inline map[string]inlineImage, allowRemote bool) (string, int) {
	doc, err := html.Parse(strings.NewReader(source))
	if err != nil {
		return source, 0
	}

	blocked := 0
	var walk func(n *html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			attrs := n.Attr[:0]
			for _, attr := range n.Attr {
				key := strings.ToLower(attr.Key)
				switch {
				case key == "srcset" || key == "background":
					// Remote images we don't look into
					continue
				case n.DataAtom == atom.Img && key == "src":
					src, ok := imageSource(attr.Val, inline, allowRemote)
					if !ok {
						blocked++
						continue
					}
					if src == "" {
						continue
					}
					attr.Val = src
				}
				attrs = append(attrs, attr)
			}
			n.Attr = attrs
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)

	var buf bytes.Buffer
	if err := html.Render(&buf, doc); err != nil {
		return source, 0
	}
	return buf.String(), blocked
}

// imageSource returns the source to use for an image. It returns ok = false for blocked remote
// images, and an empty source for images that can't be shown.
func imageSource(src string, inline map[string]inlineImage, allowRemote bool) (string, bool) {
	src = strings.TrimSpace(src)
	lower := strings.ToLower(src)
	switch {
	case strings.HasPrefix(lower, "cid:"):
		image, ok := inline[normalizeContentID(src[len("cid:"):])]
		if !ok {
			return "", true
		}
		return "data:" + image.contentType + ";base64," + base64.StdEncoding.EncodeToString(image.data), true
	case strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://"):
		if !allowRemote {
			return "", false
		}
		return src, true
	case strings.HasPrefix(lower, "data:image/"):
		return src, true
	default:
		return "", true
	}
}

// normalizeContentID turns a Content-ID header or a cid: URL into a map key.
func normalizeContentID(id string) string {
	id = strings.TrimSpace(id)
	id = strings.TrimPrefix(id, "<")
	id = strings.TrimSuffix(id, ">")
	return strings.ToLower(id)
}

// sanitizeHTML returns the HTML of a message, safe to show in a sandboxed frame, and the number of
// remote images that were blocked.
func sanitizeHTML(source string, inline map[string]inlineImage, allowRemote bool) (string, int) {
	prepared, blocked := prepareHTML(source, inline, allowRemote)
	return htmlPolicy.Sanitize(prepared), blocked
}

var blockElements = map[atom.Atom]bool{
	atom.P: true, atom.Div: true, atom.Br: true, atom.Tr: true, atom.Li: true, atom.Table: true,
	atom.H1: true, atom.H2: true, atom.H3: true, atom.H4: true, atom.H5: true, atom.H6: true,
	atom.Blockquote: true, atom.Hr: true, atom.Pre: true, atom.Ul: true, atom.Ol: true,
}

var (
	spacesPattern   = regexp.MustCompile(`[ \t\r\f\v\x{00a0}]+`)
	newlinesPattern = regexp.MustCompile(`\n\s*\n(\s*\n)+`)
)

// htmlToText returns the text of an HTML document, keeping paragraphs.
func htmlToText(source string) string {
	tokenizer := html.NewTokenizer(strings.NewReader(source))
	var sb strings.Builder
	skip := 0
	for {
		switch tokenizer.Next() {
		case html.ErrorToken:
			text := spacesPattern.ReplaceAllString(sb.String(), " ")
			lines := strings.Split(text, "\n")
			for i := range lines {
				lines[i] = strings.TrimSpace(lines[i])
			}
			text = newlinesPattern.ReplaceAllString(strings.Join(lines, "\n"), "\n\n")
			return strings.TrimSpace(text)
		case html.TextToken:
			if skip == 0 {
				sb.WriteString(strings.ReplaceAll(string(tokenizer.Text()), "\n", " "))
			}
		case html.StartTagToken, html.SelfClosingTagToken:
			name, _ := tokenizer.TagName()
			a := atom.Lookup(name)
			switch {
			case a == atom.Script || a == atom.Style || a == atom.Head || a == atom.Title:
				skip++
			case a == atom.Li:
				sb.WriteString("\n- ")
			case a == atom.Td || a == atom.Th:
				sb.WriteString(" ")
			case blockElements[a]:
				sb.WriteString("\n")
			}
		case html.EndTagToken:
			name, _ := tokenizer.TagName()
			a := atom.Lookup(name)
			switch {
			case a == atom.Script || a == atom.Style || a == atom.Head || a == atom.Title:
				if skip > 0 {
					skip--
				}
			case blockElements[a] && a != atom.Li:
				sb.WriteString("\n")
			}
		}
	}
}
