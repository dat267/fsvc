package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type TicketsExportCmd struct {
	ID  int64  `arg:"" help:"Ticket ID"`
	Out string `short:"o" help:"Output file (.docx, .md, or .html)" required:""`
}

// exportDoc is the data the exporters render. The typed fields are parsed
// once from Ticket/Conversations raw maps; renderers consume the typed view.
type exportDoc struct {
	Ticket        map[string]any // raw ticket; show's meta table reads name keys from it
	Subject       string
	DisplayID     string // display_id, falling back to id
	DescHTML      string
	DescText      string
	Conversations []conversationDoc
	Images        []exportImage
	Attachments   []exportAttachment
}

// conversationDoc is the typed per-conversation view renderers consume.
type conversationDoc struct {
	ID          string
	Author      string // nested user name when present, else user_id
	Incoming    bool
	At          string
	BodyHTML    string
	BodyText    string
	Attachments []map[string]any
}

// exportImage is a downloaded image to embed in an export. Owner is "ticket"
// or "conv-<id>" and determines which section renders the image.
type exportImage struct {
	ID    string
	Data  []byte
	Mime  string
	Name  string
	Owner string
}

// exportAttachment is non-image attachment metadata (not downloaded).
type exportAttachment struct {
	ID          string
	Name        string
	ContentType string
	Size        int64
	URL         string
}

// exportAsset is a file written alongside a markdown export.
type exportAsset struct {
	Name string
	Data []byte
}

func (c *TicketsExportCmd) Run(ctx context.Context, client *Client) error {
	ext := strings.ToLower(filepath.Ext(c.Out))
	if ext != ".docx" && ext != ".md" && ext != ".html" {
		return fmt.Errorf("unsupported output format %q (use .docx, .md, or .html)", ext)
	}

	doc, err := fetchExportDoc(ctx, client, c.ID)
	if err != nil {
		return err
	}
	if err := gatherMedia(ctx, client, doc); err != nil {
		return err
	}

	var data []byte
	switch ext {
	case ".docx":
		data, err = renderDocx(doc)
	case ".md":
		var assets []exportAsset
		var merr error
		data, assets, merr = renderMarkdown(doc)
		if merr != nil {
			return merr
		}
		if werr := writeAssets(c.Out, assets); werr != nil {
			return werr
		}
	case ".html":
		data, err = renderHTML(doc)
	}
	if err != nil {
		return err
	}

	if err := os.WriteFile(c.Out, data, 0644); err != nil {
		return fmt.Errorf("write %s: %w", c.Out, err)
	}
	fmt.Printf("Wrote %s\n", c.Out)
	return nil
}

// writeAssets writes asset files (e.g. markdown images) next to the output.
func writeAssets(outPath string, assets []exportAsset) error {
	for _, a := range assets {
		p := filepath.Join(filepath.Dir(outPath), a.Name)
		if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
			return fmt.Errorf("create %s: %w", filepath.Dir(p), err)
		}
		if err := os.WriteFile(p, a.Data, 0644); err != nil {
			return fmt.Errorf("write %s: %w", p, err)
		}
	}
	return nil
}

// parseExportDoc extracts the typed view renderers consume from the raw
// ticket and conversation maps.
func parseExportDoc(ticket map[string]any, convs []map[string]any) *exportDoc {
	doc := &exportDoc{Ticket: ticket}
	doc.Subject = exportField(ticket, "subject")
	doc.DisplayID = exportField(ticket, "display_id")
	if doc.DisplayID == "" {
		doc.DisplayID = exportField(ticket, "id")
	}
	doc.DescHTML = exportField(ticket, "description")
	doc.DescText = exportField(ticket, "description_text")
	doc.Conversations = make([]conversationDoc, len(convs))
	for i, c := range convs {
		doc.Conversations[i] = conversationDoc{
			ID:          exportField(c, "id"),
			Author:      conversationAuthor(c),
			Incoming:    c["incoming"] == true,
			At:          exportField(c, "created_at"),
			BodyHTML:    exportField(c, "body"),
			BodyText:    exportField(c, "body_text"),
			Attachments: attachmentsOf(c),
		}
	}
	return doc
}

// attachmentsOf extracts the attachments array from a ticket/conversation map.
func attachmentsOf(obj map[string]any) []map[string]any {
	raw, _ := obj["attachments"].([]any)
	out := make([]map[string]any, 0, len(raw))
	for _, a := range raw {
		if m, ok := a.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}

// fetchExportDoc pulls the full ticket and its conversations.
func fetchExportDoc(ctx context.Context, client *Client, id int64) (*exportDoc, error) {
	raw, err := client.Get(ctx, fmt.Sprintf("tickets/%d", id), nil)
	if err != nil {
		return nil, err
	}
	var ticketResp struct {
		Ticket map[string]any `json:"ticket"`
	}
	if err := json.Unmarshal(raw, &ticketResp); err != nil {
		return nil, fmt.Errorf("parse ticket: %w", err)
	}

	convRaw, err := client.Get(ctx, fmt.Sprintf("tickets/%d/conversations", id), url.Values{
		"per_page":   {"100"},
		"order_by":   {"created_at"},
		"order_type": {"asc"},
	})
	if err != nil {
		return nil, err
	}
	var convResp struct {
		Conversations []map[string]any `json:"conversations"`
	}
	if err := json.Unmarshal(convRaw, &convResp); err != nil {
		return nil, fmt.Errorf("parse conversations: %w", err)
	}

	return parseExportDoc(ticketResp.Ticket, convResp.Conversations), nil
}

// exportField returns a readable value for a ticket key, or "".
func exportField(t map[string]any, key string) string {
	v, ok := t[key]
	if !ok || v == nil {
		return ""
	}
	switch val := v.(type) {
	case string:
		return val
	case float64:
		// Render whole numbers without scientific notation (e.g. user IDs).
		if val == math.Trunc(val) && math.Abs(val) < 1e15 {
			return strconv.FormatInt(int64(val), 10)
		}
		return strconv.FormatFloat(val, 'f', -1, 64)
	case bool:
		return fmt.Sprintf("%v", val)
	default:
		b, err := json.Marshal(val)
		if err != nil {
			return fmt.Sprintf("%v", val)
		}
		return string(b)
	}
}

// stripHTML removes tags from API HTML fields and decodes HTML entities,
// leaving plain text.
func stripHTML(s string) string {
	var b strings.Builder
	inTag := false
	for _, r := range s {
		switch {
		case r == '<':
			inTag = true
		case r == '>':
			inTag = false
		case !inTag:
			b.WriteRune(r)
		}
	}
	return strings.TrimSpace(html.UnescapeString(b.String()))
}

// conversationAuthor returns the display name (or id) of a conversation author,
// preferring the nested user object when present.
func conversationAuthor(c map[string]any) string {
	if u, ok := c["user"].(map[string]any); ok {
		if name := exportField(u, "name"); name != "" {
			return name
		}
	}
	return exportField(c, "user_id")
}
