package cmd

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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

func TestTicketsRequestedCmd(t *testing.T) {
	srv := serveFixture(t, "requested_items.json", func(r *http.Request) {
		if r.URL.Path != "/api/_/tickets/10100/requested_items/7101" {
			t.Errorf("expected requested_items path, got %s", r.URL.Path)
		}
	})

	out := captureStdout(t, func() {
		err := (&TicketsRequestedCmd{ID: 10100, ItemID: 7101}).Run(context.Background(), newTestClient(srv.URL))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	if !strings.Contains(out, "This request is for other tickets") {
		t.Errorf("expected item.short_description in output:\n%s", out)
	}
}

func TestTicketsListCmd_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, `{"errors":["boom"]}`)
	}))
	defer srv.Close()

	err := (&TicketsListCmd{}).Run(context.Background(), newTestClient(srv.URL))
	if err == nil || !strings.Contains(err.Error(), "HTTP 500") {
		t.Errorf("expected HTTP 500 error, got %v", err)
	}
}
