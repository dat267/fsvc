package cmd

import (
	"bytes"
	"flag"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// updateGolden regenerates golden files. Deliberate output changes: run
//
//	go test ./cmd/ -run TestGolden -update-golden
//
// and review the diff before committing.
var updateGolden = flag.Bool("update-golden", false, "rewrite golden files")

// checkGolden compares got against testdata/golden/<name>, failing with a
// unified diff on mismatch.
func checkGolden(t *testing.T, name string, got []byte) {
	t.Helper()
	path := filepath.Join("..", "testdata", "golden", name)
	if *updateGolden {
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatalf("mkdir golden dir: %v", err)
		}
		if err := os.WriteFile(path, got, 0644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("golden file %s unreadable (run go test ./cmd/ -run TestGolden -update-golden): %v", path, err)
	}
	if bytes.Equal(want, got) {
		return
	}
	diff, _ := diffBytes(t, want, got)
	t.Errorf("golden mismatch in %s:\n%s", path, diff)
}

// diffBytes shells out to diff for a readable unified diff, falling back to a
// plain byte count when diff is unavailable.
func diffBytes(t *testing.T, want, got []byte) (string, error) {
	dir := t.TempDir()
	wantPath := filepath.Join(dir, "want")
	gotPath := filepath.Join(dir, "got")
	if err := os.WriteFile(wantPath, want, 0644); err != nil {
		return "", err
	}
	if err := os.WriteFile(gotPath, got, 0644); err != nil {
		return "", err
	}
	out, err := exec.Command("diff", "-u", wantPath, gotPath).CombinedOutput()
	if err != nil {
		if _, ok := err.(*exec.ExitError); !ok {
			return "", err
		}
	}
	return string(out), nil
}

// goldenDoc is the shared rich fixture: names, raw values, both body
// variants, image and non-image attachments, incoming and outgoing authors.
func goldenDoc() *exportDoc {
	return &exportDoc{
		Ticket: map[string]any{
			"id":              float64(10100),
			"display_id":      float64(10100),
			"subject":         "Printer not working",
			"status":          float64(2),
			"status_name":     "Open",
			"priority":        float64(2),
			"priority_name":   "Medium",
			"urgency":         float64(2),
			"impact":          float64(2),
			"group_id":        float64(4001),
			"group_name":      "Support",
			"requester_id":    float64(5001),
			"requester_name":  "Omar Saleh",
			"responder_id":    float64(3100),
			"responder_name":  "Nadia Rahman",
			"department_id":   float64(5100),
			"department_name": "IT",
			"created_at":      "2026-08-01T10:00:00Z",
			"updated_at":      "2026-08-02T09:00:00Z",
		},
		Subject:   "Printer not working",
		DisplayID: "10100",
		DescHTML:  `<p>Printer <b>jammed</b> &amp; smoking</p>`,
		DescText:  "Printer jammed & smoking",
		Conversations: []conversationDoc{
			{
				ID:       "1",
				Author:   "2100",
				Incoming: true,
				At:       "2026-08-01T10:30:00Z",
				BodyHTML: `<p>Please fix the printer <img src="/helpdesk/attachments/500"></p>`,
				BodyText: "<p>Please fix the printer</p>",
				Attachments: []map[string]any{
					{"id": "500", "name": "photo.png", "content_type": "image/png", "size": float64(9)},
				},
			},
			{
				ID:       "2",
				Author:   "Nadia Rahman",
				Incoming: false,
				At:       "2026-08-01T11:00:00Z",
				BodyHTML: "<p>Will do</p>",
				BodyText: "Will do",
			},
		},
		Images: []exportImage{
			{ID: "500", Data: pngSig, Mime: "image/png", Name: "photo.png", Owner: "conv-1"},
		},
		Attachments: []exportAttachment{
			{ID: "599", Name: "manual.pdf", ContentType: "application/pdf", Size: 99, URL: "https://acme.freshservice.com/helpdesk/attachments/599"},
		},
	}
}

func TestGoldenMarkdownExport(t *testing.T) {
	got, _, err := renderMarkdown(goldenDoc(), "ticket.md")
	if err != nil {
		t.Fatalf("renderMarkdown: %v", err)
	}
	checkGolden(t, "markdown.md", got)
}

func TestGoldenShowTicket(t *testing.T) {
	got, _, err := renderTicketMarkdown(goldenDoc(), 10100)
	if err != nil {
		t.Fatalf("renderTicketMarkdown: %v", err)
	}
	checkGolden(t, "show.md", got)
}

func TestGoldenHTML(t *testing.T) {
	got, err := renderHTML(goldenDoc())
	if err != nil {
		t.Fatalf("renderHTML: %v", err)
	}
	checkGolden(t, "export.html", got)
}

func TestGoldenDocx(t *testing.T) {
	got, err := renderDocx(goldenDoc())
	if err != nil {
		t.Fatalf("renderDocx: %v", err)
	}
	checkGolden(t, "export.docx", got)
}
