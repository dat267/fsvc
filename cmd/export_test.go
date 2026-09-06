package cmd

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func testExportFixture(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/_/tickets/10100", func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/conversations") {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ticket":{"id":10100,"display_id":10100,"subject":"Printer not working","status":2,"priority":2,"requester_name":"Omar Saleh","responder_name":"Nadia Rahman","created_at":"2026-08-01T10:00:00Z","description_text":"<p>Hello &amp; hi</p>"}}`))
		}
	})
	mux.HandleFunc("/api/_/tickets/10100/conversations", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"conversations":[{"id":1,"user_id":2100,"incoming":true,"created_at":"2026-08-01T10:30:00Z","body_text":"<p>Please fix the printer</p>"},{"id":2,"user_id":3100,"user":{"name":"Nadia Rahman"},"incoming":false,"created_at":"2026-08-01T11:00:00Z","body_text":"<p>Will do</p>"}]}`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func TestTicketsExportCmd_Docx(t *testing.T) {
	srv := testExportFixture(t)
	out := filepath.Join(t.TempDir(), "ticket.docx")

	err := (&TicketsExportCmd{ID: 10100, Out: out}).Run(context.Background(), newTestClient(srv.URL))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("failed to read output: %v", err)
	}
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("output is not a valid zip: %v", err)
	}
	var docXML string
	for _, f := range zr.File {
		if f.Name == "word/document.xml" {
			rc, err := f.Open()
			if err != nil {
				t.Fatal(err)
			}
			buf := new(bytes.Buffer)
			_, _ = buf.ReadFrom(rc)
			_ = rc.Close()
			docXML = buf.String()
		}
	}
	if docXML == "" {
		t.Fatal("missing word/document.xml")
	}
	for _, want := range []string{"Printer not working", "Hello &amp; hi", "Conversations", "Please fix the printer", "Nadia Rahman", "2100"} {
		if !strings.Contains(docXML, want) {
			t.Errorf("expected %q in docx, got:\n%s", want, docXML)
		}
	}
	// HTML should be stripped.
	if strings.Contains(docXML, "<p>") {
		t.Errorf("expected HTML stripped from docx, got:\n%s", docXML)
	}
}

func TestTicketsExportCmd_Markdown(t *testing.T) {
	srv := testExportFixture(t)
	out := filepath.Join(t.TempDir(), "ticket.md")

	err := (&TicketsExportCmd{ID: 10100, Out: out}).Run(context.Background(), newTestClient(srv.URL))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("failed to read output: %v", err)
	}
	md := string(data)
	for _, want := range []string{
		"# Ticket #10100 — Printer not working",
		"Hello & hi",
		"## Conversations",
		"### 2100 (incoming, 2026-08-01T10:30:00Z)",
		"Please fix the printer",
		"### Nadia Rahman (outgoing, 2026-08-01T11:00:00Z)",
		"Will do",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("expected %q in markdown, got:\n%s", want, md)
		}
	}
	// HTML should be stripped.
	if strings.Contains(md, "<p>") {
		t.Errorf("expected HTML stripped from markdown, got:\n%s", md)
	}
}

func TestTicketsExportCmd_MarkdownAssets(t *testing.T) {
	srv := testMediaFixture(t)
	dir := t.TempDir()
	out := filepath.Join(dir, "ticket.md")

	err := (&TicketsExportCmd{ID: 10100, Out: out}).Run(context.Background(), newTestClient(srv.URL))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("failed to read output: %v", err)
	}
	md := string(data)
	if !strings.Contains(md, "![](assets/") {
		t.Errorf("expected image reference in markdown, got:\n%s", md)
	}

	assetDir := filepath.Join(dir, "assets")
	entries, err := os.ReadDir(assetDir)
	if err != nil {
		t.Fatalf("expected assets directory: %v", err)
	}
	if len(entries) != 3 {
		t.Errorf("expected 3 asset files, got %d", len(entries))
	}
	for _, e := range entries {
		b, err := os.ReadFile(filepath.Join(assetDir, e.Name()))
		if err != nil {
			t.Fatalf("read asset: %v", err)
		}
		if !strings.HasPrefix(string(b), "\x89PNG") {
			t.Errorf("asset %s: expected PNG bytes", e.Name())
		}
	}
}

func TestTicketsExportCmd_UnsupportedExt(t *testing.T) {
	srv := testExportFixture(t)
	out := filepath.Join(t.TempDir(), "ticket.txt")
	err := (&TicketsExportCmd{ID: 10100, Out: out}).Run(context.Background(), newTestClient(srv.URL))
	if err == nil || !strings.Contains(err.Error(), "unsupported output format") {
		t.Errorf("expected unsupported format error, got %v", err)
	}
}

func TestTicketsExportCmd_HTML(t *testing.T) {
	srv := testExportFixture(t)
	out := filepath.Join(t.TempDir(), "ticket.html")

	err := (&TicketsExportCmd{ID: 10100, Out: out}).Run(context.Background(), newTestClient(srv.URL))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("failed to read output: %v", err)
	}
	md := string(data)
	for _, want := range []string{"<h1>Ticket #10100 — Printer not working</h1>", "<h2>Conversations</h2>", "<h3>2100 (incoming, 2026-08-01T10:30:00Z)</h3>"} {
		if !strings.Contains(md, want) {
			t.Errorf("expected %q in html, got:\n%s", want, md)
		}
	}
}

func TestStripHTML(t *testing.T) {
	if got := stripHTML("<p>Hello &amp; hi</p>"); got != "Hello & hi" {
		t.Errorf("expected 'Hello & hi', got %q", got)
	}
}

func TestFetchExportDoc(t *testing.T) {
	srv := testExportFixture(t)
	doc, err := fetchExportDoc(context.Background(), newTestClient(srv.URL), 10100)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if doc.Ticket["subject"] != "Printer not working" {
		t.Errorf("expected subject, got %v", doc.Ticket["subject"])
	}
	if len(doc.Conversations) != 2 {
		t.Errorf("expected 2 conversations, got %d", len(doc.Conversations))
	}
}

func TestExportField_JSON(t *testing.T) {
	raw := `{"tags":["printer","hardware"],"active":true}`
	var m map[string]any
	_ = json.Unmarshal([]byte(raw), &m)
	if got := exportField(m, "tags"); got == "" {
		t.Error("expected tags rendered")
	}
}

func TestParseExportDoc(t *testing.T) {
	ticket := map[string]any{
		"id":              float64(10100),
		"subject":         "Printer not working",
		"description":     "<p>Broken &amp; bad</p>",
		"description_text": "Printer jammed",
	}
	convs := []map[string]any{
		{
			"id": float64(2), "user_id": float64(2100), "incoming": true,
			"created_at": "2026-08-01T10:30:00Z",
			"body":       `<p>See <img src="/a.png"></p>`, "body_text": "<p>See</p>",
			"attachments": []any{map[string]any{"id": "501"}},
		},
		{
			"id": float64(3), "user_id": float64(3100),
			"user":       map[string]any{"name": "Nadia Rahman"},
			"incoming":   false,
			"created_at": "2026-08-01T11:00:00Z",
		},
	}

	doc := parseExportDoc(ticket, convs)

	if doc.Subject != "Printer not working" {
		t.Errorf("subject: got %q", doc.Subject)
	}
	if doc.DisplayID != "10100" {
		t.Errorf("expected display fallback to id, got %q", doc.DisplayID)
	}
	if doc.DescHTML != "<p>Broken &amp; bad</p>" {
		t.Errorf("desc html: got %q", doc.DescHTML)
	}
	if doc.DescText != "Printer jammed" {
		t.Errorf("desc text: got %q", doc.DescText)
	}
	if len(doc.Conversations) != 2 {
		t.Fatalf("expected 2 conversations, got %d", len(doc.Conversations))
	}
	c0 := doc.Conversations[0]
	if c0.ID != "2" || c0.Author != "2100" || !c0.Incoming || c0.At != "2026-08-01T10:30:00Z" {
		t.Errorf("conv0 scalars wrong: %+v", c0)
	}
	if c0.BodyHTML != `<p>See <img src="/a.png"></p>` || c0.BodyText != "<p>See</p>" {
		t.Errorf("conv0 bodies wrong: %q %q", c0.BodyHTML, c0.BodyText)
	}
	if len(c0.Attachments) != 1 || c0.Attachments[0]["id"] != "501" {
		t.Errorf("conv0 attachments wrong: %+v", c0.Attachments)
	}
	c1 := doc.Conversations[1]
	if c1.Author != "Nadia Rahman" || c1.Incoming {
		t.Errorf("conv1 author/incoming wrong: %+v", c1)
	}
}

func TestParseExportDoc_DisplayIDPreference(t *testing.T) {
	ticket := map[string]any{"id": float64(1), "display_id": float64(10100)}
	doc := parseExportDoc(ticket, nil)
	if doc.DisplayID != "10100" {
		t.Errorf("expected display_id preferred over id, got %q", doc.DisplayID)
	}
}
