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
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alecthomas/kong"
)

func loadFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "testdata", name))
	if err != nil {
		t.Fatalf("failed to read fixture %s: %v", name, err)
	}
	return data
}

func newTestClient(serverURL string) *Client {
	return New(ClientConfig{BaseURL: serverURL, ItildeskSession: "abc"})
}

func TestTicketsOverviewCmd(t *testing.T) {
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
		err := (&TicketsOverviewCmd{OlderThanDays: 1, PerPage: 100, IncludeUnassigned: true}).Run(context.Background(), newTestClient(srv.URL))
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
	if !strings.Contains(out, "## Waiting on customer > 1 business days — follow up or resolve (2)") {
		t.Errorf("expected 2 stale-agent tickets:\n%s", out)
	}
	if !strings.Contains(out, "10104") {
		t.Errorf("expected no-conversation ticket 10104 in stale list using created date:\n%s", out)
	}
	if !strings.Contains(out, "## Last reply from someone else, awaiting agent (1)") {
		t.Errorf("expected 1 someone-else-replied ticket:\n%s", out)
	}
}

func TestTicketsOverviewCmd_QueryJSON(t *testing.T) {
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

	err := (&TicketsOverviewCmd{QueryJSON: `{"filter":"123","query_hash":[{"condition":"status","operator":"is","value":0,"type":"default"}]}`, Page: 1, PerPage: 100}).Run(context.Background(), newTestClient(srv.URL))
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

func TestTicketsOverviewCmd_CustomFilterSingleFetch(t *testing.T) {
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

	cmd := &TicketsOverviewCmd{Filter: 123, Page: 1, PerPage: 100}
	err := cmd.Run(context.Background(), newTestClient(srv.URL))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if fetchCount != 1 {
		t.Errorf("expected custom filter to execute exactly 1 list fetch, got %d", fetchCount)
	}
}

func TestTicketsOverviewCmd_Pagination(t *testing.T) {
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
		err := (&TicketsOverviewCmd{OlderThanDays: 1, Page: 1, PerPage: 1, IncludeUnassigned: true}).Run(context.Background(), newTestClient(srv.URL))
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
		_, _ = fmt.Fprint(w, `{"tickets":[{"id":10,"planned_start_date":null,"created_at":"2026-08-01T12:07:30Z"},{"id":20,"planned_start_date":"2025-01-01T00:00:00Z","created_at":"2026-08-01T12:00:00Z"}],"meta":{"has_next":false}}`)
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

	if !strings.Contains(out, "[planned_start_date] ticket 10: nil -> 2026-08-01T12:15:00Z") {
		t.Errorf("expected preview line, got %q", out)
	}
	if len(putCalls) != 1 {
		t.Fatalf("expected 1 PUT call, got %d", len(putCalls))
	}
	if string(putCalls[0].Body) != `{"planned_start_date":"2026-08-01T12:15:00Z"}` {
		t.Errorf("unexpected PUT body: %q", putCalls[0].Body)
	}
	if !strings.Contains(out, "Done: 1 applied") {
		t.Errorf("expected summary, got %q", out)
	}
}

// pushEndFixture serves the given ticket list and records every PUT body.
// lastMsg maps a ticket id to its latest conversation time; "" means the ticket
// has no conversations at all.
func pushEndFixture(t *testing.T, ticketsJSON string, lastMsg map[int64]string) (*httptest.Server, func() []putRecord) {
	t.Helper()
	var (
		mu   sync.Mutex
		puts []putRecord
	)

	mux := http.NewServeMux()
	mux.HandleFunc("/api/_/tickets", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, ticketsJSON)
	})
	mux.HandleFunc("/api/_/tickets/", func(w http.ResponseWriter, r *http.Request) {
		rest := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/_/tickets/"), "/")
		if strings.HasSuffix(rest, "/conversations") {
			parts := strings.Split(rest, "/")
			id, _ := strconv.ParseInt(parts[0], 10, 64)
			w.Header().Set("Content-Type", "application/json")
			if at := lastMsg[id]; at != "" {
				_, _ = fmt.Fprintf(w, `{"conversations":[{"id":1,"user_id":2,"incoming":true,"created_at":%q}],"meta":{"count":1}}`, at)
			} else {
				_, _ = fmt.Fprint(w, `{"conversations":[],"meta":{"count":0}}`)
			}
			return
		}
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		puts = append(puts, putRecord{Path: r.URL.Path, Body: string(b)})
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"ticket":{}}`)
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, func() []putRecord {
		mu.Lock()
		defer mu.Unlock()
		return append([]putRecord(nil), puts...)
	}
}

// The target is the ticket's own last message plus N business days at the
// target hour, in the account's offset.
func TestTicketsPushEndDatesCmd_TargetsLastMessage(t *testing.T) {
	setNow(t, time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC))
	setTimeZone(t, "")

	srv, puts := pushEndFixture(t,
		`{"tickets":[{"id":10,"planned_end_date":null,"created_at":"2026-09-01T10:00:00+04:00"}],"meta":{"has_next":false}}`,
		map[int64]string{10: "2026-09-11T09:00:00+04:00"}) // Friday

	out := captureStdout(t, func() {
		err := (&TicketsPushEndDatesCmd{Yes: true, Days: 3, TargetHour: 17, PerPage: 100}).Run(context.Background(), newTestClient(srv.URL))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	// Fri 11 Sep + 3 business days = Wed 16 Sep, at 17:00 in the ticket offset.
	want := `{"planned_end_date":"2026-09-16T17:00:00+04:00"}`
	got := puts()
	if len(got) != 1 {
		t.Fatalf("expected 1 PUT, got %d (output: %s)", len(got), out)
	}
	if got[0].Body != want {
		t.Errorf("expected %s, got %s", want, got[0].Body)
	}
}

// Each ticket counts from its own last message, so the targets differ.
func TestTicketsPushEndDatesCmd_TargetsEachTicketSeparately(t *testing.T) {
	setNow(t, time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC))
	setTimeZone(t, "")

	srv, puts := pushEndFixture(t,
		`{"tickets":[{"id":10,"planned_end_date":null,"created_at":"2026-09-01T10:00:00+04:00"},{"id":20,"planned_end_date":null,"created_at":"2026-09-01T10:00:00+04:00"}],"meta":{"has_next":false}}`,
		map[int64]string{
			10: "2026-09-11T09:00:00+04:00", // Fri -> Wed 16 Sep
			20: "2026-09-01T08:00:00+04:00", // Tue -> Fri 4 Sep
		})

	captureStdout(t, func() {
		err := (&TicketsPushEndDatesCmd{Yes: true, Days: 3, TargetHour: 17, PerPage: 100}).Run(context.Background(), newTestClient(srv.URL))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	bodies := map[string]bool{}
	for _, p := range puts() {
		bodies[p.Body] = true
	}
	if len(bodies) != 2 {
		t.Fatalf("expected 2 distinct targets, got %d: %v", len(bodies), bodies)
	}
	for _, want := range []string{
		`{"planned_end_date":"2026-09-16T17:00:00+04:00"}`,
		`{"planned_end_date":"2026-09-04T17:00:00+04:00"}`,
	} {
		if !bodies[want] {
			t.Errorf("expected a PUT with %s, got %v", want, bodies)
		}
	}
}

func TestTicketsPushEndDatesCmd_SkipsTicketsAlreadyAtTheTarget(t *testing.T) {
	setNow(t, time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC))
	setTimeZone(t, "")

	srv, puts := pushEndFixture(t,
		`{"tickets":[{"id":10,"planned_end_date":"2026-09-16T17:00:00+04:00","created_at":"2026-09-01T10:00:00+04:00"}],"meta":{"has_next":false}}`,
		map[int64]string{10: "2026-09-11T09:00:00+04:00"})

	out := captureStdout(t, func() {
		err := (&TicketsPushEndDatesCmd{Yes: true, Days: 3, TargetHour: 17, PerPage: 100}).Run(context.Background(), newTestClient(srv.URL))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	if got := puts(); len(got) != 0 {
		t.Errorf("expected no PUT for a ticket already at the target, got %v", got)
	}
	if !strings.Contains(out, "No changes needed.") {
		t.Errorf("expected a no-op message, got %q", out)
	}
}

// A ticket with no messages counts from created_at, like the PS helper.
func TestTicketsPushEndDatesCmd_FallsBackToCreatedAt(t *testing.T) {
	setNow(t, time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC))
	setTimeZone(t, "")

	srv, puts := pushEndFixture(t,
		`{"tickets":[{"id":10,"planned_end_date":null,"created_at":"2026-09-11T09:00:00+04:00"}],"meta":{"has_next":false}}`,
		map[int64]string{10: ""})

	captureStdout(t, func() {
		err := (&TicketsPushEndDatesCmd{Yes: true, Days: 3, TargetHour: 17, PerPage: 100}).Run(context.Background(), newTestClient(srv.URL))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	want := `{"planned_end_date":"2026-09-16T17:00:00+04:00"}`
	got := puts()
	if len(got) != 1 || got[0].Body != want {
		t.Errorf("expected one PUT with %s, got %v", want, got)
	}
}

func TestTicketsPushEndDatesCmd_FlagDefaults(t *testing.T) {
	var cli CLI
	parser, err := kong.New(&cli)
	if err != nil {
		t.Fatalf("kong.New: %v", err)
	}
	if _, err := parser.Parse([]string{"tickets", "push-end-dates"}); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if got := cli.Tickets.PushEndDates.Days; got != 3 {
		t.Errorf("expected 3 business days by default, got %d", got)
	}
	if got := cli.Tickets.PushEndDates.TargetHour; got != 17 {
		t.Errorf("expected a default target hour of 17, got %d", got)
	}
}

func TestTicketQueryList_SelfAssignedView(t *testing.T) {
	var got url.Values
	var calls int
	mux := http.NewServeMux()
	mux.HandleFunc("/api/_/tickets", func(w http.ResponseWriter, r *http.Request) {
		calls++
		got = r.URL.Query()
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"tickets":[{"id":10,"subject":"A"}],"meta":{"has_next":false}}`)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	tickets, err := SelfAssignedTickets(100).List(context.Background(), newTestClient(srv.URL), 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(tickets) != 1 || tickets[0].ID != 10 {
		t.Errorf("unexpected tickets: %+v", tickets)
	}
	if calls != 1 {
		t.Errorf("expected 1 call, got %d", calls)
	}
	if got.Get("per_page") != "100" || got.Get("order_by") != "created_at" || got.Get("order_type") != "asc" {
		t.Errorf("unexpected base params: %v", got)
	}
	if !strings.Contains(got.Get("query_hash"), `"responder_id"`) || !strings.Contains(got.Get("query_hash"), `"0"`) {
		t.Errorf("expected self-assigned query_hash, got %q", got.Get("query_hash"))
	}
}

func TestTicketQueryList_QueryJSONAndFilter(t *testing.T) {
	var got url.Values
	mux := http.NewServeMux()
	mux.HandleFunc("/api/_/tickets", func(w http.ResponseWriter, r *http.Request) {
		got = r.URL.Query()
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"tickets":[],"meta":{"has_next":false}}`)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	q := TicketQuery{PerPage: 50, QueryJSON: `{"filter":"123","tags":["a","b"]}`, Filter: 1100}
	if _, err := q.List(context.Background(), newTestClient(srv.URL), 2); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Get("filter") != "123" {
		t.Errorf("expected raw JSON string values passed through unquoted, got %v", got)
	}
	if got.Get("tags") != `["a","b"]` {
		t.Errorf("expected JSON-encoded arrays, got %v", got)
	}
	if got.Get("page") != "2" {
		t.Errorf("expected start page 2, got %v", got)
	}
}

func TestChangeSetPreview(t *testing.T) {
	cs := ChangeSet{
		{id: 10, field: "planned_end_date", from: "", to: "2026-08-07T12:15:00Z"},
		{id: 30, field: "priority", from: "1", to: "3"},
	}
	want := "[planned_end_date] ticket 10:  -> 2026-08-07T12:15:00Z\n[priority] ticket 30: 1 -> 3\n"
	if got := cs.Preview(); got != want {
		t.Errorf("expected %q, got %q", want, got)
	}
}

func TestChangeSetApply(t *testing.T) {
	var mu sync.Mutex
	var putCalls []struct {
		Path string
		Body []byte
	}
	mux := http.NewServeMux()
	for _, id := range []string{"10", "20"} {
		mux.HandleFunc("/api/_/tickets/"+id, func(w http.ResponseWriter, r *http.Request) {
			b, _ := io.ReadAll(r.Body)
			mu.Lock()
			putCalls = append(putCalls, struct {
				Path string
				Body []byte
			}{Path: r.URL.Path, Body: b})
			mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprint(w, `{"ticket":{}}`)
		})
	}
	srv := httptest.NewServer(mux)
	defer srv.Close()

	cs := ChangeSet{
		{id: 10, field: "priority", body: map[string]any{"priority": float64(3)}},
		{id: 20, field: "priority", body: map[string]any{"priority": float64(1)}},
	}
	out := captureStdout(t, func() {
		if err := cs.Apply(context.Background(), newTestClient(srv.URL)); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	if len(putCalls) != 2 {
		t.Fatalf("expected 2 PUTs, got %d", len(putCalls))
	}
	bodies := map[string]string{}
	for _, p := range putCalls {
		bodies[strings.TrimPrefix(p.Path, "/api/_/tickets/")] = string(p.Body)
	}
	if bodies["10"] != `{"priority":3}` || bodies["20"] != `{"priority":1}` {
		t.Errorf("unexpected bodies: %v", bodies)
	}
	if !strings.Contains(out, "Done: 2 applied") {
		t.Errorf("expected summary, got %q", out)
	}
}

func TestChangeSetApply_ReportsError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/_/tickets/10", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = fmt.Fprint(w, `{"errors":["boom"]}`)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	cs := ChangeSet{{id: 10, field: "priority", body: map[string]any{"priority": float64(3)}}}
	var err error
	out := captureStdout(t, func() {
		err = cs.Apply(context.Background(), newTestClient(srv.URL))
	})
	if err == nil || !strings.Contains(err.Error(), "update ticket 10") {
		t.Errorf("expected wrapped update error, got %v (out: %q)", err, out)
	}
}

// The pushed planned_end_date must carry the same timezone offset as the
// ticket's existing dates (the Freshservice account timezone), not the CLI
// machine's local zone.
func TestTicketLocation(t *testing.T) {
	dubai := time.FixedZone("GST", 4*3600)
	at := time.Date(2026, 8, 1, 10, 0, 0, 0, dubai)
	utc := time.Date(2026, 8, 2, 9, 0, 0, 0, time.UTC)

	// planned_end_date wins over created_at.
	got := ticketLocation([]Ticket{{PlannedEndDate: &at, CreatedAt: utc}})
	if got == nil || got != dubai {
		t.Errorf("expected planned_end_date zone, got %v", got)
	}
	// created_at fallback when no planned_end_date.
	got = ticketLocation([]Ticket{{CreatedAt: utc}})
	if got == nil || got != time.UTC {
		t.Errorf("expected created_at zone, got %v", got)
	}
	// no dates at all.
	if got := ticketLocation([]Ticket{{}}); got != nil {
		t.Errorf("expected nil for undated tickets, got %v", got)
	}
	// skips a nil planned_end_date before finding a later one.
	got = ticketLocation([]Ticket{{}, {PlannedEndDate: &at}})
	if got == nil || got != dubai {
		t.Errorf("expected later planned_end_date zone, got %v", got)
	}
}

// overviewFixture serves one unassigned ticket plus two self-assigned ones and
// counts how many times the unassigned view is fetched.
func overviewFixture(t *testing.T) (*httptest.Server, *int) {
	t.Helper()
	unassignedFetches := 0
	mux := http.NewServeMux()
	mux.HandleFunc("/api/_/tickets", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(r.URL.Query().Get("query_hash"), `"value":["-1"]`) {
			unassignedFetches++
			_, _ = fmt.Fprint(w, `{"tickets":[{"id":10103,"subject":"Unassigned printer ticket","priority":1,"status":2,"responder_id":-1,"created_at":"2026-08-01T00:00:00+04:00"}],"meta":{"has_next":false}}`)
			return
		}
		_, _ = fmt.Fprint(w, `{"tickets":[{"id":10100,"subject":"Mine one","priority":2,"status":2,"responder_id":3100,"created_at":"2026-07-29T16:42:48+04:00"},{"id":10101,"subject":"Mine two","priority":1,"status":4,"responder_id":3101,"created_at":"2026-07-28T10:00:00+04:00"}],"meta":{"has_next":false}}`)
	})
	mux.HandleFunc("/api/_/tickets/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"conversations":[],"meta":{"count":0}}`)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, &unassignedFetches
}

func TestTicketsOverviewCmd_SkipsUnassignedByDefault(t *testing.T) {
	setNow(t, time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC))
	srv, unassignedFetches := overviewFixture(t)

	out := captureStdout(t, func() {
		if err := (&TicketsOverviewCmd{OlderThanDays: 1, Page: 1, PerPage: 100}).Run(context.Background(), newTestClient(srv.URL)); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	if *unassignedFetches != 0 {
		t.Errorf("expected the unassigned view to be skipped entirely, fetched %d time(s)", *unassignedFetches)
	}
	if strings.Contains(out, "## Unassigned") {
		t.Errorf("expected no unassigned section by default:\n%s", out)
	}
	if strings.Contains(out, "10103") {
		t.Errorf("expected the unassigned ticket to be absent by default:\n%s", out)
	}
	if !strings.Contains(out, "Scanned 2 unresolved tickets (2 self-assigned)") {
		t.Errorf("expected a self-assigned-only summary:\n%s", out)
	}
}

func TestTicketsOverviewCmd_IncludeUnassigned(t *testing.T) {
	setNow(t, time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC))
	srv, unassignedFetches := overviewFixture(t)

	out := captureStdout(t, func() {
		if err := (&TicketsOverviewCmd{OlderThanDays: 1, Page: 1, PerPage: 100, IncludeUnassigned: true}).Run(context.Background(), newTestClient(srv.URL)); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	if *unassignedFetches != 1 {
		t.Errorf("expected the unassigned view to be fetched once, got %d", *unassignedFetches)
	}
	if !strings.Contains(out, "## Unassigned (1)") {
		t.Errorf("expected the unassigned section with --include-unassigned:\n%s", out)
	}
	if !strings.Contains(out, "10103") {
		t.Errorf("expected the unassigned ticket with --include-unassigned:\n%s", out)
	}
	if !strings.Contains(out, "Scanned 3 unresolved tickets (2 self-assigned, 1 unassigned)") {
		t.Errorf("expected the three-way summary with --include-unassigned:\n%s", out)
	}
}

func TestTicketsOverviewCmd_IncludeUnassignedFlagBinds(t *testing.T) {
	var cli CLI
	parser, err := kong.New(&cli)
	if err != nil {
		t.Fatalf("kong.New: %v", err)
	}
	if _, err := parser.Parse([]string{"tickets", "overview"}); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if cli.Tickets.Overview.IncludeUnassigned {
		t.Error("expected --include-unassigned to be off by default")
	}

	var custom CLI
	customParser, err := kong.New(&custom)
	if err != nil {
		t.Fatalf("kong.New: %v", err)
	}
	if _, err := customParser.Parse([]string{"tickets", "overview", "--include-unassigned"}); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if !custom.Tickets.Overview.IncludeUnassigned {
		t.Error("expected --include-unassigned to bind")
	}
}
