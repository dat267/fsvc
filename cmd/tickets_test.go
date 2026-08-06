package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

func loadFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "testdata", name))
	if err != nil {
		t.Fatalf("failed to read fixture %s: %v", name, err)
	}
	return data
}

func serveFixture(t *testing.T, name string, check func(*http.Request)) *httptest.Server {
	t.Helper()
	body := loadFixture(t, name)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if check != nil {
			check(r)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func newTestClient(serverURL string) *Client {
	return New(ClientConfig{BaseURL: serverURL, ItildeskSession: "abc"})
}

func TestTicketsListCmd_Table(t *testing.T) {
	srv := serveFixture(t, "tickets.json", func(r *http.Request) {
		if r.URL.Path != "/api/_/tickets" {
			t.Errorf("expected path /api/_/tickets, got %s", r.URL.Path)
		}
		if r.URL.Query().Get("per_page") != "30" {
			t.Errorf("expected default per_page=30, got %v", r.URL.Query())
		}
		if r.URL.Query().Get("order_type") != "desc" {
			t.Errorf("expected default order_type=desc, got %v", r.URL.Query())
		}
	})

	out := captureStdout(t, func() {
		err := (&TicketsListCmd{OrderBy: "created_at", OrderType: "desc", Page: 1, PerPage: 30}).Run(context.Background(), newTestClient(srv.URL))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	if !strings.Contains(out, "| ID    | Subject") {
		t.Errorf("expected table header, got:\n%s", out)
	}
	if !strings.Contains(out, "Omar Saleh") {
		t.Errorf("expected nested requester name in output:\n%s", out)
	}
	if !strings.Contains(out, "10100") {
		t.Errorf("expected ticket id in output:\n%s", out)
	}
}

func TestTicketsListCmd_JSONFormat(t *testing.T) {
	srv := serveFixture(t, "tickets.json", nil)
	out := captureStdout(t, func() {
		err := (&TicketsListCmd{Format: "json"}).Run(context.Background(), newTestClient(srv.URL))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
	if !strings.Contains(out, `"subject"`) {
		t.Errorf("expected raw JSON output:\n%s", out)
	}
}

func TestTicketsListCmd_CSVFormat(t *testing.T) {
	srv := serveFixture(t, "tickets.json", nil)
	out := captureStdout(t, func() {
		err := (&TicketsListCmd{Format: "csv"}).Run(context.Background(), newTestClient(srv.URL))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
	if !strings.HasPrefix(out, "ID,Subject") {
		t.Errorf("expected CSV header, got:\n%s", out)
	}
}

func TestTicketsListCmd_QueryParams(t *testing.T) {
	srv := serveFixture(t, "tickets.json", func(r *http.Request) {
		q := r.URL.Query()
		if q.Get("filter") != "1100" {
			t.Errorf("expected filter=1100, got %v", q)
		}
		if q.Get("include") != "stats,responder" {
			t.Errorf("expected include=stats,responder, got %v", q)
		}
		if q.Get("order_by") != "updated_at" {
			t.Errorf("expected order_by=updated_at, got %v", q)
		}
		if q.Get("order_type") != "asc" {
			t.Errorf("expected order_type=asc, got %v", q)
		}
		if q.Get("page") != "2" {
			t.Errorf("expected page=2, got %v", q)
		}
		if q.Get("per_page") != "50" {
			t.Errorf("expected per_page=50, got %v", q)
		}
	})

	cmd := &TicketsListCmd{
		Filter:    1100,
		Include:   "stats,responder",
		OrderBy:   "updated_at",
		OrderType: "asc",
		Page:      2,
		PerPage:   50,
	}
	if err := cmd.Run(context.Background(), newTestClient(srv.URL)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestTicketsConvCmd(t *testing.T) {
	srv := serveFixture(t, "conversations.json", func(r *http.Request) {
		if r.URL.Path != "/api/_/tickets/10100/conversations" {
			t.Errorf("expected conversations path, got %s", r.URL.Path)
		}
		if r.URL.Query().Get("per_page") != "3" {
			t.Errorf("expected per_page=3, got %v", r.URL.Query())
		}
	})

	out := captureStdout(t, func() {
		err := (&TicketsConvCmd{ID: 10100, PerPage: 3}).Run(context.Background(), newTestClient(srv.URL))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	if !strings.Contains(out, "We called the customer") {
		t.Errorf("expected conversation body_text in output:\n%s", out)
	}
}

func TestTicketsClassifyCmd(t *testing.T) {
	setNow(t, time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC))
	mux := http.NewServeMux()
	mux.HandleFunc("/api/_/tickets", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.Contains(r.URL.Query().Get("query_hash"), `"value":["-1"]`):
			// unassigned query: tickets with responder_id -1 (10103)
			_, _ = fmt.Fprint(w, `{"tickets":[{"id":10103,"subject":"Unassigned printer ticket","priority":1,"status":2,"responder_id":-1,"created_at":"2026-08-01T00:00:00+04:00"}],"meta":{"has_next":false}}`)
		case strings.Contains(r.URL.Query().Get("query_hash"), "responder_id"):
			// self-assigned query: only assigned tickets
			_, _ = fmt.Fprint(w, `{"tickets":[{"id":10100,"subject":"Request for Omar Saleh : Customer Support Ticket","priority":2,"status":2,"responder_id":3100,"created_at":"2026-07-29T16:42:48+04:00"},{"id":10101,"subject":"Printer not working","priority":1,"status":4,"responder_id":3101,"created_at":"2026-07-28T10:00:00+04:00"},{"id":10104,"subject":"Old ticket with no messages","priority":2,"status":2,"responder_id":3102,"created_at":"2026-07-25T00:00:00+04:00"}],"meta":{"has_next":false}}`)
		default:
			_, _ = w.Write(loadFixture(t, "tickets.json"))
		}
	})
	// Ticket 10100: assigned, latest conversation from the responder (3100),
	// July 31 2026 (4 days ago) -> stale, awaiting customer
	mux.HandleFunc("/api/_/tickets/10100/conversations", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"conversations":[{"id":1,"incoming":false,"created_at":"2026-07-31T00:00:00Z","user_id":3100,"body_text":"."}],"meta":{"count":1}}`)
	})
	// Ticket 10101: assigned, latest conversation from someone else (2), today
	mux.HandleFunc("/api/_/tickets/10101/conversations", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"conversations":[{"id":2,"incoming":true,"created_at":"2026-08-04T00:00:00Z","user_id":2,"body_text":"."}],"meta":{"count":1}}`)
	})
	// Any other ticket (e.g. 10104): no conversations
	mux.HandleFunc("/api/_/tickets/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"conversations":[],"meta":{"count":0}}`)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	out := captureStdout(t, func() {
		err := (&TicketsClassifyCmd{OlderThanDays: 1, PerPage: 100}).Run(context.Background(), newTestClient(srv.URL))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	if !strings.Contains(out, "Scanned 4 unresolved tickets (3 self-assigned, 1 unassigned)") {
		t.Errorf("expected scan summary:\n%s", out)
	}
	if !strings.Contains(out, "## Unassigned (1)") {
		t.Errorf("expected 1 unassigned ticket:\n%s", out)
	}
	if !strings.Contains(out, "10103") {
		t.Errorf("expected unassigned ticket 10103:\n%s", out)
	}
	if !strings.Contains(out, "## Agent replied > 1 business days, awaiting customer (2)") {
		t.Errorf("expected 2 stale-agent tickets:\n%s", out)
	}
	if !strings.Contains(out, "10104") {
		t.Errorf("expected no-conversation ticket 10104 in stale list using created date:\n%s", out)
	}
	if !strings.Contains(out, "## Last reply from someone else, awaiting agent (1)") {
		t.Errorf("expected 1 someone-else-replied ticket:\n%s", out)
	}
}

func TestTicketsClassifyCmd_QueryJSON(t *testing.T) {
	var gotQuery url.Values
	mux := http.NewServeMux()
	mux.HandleFunc("/api/_/tickets", func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(loadFixture(t, "tickets.json"))
	})
	mux.HandleFunc("/api/_/tickets/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"conversations":[],"meta":{"count":0}}`)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	err := (&TicketsClassifyCmd{QueryJSON: `{"filter":"123","query_hash":[{"condition":"status","operator":"is","value":0,"type":"default"}]}`, Page: 1, PerPage: 100}).Run(context.Background(), newTestClient(srv.URL))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if gotQuery.Get("filter") != "123" {
		t.Errorf("expected filter=123 (unquoted), got %q", gotQuery.Get("filter"))
	}
	var gotHash, wantHash any
	if err := json.Unmarshal([]byte(gotQuery.Get("query_hash")), &gotHash); err != nil {
		t.Fatalf("query_hash is not JSON: %q", gotQuery.Get("query_hash"))
	}
	_ = json.Unmarshal([]byte(`[{"condition":"status","operator":"is","value":0,"type":"default"}]`), &wantHash)
	if !reflect.DeepEqual(gotHash, wantHash) {
		t.Errorf("expected query_hash %v, got %v", wantHash, gotHash)
	}
	if gotQuery.Get("per_page") != "100" {
		t.Errorf("expected per_page=100, got %q", gotQuery.Get("per_page"))
	}
}

func TestTicketsClassifyCmd_CustomFilterSingleFetch(t *testing.T) {
	fetchCount := 0
	mux := http.NewServeMux()
	mux.HandleFunc("/api/_/tickets", func(w http.ResponseWriter, r *http.Request) {
		fetchCount++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(loadFixture(t, "tickets.json"))
	})
	mux.HandleFunc("/api/_/tickets/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"conversations":[],"meta":{"count":0}}`)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	cmd := &TicketsClassifyCmd{Filter: 123, Page: 1, PerPage: 100}
	err := cmd.Run(context.Background(), newTestClient(srv.URL))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if fetchCount != 1 {
		t.Errorf("expected custom filter to execute exactly 1 list fetch, got %d", fetchCount)
	}
}

func TestTicketsClassifyCmd_Pagination(t *testing.T) {
	setNow(t, time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC))
	var mu sync.Mutex
	calls := 0
	var pages []string

	mux := http.NewServeMux()
	mux.HandleFunc("/api/_/tickets", func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Query().Get("page")
		mu.Lock()
		calls++
		pages = append(pages, p)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if p == "1" {
			_, _ = fmt.Fprint(w, `{"tickets":[{"id":99991,"subject":"Page 1 ticket","group_id":1,"responder_id":99,"priority":1,"status":2,"created_at":"2026-07-01T00:00:00Z"}],"meta":{"has_next":true}}`)
		} else {
			_, _ = fmt.Fprint(w, `{"tickets":[{"id":99992,"subject":"Page 2 ticket","group_id":1,"responder_id":99,"priority":2,"status":2,"created_at":"2026-07-02T00:00:00Z"}],"meta":{"has_next":false}}`)
		}
	})
	mux.HandleFunc("/api/_/tickets/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"conversations":[],"meta":{"count":0}}`)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	out := captureStdout(t, func() {
		err := (&TicketsClassifyCmd{OlderThanDays: 1, Page: 1, PerPage: 1}).Run(context.Background(), newTestClient(srv.URL))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	if calls != 4 {
		t.Errorf("expected 4 page fetches (2 pages x 2 queries), got %d", calls)
	}
	if !strings.Contains(out, "99991") || !strings.Contains(out, "99992") {
		t.Errorf("expected tickets from both pages:\n%s", out)
	}
}

func TestTicketsFillStartDatesCmd(t *testing.T) {
	var putCalls []struct {
		Path string
		Body []byte
	}
	var putMu sync.Mutex

	mux := http.NewServeMux()
	mux.HandleFunc("/api/_/tickets", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"tickets":[{"id":10,"planned_start_date":null,"created_at":"2026-08-01T12:00:00Z"},{"id":20,"planned_start_date":"2025-01-01T00:00:00Z","created_at":"2026-08-01T12:00:00Z"}],"meta":{"has_next":false}}`)
	})
	// Ticket 10: planned_start_date=null, created_at populated → fillable (PUT)
	mux.HandleFunc("/api/_/tickets/10", func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		putMu.Lock()
		putCalls = append(putCalls, struct {
			Path string
			Body []byte
		}{Path: r.URL.Path, Body: b})
		putMu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"ticket":{"id":10}}`)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	out := captureStdout(t, func() {
		err := (&TicketsFillStartDatesCmd{Yes: true, PerPage: 100}).Run(context.Background(), newTestClient(srv.URL))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	if !strings.Contains(out, "[planned_start_date] ticket 10: nil -> 2026-08-01T12:00:00Z") {
		t.Errorf("expected preview line, got %q", out)
	}
	if len(putCalls) != 1 {
		t.Fatalf("expected 1 PUT call, got %d", len(putCalls))
	}
	if string(putCalls[0].Body) != `{"planned_start_date":"2026-08-01T12:00:00Z"}` {
		t.Errorf("unexpected PUT body: %q", putCalls[0].Body)
	}
	if !strings.Contains(out, "Done: 1 applied") {
		t.Errorf("expected summary, got %q", out)
	}
}

func TestTicketsFillEndDatesCmd(t *testing.T) {
	setNow(t, time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC))
	var putCalls []struct {
		Path string
		Body []byte
	}
	var putMu sync.Mutex

	mux := http.NewServeMux()
	mux.HandleFunc("/api/_/tickets", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"tickets":[{"id":10,"planned_end_date":null},{"id":20,"planned_end_date":"2099-01-01T00:00:00Z"},{"id":30,"planned_end_date":"2020-01-01T00:00:00Z"}],"meta":{"has_next":false}}`)
	})
	mux.HandleFunc("/api/_/tickets/10", func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		putMu.Lock()
		putCalls = append(putCalls, struct {
			Path string
			Body []byte
		}{Path: r.URL.Path, Body: b})
		putMu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"ticket":{"id":10}}`)
	})
	mux.HandleFunc("/api/_/tickets/30", func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		putMu.Lock()
		putCalls = append(putCalls, struct {
			Path string
			Body []byte
		}{Path: r.URL.Path, Body: b})
		putMu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"ticket":{"id":30}}`)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	out := captureStdout(t, func() {
		err := (&TicketsFillEndDatesCmd{Yes: true, PerPage: 100}).Run(context.Background(), newTestClient(srv.URL))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	if !strings.Contains(out, "[planned_end_date] ticket 10:  -> ") {
		t.Errorf("expected null→filled preview, got %q", out)
	}
	if !strings.Contains(out, "[planned_end_date] ticket 30: 2020-01-01T00:00:00Z -> ") {
		t.Errorf("expected past→bump preview, got %q", out)
	}
	if strings.Contains(out, "ticket 20") {
		t.Errorf("future-date ticket 20 should not appear, got %q", out)
	}
	if len(putCalls) != 2 {
		t.Fatalf("expected 2 PUT calls, got %d", len(putCalls))
	}
	if !strings.Contains(out, "Done: 2 applied") {
		t.Errorf("expected summary, got %q", out)
	}
}

func TestTicketsListCmd_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = fmt.Fprint(w, `{"errors":["boom"]}`)
	}))
	defer srv.Close()

	err := (&TicketsListCmd{}).Run(context.Background(), newTestClient(srv.URL))
	if err == nil || !strings.Contains(err.Error(), "HTTP 500") {
		t.Errorf("expected HTTP 500 error, got %v", err)
	}
}

func TestTicketsUpdateCmd_Pairs(t *testing.T) {
	var gotBody []byte
	var gotCSRF string

	srv := serveFixture(t, "put_ticket.json", func(r *http.Request) {
		if r.Method != http.MethodPut {
			t.Errorf("expected PUT, got %s", r.Method)
		}
		if r.URL.Path != "/api/_/tickets/10100" {
			t.Errorf("expected path /api/_/tickets/10100, got %s", r.URL.Path)
		}
		if ct := r.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
			t.Errorf("expected application/json content type, got %q", ct)
		}
		gotCSRF = r.Header.Get("X-CSRF-Token")
		var err error
		gotBody, err = io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("failed to read request body: %v", err)
		}
	})

	out := captureStdout(t, func() {
		cmd := &TicketsUpdateCmd{
			ID:    10100,
			Pairs: []string{"priority=1", "group_id=4001", "custom_fields.type_of_ticket_received=Duplicate"},
		}
		client := newTestClient(srv.URL)
		client.csrf = "tok123"
		if err := cmd.Run(context.Background(), client); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	if gotCSRF != "tok123" {
		t.Errorf("expected X-CSRF-Token tok123, got %q", gotCSRF)
	}
	wantBody := `{"custom_fields":{"type_of_ticket_received":"Duplicate"},"group_id":4001,"priority":1}`
	if string(gotBody) != wantBody {
		t.Errorf("expected body %s, got %s", wantBody, gotBody)
	}
	if !strings.Contains(out, "10100") {
		t.Errorf("expected updated ticket id in output:\n%s", out)
	}
}

func TestTicketsUpdateCmd_RawBody(t *testing.T) {
	var gotBody []byte

	srv := serveFixture(t, "put_ticket.json", func(r *http.Request) {
		var err error
		gotBody, err = io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("failed to read request body: %v", err)
		}
	})

	raw := `{"priority":1,"br_validation_excludes":"eyJhbGciOiJIUzI1NiJ9.eyJvcHRpb25hbCI6W119"}`
	err := (&TicketsUpdateCmd{
		ID:    10100,
		Body:  raw,
		Pairs: []string{"priority=9"},
	}).Run(context.Background(), newTestClient(srv.URL))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(gotBody) != raw {
		t.Errorf("expected verbatim body %s, got %s", raw, gotBody)
	}
}

func TestTicketsUpdateCmd_NothingToUpdate(t *testing.T) {
	srv := serveFixture(t, "put_ticket.json", nil)
	err := (&TicketsUpdateCmd{ID: 10100}).Run(context.Background(), newTestClient(srv.URL))
	if err == nil || !strings.Contains(err.Error(), "nothing to update") {
		t.Errorf("expected 'nothing to update' error, got %v", err)
	}
}

func TestTicketsUpdateCmd_InvalidBody(t *testing.T) {
	srv := serveFixture(t, "put_ticket.json", nil)
	err := (&TicketsUpdateCmd{ID: 10100, Body: "not json"}).Run(context.Background(), newTestClient(srv.URL))
	if err == nil || !strings.Contains(err.Error(), "invalid JSON") {
		t.Errorf("expected invalid JSON error, got %v", err)
	}
}

func TestTicketsUpdateCmd_InvalidPair(t *testing.T) {
	srv := serveFixture(t, "put_ticket.json", nil)
	err := (&TicketsUpdateCmd{ID: 10100, Pairs: []string{"noequals"}}).Run(context.Background(), newTestClient(srv.URL))
	if err == nil {
		t.Error("expected error for invalid pair")
	}
}

func TestTicketsSyncUrgencyImpactCmd(t *testing.T) {
	var putCalls []struct {
		Path string
		Body []byte
	}
	var putMu sync.Mutex

	mux := http.NewServeMux()
	mux.HandleFunc("/api/_/tickets", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"tickets":[{"id":10,"priority":2,"urgency":2,"impact":2},{"id":20,"priority":3,"urgency":1,"impact":1},{"id":30,"priority":4,"urgency":3,"impact":3}],"meta":{"has_next":false}}`)
	})
	mux.HandleFunc("/api/_/tickets/10", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			_, _ = fmt.Fprint(w, `{"ticket":{"id":10,"priority":2,"urgency":2,"impact":2}}`)
		} else {
			b, _ := io.ReadAll(r.Body)
			putMu.Lock()
			putCalls = append(putCalls, struct {
				Path string
				Body []byte
			}{Path: r.URL.Path, Body: b})
			putMu.Unlock()
			_, _ = fmt.Fprint(w, `{"ticket":{"id":10}}`)
		}
	})
	mux.HandleFunc("/api/_/tickets/20", func(w http.ResponseWriter, r *http.Request) {
		// priority 3, urgency=1 impact=1 -> target (3,2)
		if r.Method == http.MethodGet {
			_, _ = fmt.Fprint(w, `{"ticket":{"id":20,"priority":3,"urgency":1,"impact":1}}`)
		} else {
			b, _ := io.ReadAll(r.Body)
			putMu.Lock()
			putCalls = append(putCalls, struct {
				Path string
				Body []byte
			}{Path: r.URL.Path, Body: b})
			putMu.Unlock()
			_, _ = fmt.Fprint(w, `{"ticket":{"id":20}}`)
		}
	})
	mux.HandleFunc("/api/_/tickets/30", func(w http.ResponseWriter, r *http.Request) {
		// priority 4, urgency=3 impact=3 -> already correct, skip
		_, _ = fmt.Fprint(w, `{"ticket":{"id":30,"priority":4,"urgency":3,"impact":3}}`)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	out := captureStdout(t, func() {
		err := (&TicketsSyncUrgencyImpactCmd{Yes: true, PerPage: 100}).Run(context.Background(), newTestClient(srv.URL))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	if !strings.Contains(out, "[priority=2] ticket 10: urgency=2 impact=2 -> urgency=3 impact=1") {
		t.Errorf("expected ticket 10 preview, got %q", out)
	}
	if !strings.Contains(out, "[priority=3] ticket 20: urgency=1 impact=1 -> urgency=3 impact=2") {
		t.Errorf("expected ticket 20 preview, got %q", out)
	}
	if strings.Contains(out, "ticket 30") {
		t.Errorf("ticket 30 should not appear, got %q", out)
	}
	if len(putCalls) != 2 {
		t.Fatalf("expected 2 PUT calls, got %d", len(putCalls))
	}
	if !strings.Contains(out, "Done: 2 applied") {
		t.Errorf("expected summary, got %q", out)
	}
}

func TestTicketsSyncPriorityCmd(t *testing.T) {
	var putCalls []struct {
		Path string
		Body []byte
	}
	var putMu sync.Mutex

	mux := http.NewServeMux()
	mux.HandleFunc("/api/_/tickets", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"tickets":[{"id":10,"priority":2,"urgency":3,"impact":3},{"id":20,"priority":2,"urgency":2,"impact":2},{"id":30,"priority":3,"urgency":3,"impact":1}],"meta":{"has_next":false}}`)
	})
	mux.HandleFunc("/api/_/tickets/10", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			_, _ = fmt.Fprint(w, `{"ticket":{"id":10,"priority":2,"urgency":3,"impact":3}}`)
		} else {
			b, _ := io.ReadAll(r.Body)
			putMu.Lock()
			putCalls = append(putCalls, struct {
				Path string
				Body []byte
			}{Path: r.URL.Path, Body: b})
			putMu.Unlock()
			_, _ = fmt.Fprint(w, `{"ticket":{"id":10}}`)
		}
	})
	mux.HandleFunc("/api/_/tickets/20", func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, `{"ticket":{"id":20,"priority":2,"urgency":2,"impact":2}}`)
	})
	mux.HandleFunc("/api/_/tickets/30", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			_, _ = fmt.Fprint(w, `{"ticket":{"id":30,"priority":3,"urgency":3,"impact":1}}`)
		} else {
			b, _ := io.ReadAll(r.Body)
			putMu.Lock()
			putCalls = append(putCalls, struct {
				Path string
				Body []byte
			}{Path: r.URL.Path, Body: b})
			putMu.Unlock()
			_, _ = fmt.Fprint(w, `{"ticket":{"id":30}}`)
		}
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	out := captureStdout(t, func() {
		err := (&TicketsSyncPriorityCmd{Yes: true, PerPage: 100}).Run(context.Background(), newTestClient(srv.URL))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	if !strings.Contains(out, "[urgency=3 impact=3] ticket 10: priority=2 -> priority=4") {
		t.Errorf("expected ticket 10 preview, got %q", out)
	}
	if !strings.Contains(out, "[urgency=3 impact=1] ticket 30: priority=3 -> priority=2") {
		t.Errorf("expected ticket 30 preview, got %q", out)
	}
	if strings.Contains(out, "ticket 20") {
		t.Errorf("ticket 20 should not appear, got %q", out)
	}
	if len(putCalls) != 2 {
		t.Fatalf("expected 2 PUT calls, got %d", len(putCalls))
	}
	if !strings.Contains(out, "Done: 2 applied") {
		t.Errorf("expected summary, got %q", out)
	}
}
