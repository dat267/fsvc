package cmd

import (
	"archive/zip"
	"bytes"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
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

// checkGoldenZip compares got against a golden zip by entry name and
// decompressed content. Zip bytes depend on the Go toolchain's compressor, so
// a raw byte comparison fails whenever CI and the developer build with
// different Go versions; comparing entries still catches real changes.
func checkGoldenZip(t *testing.T, name string, got []byte) {
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
	diff, err := compareZipEntries(want, got)
	if err != nil {
		t.Fatalf("compare %s: %v", path, err)
	}
	if diff != "" {
		t.Errorf("golden mismatch in %s:\n%s", path, diff)
	}
}

// compareZipEntries lists entry-level differences between two zips, or returns
// "" when every entry name and decompressed content matches.
func compareZipEntries(want, got []byte) (string, error) {
	wantEntries, err := readZipEntries(want)
	if err != nil {
		return "", fmt.Errorf("read golden zip: %w", err)
	}
	gotEntries, err := readZipEntries(got)
	if err != nil {
		return "", fmt.Errorf("read output zip: %w", err)
	}

	names := make([]string, 0, len(wantEntries)+len(gotEntries))
	for name := range wantEntries {
		names = append(names, name)
	}
	for name := range gotEntries {
		if _, ok := wantEntries[name]; !ok {
			names = append(names, name)
		}
	}
	sort.Strings(names)

	var b strings.Builder
	for _, name := range names {
		w, inWant := wantEntries[name]
		g, inGot := gotEntries[name]
		switch {
		case !inWant:
			fmt.Fprintf(&b, "unexpected entry in output: %s\n", name)
		case !inGot:
			fmt.Fprintf(&b, "missing entry in output: %s\n", name)
		case !bytes.Equal(w, g):
			fmt.Fprintf(&b, "entry %s differs (golden %d bytes, output %d bytes)\n", name, len(w), len(g))
		}
	}
	return b.String(), nil
}

// readZipEntries returns each entry's decompressed content by name.
func readZipEntries(data []byte) (map[string][]byte, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, err
	}
	entries := make(map[string][]byte, len(zr.File))
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			return nil, fmt.Errorf("open %s: %w", f.Name, err)
		}
		content, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", f.Name, err)
		}
		entries[f.Name] = content
	}
	return entries, nil
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
	got, _, err := renderMarkdown(goldenDoc())
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
	checkGoldenZip(t, "export.docx", got)
}

// rewriteZip repacks data with the given method, optionally mutating each
// entry's content first. It stands in for a different Go toolchain's packer.
func rewriteZip(t *testing.T, data []byte, method uint16, mutate func(name string, content []byte) []byte) []byte {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("read zip: %v", err)
	}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatalf("open %s: %v", f.Name, err)
		}
		content, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			t.Fatalf("read %s: %v", f.Name, err)
		}
		if mutate != nil {
			content = mutate(f.Name, content)
		}
		w, err := zw.CreateHeader(&zip.FileHeader{Name: f.Name, Method: method})
		if err != nil {
			t.Fatalf("create %s: %v", f.Name, err)
		}
		if _, err := w.Write(content); err != nil {
			t.Fatalf("write %s: %v", f.Name, err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}
	return buf.Bytes()
}

func TestGoldenZipComparisonToleratesDifferentCompression(t *testing.T) {
	golden, err := os.ReadFile(filepath.Join("..", "testdata", "golden", "export.docx"))
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	repacked := rewriteZip(t, golden, zip.Store, nil)
	if bytes.Equal(golden, repacked) {
		t.Fatal("test setup: expected repacking to change the bytes")
	}
	diff, err := compareZipEntries(golden, repacked)
	if err != nil {
		t.Fatalf("compareZipEntries: %v", err)
	}
	if diff != "" {
		t.Errorf("expected identical content to compare equal, got:\n%s", diff)
	}
}

func TestGoldenZipComparisonCatchesContentChanges(t *testing.T) {
	golden, err := os.ReadFile(filepath.Join("..", "testdata", "golden", "export.docx"))
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	modified := rewriteZip(t, golden, zip.Deflate, func(name string, content []byte) []byte {
		if name != "word/document.xml" {
			return content
		}
		return bytes.Replace(content, []byte("<w:t>"), []byte("<w:t>X"), 1)
	})
	diff, err := compareZipEntries(golden, modified)
	if err != nil {
		t.Fatalf("compareZipEntries: %v", err)
	}
	if diff == "" {
		t.Fatal("expected a changed entry to be reported")
	}
	if !strings.Contains(diff, "word/document.xml") {
		t.Errorf("expected the diff to name the changed entry, got %q", diff)
	}
}
