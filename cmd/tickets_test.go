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
	"testing"

	"fsvc/internal/fsapi"
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

func newTestClient(serverURL string) *fsapi.Client {
	return fsapi.New(fsapi.ClientConfig{BaseURL: serverURL, Cookie: "helpdesk_node_session=abc"})
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

func TestTicketsCategorizeCmd(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/_/tickets", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(loadFixture(t, "tickets.json"))
	})
	// Ticket 10100: assigned, latest conversation incoming=false, July 31 2026 (4 days ago)
	mux.HandleFunc("/api/_/tickets/10100/conversations", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"conversations":[{"id":1,"incoming":false,"created_at":"2026-07-31T00:00:00Z","user_id":1,"body_text":"."}],"meta":{"count":1}}`)
	})
	// Ticket 10101: assigned, latest conversation incoming=true, today
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
		err := (&TicketsCategorizeCmd{OlderThanDays: 1}).Run(context.Background(), newTestClient(srv.URL))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	if !strings.Contains(out, "# Unassigned (1)") {
		t.Errorf("expected 1 unassigned ticket:\n%s", out)
	}
	if !strings.Contains(out, "10103") {
		t.Errorf("expected unassigned ticket 10103:\n%s", out)
	}
	if !strings.Contains(out, "# Agent replied > 1.0 days, awaiting customer (2)") {
		t.Errorf("expected 2 stale-agent tickets:\n%s", out)
	}
	if !strings.Contains(out, "10104") {
		t.Errorf("expected no-conversation ticket 10104 in stale list using created date:\n%s", out)
	}
	if !strings.Contains(out, "# Customer replied, awaiting agent (1)") {
		t.Errorf("expected 1 customer-responded ticket:\n%s", out)
	}
}

func TestTicketsCategorizeCmd_QueryJSON(t *testing.T) {
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

	err := (&TicketsCategorizeCmd{QueryJSON: `{"filter":"123","query_hash":[{"condition":"status","operator":"is","value":0,"type":"default"}]}`, Page: 1, PerPage: 30}).Run(context.Background(), newTestClient(srv.URL))
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
	if gotQuery.Get("per_page") != "30" {
		t.Errorf("expected per_page=30, got %q", gotQuery.Get("per_page"))
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
		client.SetCSRF("tok123")
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
