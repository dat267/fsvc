package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"os"
	"path/filepath"
	"strings"
)

type TicketsExportCmd struct {
	ID   int64  `arg:"" help:"Ticket ID"`
	Out  string `short:"o" help:"Output file (.docx or .pdf)" required:""`
	Tags bool   `help:"Include raw tag values in the summary"`
}

// exportDoc is the data the exporters render.
type exportDoc struct {
	Ticket        map[string]any
	Conversations []map[string]any
}

func (c *TicketsExportCmd) Run(ctx context.Context, client *Client) error {
	ext := strings.ToLower(filepath.Ext(c.Out))
	if ext != ".docx" && ext != ".pdf" {
		return fmt.Errorf("unsupported output format %q (use .docx or .pdf)", ext)
	}

	doc, err := fetchExportDoc(ctx, client, c.ID)
	if err != nil {
		return err
	}

	var data []byte
	switch ext {
	case ".docx":
		data, err = renderDocx(doc, c.Tags)
	case ".pdf":
		data, err = renderPDF(doc, c.Tags)
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

	convRaw, err := client.Get(ctx, fmt.Sprintf("tickets/%d/conversations", id), nil)
	if err != nil {
		return nil, err
	}
	var convResp struct {
		Conversations []map[string]any `json:"conversations"`
	}
	if err := json.Unmarshal(convRaw, &convResp); err != nil {
		return nil, fmt.Errorf("parse conversations: %w", err)
	}

	return &exportDoc{Ticket: ticketResp.Ticket, Conversations: convResp.Conversations}, nil
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
		return fmt.Sprintf("%g", val)
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
