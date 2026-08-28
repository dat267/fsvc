package cmd

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestTicketsShowCmd(t *testing.T) {
	var ticketBody, convBody string
	mux := http.NewServeMux()
	mux.HandleFunc("/api/_/tickets/10100/conversations", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(convBody))
	})
	mux.HandleFunc("/api/_/tickets/10100", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(ticketBody))
	})
	// Also serve a second ticket for multi-ID testing.
	mux.HandleFunc("/api/_/tickets/10101/conversations", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(convBody))
	})
	mux.HandleFunc("/api/_/tickets/10101", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(ticketBody))
	})
	for _, id := range []string{"500", "501", "599"} {
		mux.HandleFunc("/helpdesk/attachments/"+id, func(w http.ResponseWriter, r *http.Request) {
			if strings.HasSuffix(r.URL.Path, "599") {
				w.Header().Set("Content-Type", "application/pdf")
				_, _ = w.Write([]byte("%PDF-fake"))
				return
			}
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write(pngSig)
		})
	}
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	ticketBody = `{"ticket":{"id":10100,"display_id":10100,"subject":"Printer not working","status":2,"status_name":"Open","priority":2,"priority_name":"Medium","urgency":1,"impact":1,"group_id":4001,"group_name":"Support","requester_id":5001,"requester_name":"Omar Saleh","responder_id":3100,"responder_name":"Nadia Rahman","department_id":5100,"department_name":"IT","created_at":"2026-08-01T10:00:00Z","updated_at":"2026-08-02T09:00:00Z","description":"<p>Printer jammed</p>","description_text":"Printer jammed","attachments":[{"id":500,"name":"printer.png","content_type":"image/png","size":9,"canonical_url":"` + srv.URL + `/helpdesk/attachments/500"},{"id":599,"name":"manual.pdf","content_type":"application/pdf","size":99,"canonical_url":"` + srv.URL + `/helpdesk/attachments/599"}]}}`
	convBody = `{"conversations":[{"id":2,"user_id":2100,"incoming":true,"created_at":"2026-08-01T10:30:00Z","body_text":"<p>Please fix the printer</p>","attachments":[{"id":501,"name":"photo.png","content_type":"image/png","size":9,"canonical_url":"` + srv.URL + `/helpdesk/attachments/501"}]},{"id":1,"user_id":3100,"user":{"name":"Nadia Rahman"},"incoming":true,"created_at":"2026-08-01T11:00:00Z","body_text":"<p>Will do</p>","attachments":[]}]}`

	// Image endpoints.
	http.HandleFunc("/helpdesk/attachments/500", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(pngSig)
	})
	http.HandleFunc("/helpdesk/attachments/501", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(pngSig)
	})

	out := captureStdout(t, func() {
		err := (&TicketsShowCmd{IDs: []int64{10100}}).Run(context.Background(), newTestClient(srv.URL))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	for _, want := range []string{
		"# Ticket #10100 — Printer not working",
		"Status     : Open", "Priority   : Medium", "Requester  : Omar Saleh",
		"Responder  : Nadia Rahman", "Urgency    : Low", "Impact     : Low",
		"Created    : 2026-08-01T10:00:00Z",
		"Printer jammed",
		"## Attachments", "photo.png", "printer.png",
		"## Conversations",
		"### 2100 (incoming, 2026-08-01T10:30:00Z)",
		"Please fix the printer",
		"### Nadia Rahman (incoming, 2026-08-01T11:00:00Z)",
		"Will do",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in show output, got:\n%s", want, out)
		}
	}

	// Oldest conversation must appear before the newer one in the trace.
	iOlder := strings.Index(out, "2026-08-01T10:30:00Z")
	iNewer := strings.Index(out, "2026-08-01T11:00:00Z")
	if iOlder > iNewer {
		t.Errorf("expected oldest conversation first, older index %d > newer index %d", iOlder, iNewer)
	}

	if strings.Contains(out, "<p>") {
		t.Errorf("expected HTML stripped, got:\n%s", out)
	}
}

func TestTicketsShowCmd_MultiID(t *testing.T) {
	convBody := `{"conversations":[],"meta":{"count":0}}`

	mux := http.NewServeMux()
	for _, id := range []string{"10100", "10101"} {
		id := id
		path := "/api/_/tickets/" + id
		mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			body := fmt.Sprintf(`{"ticket":{"id":%[1]s,"display_id":%[1]s,"subject":"Ticket %[1]s","status":2,"status_name":"Open","priority":2,"priority_name":"Medium","urgency":1,"impact":1,"group_id":4001,"group_name":"Support","requester_id":5001,"requester_name":"Omar Saleh","responder_id":3100,"responder_name":"Nadia Rahman","department_id":5100,"department_name":"IT","created_at":"2026-08-01T10:00:00Z","updated_at":"2026-08-02T09:00:00Z","description_text":"Body for %[1]s","attachments":[]}}`, id)
			_, _ = w.Write([]byte(body))
		})
		mux.HandleFunc(path+"/conversations", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(convBody))
		})
	}
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	out := captureStdout(t, func() {
		err := (&TicketsShowCmd{IDs: []int64{10100, 10101}}).Run(context.Background(), newTestClient(srv.URL))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	if !strings.Contains(out, "# Ticket #10100") {
		t.Errorf("expected first ticket, got:\n%s", out)
	}
	if !strings.Contains(out, "# Ticket #10101") {
		t.Errorf("expected second ticket, got:\n%s", out)
	}
	if !strings.Contains(out, "---") {
		t.Errorf("expected separator between tickets, got:\n%s", out)
	}
}