package cmd

import (
	"context"
	"fmt"
	"strconv"
	"strings"
)

// TicketsShowCmd prints one or more tickets and their full conversation
// traces as Markdown.
type TicketsShowCmd struct {
	IDs []int64 `arg:"" help:"Ticket ID(s)"`
}

func (c *TicketsShowCmd) Run(ctx context.Context, client *Client) error {
	for i, id := range c.IDs {
		if i > 0 {
			fmt.Println("\n---")
		}
		doc, err := fetchExportDoc(ctx, client, id)
		if err != nil {
			return err
		}
		if err := gatherMedia(ctx, client, doc); err != nil {
			return err
		}

		data, _, err := renderTicketMarkdown(doc, id)
		if err != nil {
			return err
		}
		fmt.Print(string(data))
	}
	return nil
}

// urgencyImpactName maps numeric urgency/impact values to their display names.
var urgencyImpactName = map[int64]string{1: "Low", 2: "Medium", 3: "High"}

// ticketMetaFields lists the metadata rows shown at the top of a ticket view.
var ticketMetaFields = []struct{ label, key, nameKey string }{
	{"Status", "status", "status_name"},
	{"Priority", "priority", "priority_name"},
	{"Urgency", "urgency", "urgency_name"},
	{"Impact", "impact", "impact_name"},
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

	maxLabel := 0
	for _, f := range ticketMetaFields {
		if len(f.label) > maxLabel {
			maxLabel = len(f.label)
		}
	}
	for _, f := range ticketMetaFields {
		val := exportField(doc.Ticket, f.nameKey)
		if val == "" {
			val = exportField(doc.Ticket, f.key)
		}
		if val != "" && (f.key == "urgency" || f.key == "impact") {
			if n, err := strconv.ParseInt(val, 10, 64); err == nil {
				if name, ok := urgencyImpactName[n]; ok {
					val = name
				}
			}
		}
		fmt.Fprintf(&b, "%-*s : %s\n", maxLabel, f.label, val)
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