package cmd

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alecthomas/kong"
)

// setConcurrency pins the configured worker count for the duration of a test.
func setConcurrency(t *testing.T, n int) {
	t.Helper()
	old := concurrency
	concurrency = n
	t.Cleanup(func() { concurrency = old })
}

// inflightTracker records the highest number of requests being handled at the
// same time, plus the total handled. delay keeps each request open long enough
// for overlap to be observable.
type inflightTracker struct {
	delay time.Duration

	mu    sync.Mutex
	cur   int
	max   int
	total int
}

func (t *inflightTracker) handler(w http.ResponseWriter, r *http.Request) {
	t.mu.Lock()
	t.cur++
	t.total++
	if t.cur > t.max {
		t.max = t.cur
	}
	t.mu.Unlock()

	time.Sleep(t.delay)

	t.mu.Lock()
	t.cur--
	t.mu.Unlock()
}

func (t *inflightTracker) stats() (max, total int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.max, t.total
}

// ticketListBody is a ticket-list mock body with n self-assigned tickets.
func ticketListBody(n int) string {
	items := make([]string, n)
	for i := range items {
		items[i] = fmt.Sprintf(`{"id":%d,"subject":"T%d","priority":2,"status":2,"responder_id":99,"created_at":"2026-08-01T00:00:00Z"}`, 1000+i, i)
	}
	return `{"tickets":[` + strings.Join(items, ",") + `],"meta":{"has_next":false}}`
}

func TestClassifyConcurrencyBoundsInFlight(t *testing.T) {
	setNow(t, time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC))
	setConcurrency(t, 2)

	const tickets = 6
	track := &inflightTracker{delay: 25 * time.Millisecond}
	body := ticketListBody(tickets)

	mux := http.NewServeMux()
	mux.HandleFunc("/api/_/tickets", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, body)
	})
	mux.HandleFunc("/api/_/tickets/", func(w http.ResponseWriter, r *http.Request) {
		track.handler(w, r)
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"conversations":[],"meta":{"count":0}}`)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	captureStdout(t, func() {
		if err := (&TicketsOverviewCmd{OlderThanDays: 1, Page: 1, PerPage: 100}).Run(context.Background(), newTestClient(srv.URL)); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	max, total := track.stats()
	if total != tickets {
		t.Errorf("expected %d conversation fetches, got %d", tickets, total)
	}
	if max > 2 {
		t.Errorf("expected at most 2 in-flight requests, saw %d", max)
	}
	if max < 2 {
		t.Errorf("expected the conversation scan to run in parallel, max in-flight was %d", max)
	}
}

func TestClassifyConcurrencyOneRunsSequentially(t *testing.T) {
	setNow(t, time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC))
	setConcurrency(t, 1)

	const tickets = 4
	track := &inflightTracker{delay: 10 * time.Millisecond}
	body := ticketListBody(tickets)

	mux := http.NewServeMux()
	mux.HandleFunc("/api/_/tickets", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, body)
	})
	mux.HandleFunc("/api/_/tickets/", func(w http.ResponseWriter, r *http.Request) {
		track.handler(w, r)
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"conversations":[],"meta":{"count":0}}`)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	captureStdout(t, func() {
		if err := (&TicketsOverviewCmd{OlderThanDays: 1, Page: 1, PerPage: 100}).Run(context.Background(), newTestClient(srv.URL)); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	max, total := track.stats()
	if total != tickets {
		t.Errorf("expected %d conversation fetches, got %d", tickets, total)
	}
	if max != 1 {
		t.Errorf("expected strictly sequential fetches with --concurrency 1, saw %d in flight", max)
	}
}

func TestChangeSetApplyConcurrencyBoundsInFlight(t *testing.T) {
	setConcurrency(t, 3)

	const changes = 7
	track := &inflightTracker{delay: 15 * time.Millisecond}

	mux := http.NewServeMux()
	mux.HandleFunc("/api/_/tickets/", func(w http.ResponseWriter, r *http.Request) {
		track.handler(w, r)
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"ticket":{}}`)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	cs := make(ChangeSet, changes)
	for i := range cs {
		cs[i] = pendingChange{id: int64(1000 + i), field: "status", from: "2", to: "4", body: map[string]any{"status": 4}}
	}

	captureStdout(t, func() {
		if err := cs.Apply(context.Background(), newTestClient(srv.URL)); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	max, total := track.stats()
	if total != changes {
		t.Errorf("expected %d PUTs, got %d", changes, total)
	}
	if max > 3 {
		t.Errorf("expected at most 3 in-flight PUTs, saw %d", max)
	}
	if max < 3 {
		t.Errorf("expected PUTs to run in parallel, max in-flight was %d", max)
	}
}

func TestConcurrencyDefaultsToEight(t *testing.T) {
	if concurrency != 8 {
		t.Errorf("expected a default worker count of 8, got %d", concurrency)
	}
}

func TestConcurrencyFlagBindsAndDefaults(t *testing.T) {
	var cli CLI
	parser, err := kong.New(&cli)
	if err != nil {
		t.Fatalf("kong.New: %v", err)
	}
	if _, err := parser.Parse([]string{"session"}); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if cli.Concurrency != 8 {
		t.Errorf("expected a default of 8, got %d", cli.Concurrency)
	}

	var custom CLI
	customParser, err := kong.New(&custom)
	if err != nil {
		t.Fatalf("kong.New: %v", err)
	}
	if _, err := customParser.Parse([]string{"session", "--concurrency", "3"}); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if custom.Concurrency != 3 {
		t.Errorf("expected --concurrency 3 to bind, got %d", custom.Concurrency)
	}
}
