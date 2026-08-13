package cmd

import (
	"encoding/base64"
	"strings"
	"testing"
)

func TestRenderHTML(t *testing.T) {
	doc := &exportDoc{
		Ticket: map[string]any{
			"display_id":  "10100",
			"subject":     "Printer not working",
			"description": `<p>Broken</p><img src="http://acme.freshservice.com/a.png">`,
		},
		Conversations: []map[string]any{
			{
				"id":        "1",
				"user_id":   "2100",
				"incoming":  true,
				"created_at": "2026-08-01T10:30:00Z",
				"body":      `<p>See this</p><img src="http://acme.freshservice.com/b.png">`,
			},
		},
		Images: []exportImage{
			{ID: "http://acme.freshservice.com/a.png", Data: []byte("aaaa"), Mime: "image/png", Owner: "ticket"},
			{ID: "http://acme.freshservice.com/b.png", Data: []byte("bbbb"), Mime: "image/png", Owner: "conv-1"},
		},
		Attachments: []exportAttachment{
			{ID: "501", Name: "manual.pdf", ContentType: "application/pdf", Size: 99, URL: "http://acme.freshservice.com/helpdesk/attachments/501"},
		},
	}

	out, err := renderHTML(doc)
	if err != nil {
		t.Fatalf("renderHTML: %v", err)
	}
	md := string(out)

	if !strings.Contains(md, "<h1>Ticket #10100 — Printer not working</h1>") {
		t.Errorf("expected h1 title, got:\n%s", md)
	}
	if !strings.Contains(md, "<p>Broken</p>") {
		t.Errorf("expected description body, got:\n%s", md)
	}
	// Image srcs rewritten to base64 data URIs.
	if !strings.Contains(md, `src="data:image/png;base64,`+base64.StdEncoding.EncodeToString([]byte("aaaa"))+`"`) {
		t.Errorf("expected base64 data URI for ticket image, got:\n%s", md)
	}
	if !strings.Contains(md, `src="data:image/png;base64,`+base64.StdEncoding.EncodeToString([]byte("bbbb"))+`"`) {
		t.Errorf("expected base64 data URI for conversation image, got:\n%s", md)
	}
	if strings.Contains(md, "http://acme.freshservice.com/a.png\"") {
		t.Errorf("expected original ticket img src rewritten, got:\n%s", md)
	}
	// Conversation header + attachment list.
	if !strings.Contains(md, "<h3>2100 (incoming, 2026-08-01T10:30:00Z)</h3>") {
		t.Errorf("expected conversation h3 header, got:\n%s", md)
	}
	if !strings.Contains(md, "<ul>") || !strings.Contains(md, "manual.pdf") || !strings.Contains(md, "application/pdf") {
		t.Errorf("expected attachment list, got:\n%s", md)
	}
}

func TestRenderHTML_AppendsAttachmentOnlyImages(t *testing.T) {
	doc := &exportDoc{
		Ticket: map[string]any{
			"display_id":  "10100",
			"subject":     "Printer not working",
			"description": `<p>Broken</p>`,
		},
		Conversations: nil,
		Images: []exportImage{
			{ID: "http://acme.freshservice.com/helpdesk/attachments/500", Data: []byte("aaaa"), Mime: "image/png", Name: "printer.png", Owner: "ticket"},
		},
	}

	out, err := renderHTML(doc)
	if err != nil {
		t.Fatalf("renderHTML: %v", err)
	}
	md := string(out)
	want := `<img src="data:image/png;base64,` + base64.StdEncoding.EncodeToString([]byte("aaaa")) + `" alt="printer.png">`
	if !strings.Contains(md, want) {
		t.Errorf("expected appended attachment image, got:\n%s", md)
	}
}
