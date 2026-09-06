package cmd

import (
	"fmt"
	"path/filepath"
	"strings"
)

// renderMarkdown produces a Markdown document containing the ticket and
// returns both the document and the asset files to write alongside it.
func renderMarkdown(doc *exportDoc) ([]byte, []exportAsset, error) {
	var b strings.Builder

	subject := doc.Subject
	display := doc.DisplayID
	fmt.Fprintf(&b, "# Ticket #%s — %s\n\n", display, subject)

	if desc := stripHTML(doc.DescText); desc != "" {
		fmt.Fprintf(&b, "%s\n\n", desc)
	}
	for _, img := range imagesFor(doc.Images, "ticket") {
		b.WriteString(markdownImage(img))
		b.WriteString("\n")
	}
	writeMarkdownAttachments(&b, doc.Attachments)

	b.WriteString("## Conversations\n\n")
	if len(doc.Conversations) == 0 {
		b.WriteString("(none)\n")
	}
	var assets []exportAsset
	for _, conv := range doc.Conversations {
		writeMarkdownConversation(&b, conv)
		for _, img := range imagesFor(doc.Images, "conv-"+conv.ID) {
			b.WriteString(markdownImage(img))
			b.WriteString("\n")
		}
	}

	for _, img := range doc.Images {
		assets = append(assets, exportAsset{Name: assetRelPath(img), Data: img.Data})
	}

	return []byte(b.String()), assets, nil
}

func writeMarkdownAttachments(b *strings.Builder, atts []exportAttachment) {
	if len(atts) == 0 {
		return
	}
	b.WriteString("## Attachments\n\n")
	for _, a := range atts {
		url := a.URL
		if url == "" {
			url = a.Name
		}
		fmt.Fprintf(b, "- %s (%s, %d bytes): %s\n", a.Name, a.ContentType, a.Size, url)
	}
	b.WriteString("\n")
}

// assetRelPath is the path referenced in the markdown, relative to the .md file.
func assetRelPath(img exportImage) string {
	name := sanitizeID(img.Name)
	if name == "" {
		name = sanitizeID(img.ID)
	}
	ext := imageExt(img.Mime)
	if !strings.Contains(name, ".") {
		name += "." + ext
	}
	return filepath.Join("assets", name)
}

func markdownImage(img exportImage) string {
	return "![](" + assetRelPath(img) + ")"
}

func writeMarkdownConversation(b *strings.Builder, conv conversationDoc) {
	dir := "incoming"
	if !conv.Incoming {
		dir = "outgoing"
	}
	bodyText := stripHTML(conv.BodyText)

	fmt.Fprintf(b, "### %s (%s, %s)\n", conv.Author, dir, conv.At)
	if bodyText != "" {
		fmt.Fprintf(b, "%s\n", bodyText)
	} else {
		b.WriteString("(no body)\n")
	}
	b.WriteString("\n")
}
