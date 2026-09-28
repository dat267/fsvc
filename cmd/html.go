package cmd

import (
	"encoding/base64"
	"fmt"
	"strings"
)

// renderHTML produces a bare HTML fragment of the ticket, embedding images as
// base64 data URIs.
func renderHTML(doc *exportDoc) ([]byte, error) {
	// dataURI maps resolved image URL -> data URI.
	dataURI := map[string]string{}
	for _, img := range doc.Images {
		dataURI[img.ID] = "data:" + img.Mime + ";base64," + base64.StdEncoding.EncodeToString(img.Data)
	}

	var b strings.Builder
	subject := doc.Subject
	display := doc.DisplayID
	fmt.Fprintf(&b, "<h1>Ticket #%s — %s</h1>\n", display, subject)

	if desc := doc.DescHTML; desc != "" {
		b.WriteString(rewriteImageSrcs(desc, dataURI))
		b.WriteString("\n")
	} else if desc := stripHTML(doc.DescText); desc != "" {
		fmt.Fprintf(&b, "<p>%s</p>\n", desc)
	}
	writeImagesNotInlined(&b, doc, "ticket", dataURI, doc.DescHTML)

	writeHTMLAttachments(&b, doc.Attachments)

	fmt.Fprintf(&b, "<h2>Conversations</h2>\n")
	if len(doc.Conversations) == 0 {
		b.WriteString("<p>(none)</p>\n")
	}
	for _, conv := range doc.Conversations {
		writeHTMLConversation(&b, conv, dataURI)
		writeImagesNotInlined(&b, doc, "conv-"+conv.ID, dataURI, conv.BodyHTML)
	}
	return []byte(b.String()), nil
}

// writeImagesNotInlined appends <img> tags for an owner's images that were not
// referenced inline in the given HTML body (e.g. attachment-only images).
func writeImagesNotInlined(b *strings.Builder, doc *exportDoc, owner string, dataURI map[string]string, bodyHTML string) {
	for _, img := range imagesFor(doc.Images, owner) {
		if strings.Contains(bodyHTML, img.ID) {
			continue
		}
		fmt.Fprintf(b, "<img src=\"%s\" alt=\"%s\">\n", dataURI[img.ID], img.Name)
	}
}

// rewriteImageSrcs replaces each <img src> in an HTML string with its base64
// data URI. Unknown srcs are left untouched.
func rewriteImageSrcs(html string, dataURI map[string]string) string {
	return imgSrcRe.ReplaceAllStringFunc(html, func(tag string) string {
		sub := imgSrcRe.FindStringSubmatch(tag)
		if uri, ok := dataURI[sub[1]]; ok {
			return strings.Replace(tag, sub[1], uri, 1)
		}
		return tag
	})
}

func writeHTMLAttachments(b *strings.Builder, atts []exportAttachment) {
	if len(atts) == 0 {
		return
	}
	b.WriteString("<h2>Attachments</h2>\n<ul>\n")
	for _, a := range atts {
		url := a.URL
		if url == "" {
			url = a.Name
		}
		fmt.Fprintf(b, "<li><a href=\"%s\">%s</a> (%s, %d bytes)</li>\n", url, a.Name, a.ContentType, a.Size)
	}
	b.WriteString("</ul>\n")
}

func writeHTMLConversation(b *strings.Builder, conv conversationDoc, dataURI map[string]string) {
	dir := "incoming"
	if !conv.Incoming {
		dir = "outgoing"
	}
	fmt.Fprintf(b, "<h3>%s (%s, %s)</h3>\n", conv.Author, dir, conv.At)

	if body := conv.BodyHTML; body != "" {
		b.WriteString(rewriteImageSrcs(body, dataURI))
		b.WriteString("\n")
	} else if body := stripHTML(conv.BodyText); body != "" {
		fmt.Fprintf(b, "<p>%s</p>\n", body)
	} else {
		b.WriteString("<p>(no body)</p>\n")
	}
}
