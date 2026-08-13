package cmd

import (
	"fmt"
	"path/filepath"
	"strings"
)

// renderMarkdown produces a Markdown document containing the ticket, writing
// images to assets/ alongside outPath and returning both the document and the
// asset files to write.
func renderMarkdown(doc *exportDoc, outPath string) ([]byte, []exportAsset, error) {
	var b strings.Builder

	subject := exportField(doc.Ticket, "subject")
	display := exportField(doc.Ticket, "display_id")
	if display == "" {
		display = exportField(doc.Ticket, "id")
	}
	fmt.Fprintf(&b, "# Ticket #%s — %s\n\n", display, subject)

	if desc := stripHTML(exportField(doc.Ticket, "description_text")); desc != "" {
		fmt.Fprintf(&b, "%s\n\n", desc)
	}
	for _, img := range imagesFor(doc.Images, "ticket") {
		b.WriteString(markdownImage(outPath, img))
		b.WriteString("\n")
	}
	writeMarkdownAttachments(&b, doc.Attachments)

	b.WriteString("## Conversations\n\n")
	if len(doc.Conversations) == 0 {
		b.WriteString("(none)\n")
	}
	var assets []exportAsset
	for _, c := range doc.Conversations {
		writeMarkdownConversation(&b, c)
		for _, img := range imagesFor(doc.Images, "conv-"+exportField(c, "id")) {
			b.WriteString(markdownImage(outPath, img))
			b.WriteString("\n")
		}
	}

	for _, img := range doc.Images {
		assets = append(assets, exportAsset{Name: assetRelPath(outPath, img), Data: img.Data})
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
func assetRelPath(outPath string, img exportImage) string {
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

func markdownImage(outPath string, img exportImage) string {
	return "![](" + assetRelPath(outPath, img) + ")"
}

func writeMarkdownConversation(b *strings.Builder, c map[string]any) {
	dir := "incoming"
	if incoming, ok := c["incoming"].(bool); ok && !incoming {
		dir = "outgoing"
	}
	author := conversationAuthor(c)
	stamp := exportField(c, "created_at")
	bodyText := stripHTML(exportField(c, "body_text"))

	fmt.Fprintf(b, "### %s (%s, %s)\n", author, dir, stamp)
	if bodyText != "" {
		fmt.Fprintf(b, "%s\n", bodyText)
	} else {
		b.WriteString("(no body)\n")
	}
	b.WriteString("\n")
}
