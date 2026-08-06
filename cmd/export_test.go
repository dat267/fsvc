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

func TestTicketsExportCmd_PDF(t *testing.T) {
	srv := testExportFixture(t)
	out := filepath.Join(t.TempDir(), "ticket.pdf")

	err := (&TicketsExportCmd{ID: 10100, Out: out}).Run(context.Background(), newTestClient(srv.URL))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("failed to read output: %v", err)
	}
	if len(data) < 100 || !strings.HasPrefix(string(data[:5]), "%PDF") {
		t.Errorf("expected PDF header, got %q", data[:min(20, len(data))])
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
