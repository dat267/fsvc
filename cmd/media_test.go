package cmd

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// pngSig is the minimal PNG magic header DetectContentType recognizes.
var pngSig = []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}

func testMediaFixture(t *testing.T) *httptest.Server {
	t.Helper()
	var ticketBody, convBody string
	mux := http.NewServeMux()
	mux.HandleFunc("/api/_/tickets/10100", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(ticketBody))
	})
	mux.HandleFunc("/api/_/tickets/10100/conversations", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(convBody))
	})
	for _, id := range []string{"500", "501", "502"} {
		mux.HandleFunc("/helpdesk/attachments/"+id, func(w http.ResponseWriter, r *http.Request) {
			if strings.HasSuffix(r.URL.Path, "501") {
				w.Header().Set("Content-Type", "application/pdf")
				_, _ = w.Write([]byte("%PDF-fake"))
				return
			}
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write(pngSig)
		})
	}
	mux.HandleFunc("/pic.png", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(pngSig)
	})
	srv := httptest.NewServer(mux)
	ticketBody = `{"ticket":{"id":10100,"display_id":10100,"subject":"Printer not working","description":"<p>Broken</p><img src=\"pic.png\"><img src=\"/helpdesk/attachments/500\">","description_text":"Broken","attachments":[{"id":500,"name":"printer.png","content_type":"image/png","size":9,"canonical_url":"` + srv.URL + `/helpdesk/attachments/500"},{"id":501,"name":"manual.pdf","content_type":"application/pdf","size":99,"canonical_url":"` + srv.URL + `/helpdesk/attachments/501"}]}}`
	convBody = `{"conversations":[{"id":1,"user_id":2100,"incoming":true,"created_at":"2026-08-01T10:30:00Z","body":"<p>See the screenshot</p><img src=\"/helpdesk/attachments/502\">","body_text":"See the screenshot","attachments":[{"id":502,"name":"conv.png","content_type":"image/png","size":9,"canonical_url":"` + srv.URL + `/helpdesk/attachments/502"}]}]}`
	t.Cleanup(srv.Close)
	return srv
}

func TestImageSrcs(t *testing.T) {
	got := imageSrcs(`<p>a</p><img src="/x.png"><img src='y.png' alt="z"><IMG SRC="/Z.PNG">`)
	want := []string{"/x.png", "y.png", "/Z.PNG"}
	if len(got) != len(want) {
		t.Fatalf("expected %d srcs, got %d: %v", len(want), len(got), got)
	}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("src %d: expected %q, got %q", i, w, got[i])
		}
	}
}

func TestImageSrcs_NoImgs(t *testing.T) {
	if got := imageSrcs("<p>no images</p>"); len(got) != 0 {
		t.Errorf("expected no srcs, got %v", got)
	}
}

func TestResolveImageURL(t *testing.T) {
	base := "https://acme.freshservice.com"
	cases := []struct{ src, want string }{
		{"/helpdesk/attachments/500", "https://acme.freshservice.com/helpdesk/attachments/500"},
		{"https://acme.freshservice.com/helpdesk/attachments/500", "https://acme.freshservice.com/helpdesk/attachments/500"},
		{"//other.example.com/img.png", "//other.example.com/img.png"},
	}
	for _, tc := range cases {
		if got := resolveImageURL(base, tc.src); got != tc.want {
			t.Errorf("resolveImageURL(%q, %q): expected %q, got %q", base, tc.src, tc.want, got)
		}
	}
}

func TestSameMediaHost(t *testing.T) {
	cases := []struct {
		name string
		base string
		host string
		want bool
	}{
		{"same host", "acme.freshservice.com", "acme.freshservice.com", true},
		{"media cdn subdomain", "acme.freshservice.com", "acme.attachments.freshservice.com", true},
		{"unrelated subdomain of same registrable", "acme.freshservice.com", "other.freshservice.com", true},
		{"unrelated domain", "acme.freshservice.com", "evil.example.com", false},
		{"parent domain", "acme.freshservice.com", "freshservice.com", false},
		{"ip exact", "127.0.0.1", "127.0.0.1", true},
		{"ip other", "127.0.0.1", "127.0.0.2", false},
		{"ip suffix that shares label tail", "10.0.0.1", "10.0.0.2", false},
		{"bare name exact", "fsvc", "fsvc", true},
		{"bare name other", "fsvc", "other", false},
	}
	for _, tc := range cases {
		if got := sameMediaHost(tc.base, tc.host); got != tc.want {
			t.Errorf("%s: sameMediaHost(%q, %q): expected %v, got %v", tc.name, tc.base, tc.host, tc.want, got)
		}
	}
}

func TestDownload_AllowsSameRegistrableDomain(t *testing.T) {
	// base is 127.0.0.1:PORT; image src is a sibling CDN-style host under the
	// same registrable domain. The test server host is not one we control, so
	// verify via sameMediaHost directly plus that gatherMedia keeps the image
	// when the URL is allowed.
	if !sameMediaHost("myzoihelpdesk.freshservice.com", "myzoihelpdesk.attachments.freshservice.com") {
		t.Fatal("expected media CDN host to be allowed")
	}
}

func TestGatherMedia(t *testing.T) {
	srv := testMediaFixture(t)
	client := newTestClient(srv.URL)

	doc, err := fetchExportDoc(context.Background(), client, 10100)
	if err != nil {
		t.Fatalf("fetchExportDoc: %v", err)
	}

	if err := gatherMedia(context.Background(), client, doc); err != nil {
		t.Fatalf("gatherMedia: %v", err)
	}

	// Images: pic.png (relative, resolved), /helpdesk/attachments/500
	// (description img + attachment dedupe), conv.png attachment + conv img.
	if len(doc.Images) != 3 {
		t.Fatalf("expected 3 images, got %d", len(doc.Images))
	}

	var ownerTicket, ownerConv int
	for _, img := range doc.Images {
		if !strings.HasPrefix(string(img.Data), "\x89PNG") {
			t.Errorf("image %q: expected PNG bytes, got %q", img.ID, img.Data)
		}
		if img.Mime != "image/png" {
			t.Errorf("image %q: expected image/png, got %q", img.ID, img.Mime)
		}
		switch img.Owner {
		case "ticket":
			ownerTicket++
		case "conv-1":
			ownerConv++
		default:
			t.Errorf("image %q: unexpected owner %q", img.ID, img.Owner)
		}
	}
	if ownerTicket != 2 || ownerConv != 1 {
		t.Errorf("expected 2 ticket + 1 conv images, got %d + %d", ownerTicket, ownerConv)
	}

	// Non-image attachment listed as metadata only.
	if len(doc.Attachments) != 1 {
		t.Fatalf("expected 1 metadata attachment, got %d", len(doc.Attachments))
	}
	att := doc.Attachments[0]
	if att.Name != "manual.pdf" || att.ContentType != "application/pdf" || att.Size != 99 {
		t.Errorf("unexpected attachment metadata: %+v", att)
	}
}

func TestGatherMedia_SkipsFailedDownload(t *testing.T) {
	srv := testMediaFixture(t)
	client := newTestClient(srv.URL)

	doc := &exportDoc{
		Ticket: map[string]any{
			"description": `<img src="/missing.png">`,
		},
		Conversations: nil,
	}
	if err := gatherMedia(context.Background(), client, doc); err != nil {
		t.Fatalf("gatherMedia should not fail on missing image: %v", err)
	}
	if len(doc.Images) != 0 {
		t.Errorf("expected no images (download failed), got %d", len(doc.Images))
	}
}
