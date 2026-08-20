package cmd

import (
	"context"
	"fmt"
	"strings"
)

// TicketsShowCmd prints a ticket and its full conversation trace as Markdown.
type TicketsShowCmd struct {
	ID int64 `arg:"" help:"Ticket ID"`
}

func (c *TicketsShowCmd) Run(ctx context.Context, client *Client) error {
	doc, err := fetchExportDoc(ctx, client, c.ID)
	if err != nil {
		return err
	}
	if err := gatherMedia(ctx, client, doc); err != nil {
		return err
	}

	data, _, err := renderTicketMarkdown(doc, c.ID)
	if err != nil {
		return err
	}
	fmt.Println(string(data))
	return nil
}

// ticketMetaFields lists the metadata rows shown at the top of a ticket view.
var ticketMetaFields = []struct{ label, key, nameKey string }{
	{"Status", "status", "status_name"},
	{"Priority", "priority", "priority_name"},
	{"Urgency", "urgency", ""},
	{"Impact", "impact", ""},
	{"Group", "group_id", "group_name"},
	{"Requester", "requester_id", "requester_name"},
	{"Responder", "responder_id", "responder_name"},
	{"Department", "department_id", "department_name"},
	{"Created", "created_at", ""},
	{"Updated", "updated_at", ""},
}

// renderTicketMarkdown renders a full ticket with its conversation trace as a
// Markdown document. The asset files are returned for callers that save the
// output to disk; stdout callers can discard them.
func renderTicketMarkdown(doc *exportDoc, id int64) ([]byte, []exportAsset, error) {
	var b strings.Builder

	subject := exportField(doc.Ticket, "subject")
	display := exportField(doc.Ticket, "display_id")
	if display == "" {
		display = fmt.Sprintf("%d", id)
		if d := exportField(doc.Ticket, "id"); d != "" {
			display = d
		}
	}
	fmt.Fprintf(&b, "# Ticket #%s — %s\n\n", display, subject)

	b.WriteString("| Field | Value\n|---|---\n")
	for _, f := range ticketMetaFields {
		val := exportField(doc.Ticket, f.nameKey)
		if val == "" {
			val = exportField(doc.Ticket, f.key)
		}
		fmt.Fprintf(&b, "| %s | %s\n", f.label, val)
	}
	b.WriteString("\n")

	if desc := stripHTML(exportField(doc.Ticket, "description_text")); desc != "" {
		fmt.Fprintf(&b, "%s\n\n", desc)
	}
	for _, img := range imagesFor(doc.Images, "ticket") {
		b.WriteString(markdownImage("show", img))
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
			b.WriteString(markdownImage("show", img))
			b.WriteString("\n")
		}
	}

	for _, img := range doc.Images {
		assets = append(assets, exportAsset{Name: assetRelPath("show", img), Data: img.Data})
	}

	return []byte(b.String()), assets, nil
}