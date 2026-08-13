package cmd

import (
	"archive/zip"
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRenderDocx_Images(t *testing.T) {
	doc := &exportDoc{
		Ticket: map[string]any{
			"display_id": "10100",
			"subject":    "Printer not working",
		},
		Conversations: []map[string]any{
			{
				"id":        "1",
				"user_id":   "2100",
				"incoming":  true,
				"created_at": "2026-08-01T10:30:00Z",
			},
		},
		Images: []exportImage{
			{ID: "a.png", Data: pngSig, Mime: "image/png", Name: "a.png", Owner: "ticket"},
			{ID: "b.png", Data: pngSig, Mime: "image/png", Name: "b.png", Owner: "conv-1"},
		},
	}

	data, err := renderDocx(doc)
	if err != nil {
		t.Fatalf("renderDocx: %v", err)
	}

	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("output is not a valid zip: %v", err)
	}

	var docXML string
	var mediaCount int
	var rels string
	names := map[string]bool{}
	for _, f := range zr.File {
		names[f.Name] = true
		if strings.HasPrefix(f.Name, "word/media/") {
			mediaCount++
		}
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		buf := new(bytes.Buffer)
		_, _ = buf.ReadFrom(rc)
		_ = rc.Close()
		switch f.Name {
		case "word/document.xml":
			docXML = buf.String()
		case "word/_rels/document.xml.rels":
			rels = buf.String()
		}
	}

	if mediaCount != 2 {
		t.Errorf("expected 2 media parts, got %d (%v)", mediaCount, names)
	}
	for _, media := range []string{"word/media/a.png", "word/media/b.png"} {
		if !names[media] {
			t.Errorf("missing %s", media)
		}
	}
	if !strings.Contains(docXML, "<w:drawing>") {
		t.Errorf("expected <w:drawing> in document.xml, got:\n%s", docXML)
	}
	if !strings.Contains(rels, `Target="media/a.png"`) {
		t.Errorf("expected image relationships, got:\n%s", rels)
	}
}

func TestTicketsExportCmd_DocxWithImages(t *testing.T) {
	srv := testMediaFixture(t)
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
	found := false
	for _, f := range zr.File {
		if strings.HasPrefix(f.Name, "word/media/") {
			found = true
		}
	}
	if !found {
		t.Error("expected word/media/ entries in exported docx")
	}
}
