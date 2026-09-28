package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

type TicketsCmdGroup struct {
	Overview       TicketsOverviewCmd       `cmd:"" help:"Overview of your unresolved tickets: awaiting agent, waiting on customer"`
	FillStartDates TicketsFillStartDatesCmd `cmd:"" help:"Autofill planned_start_date from created_at on your unresolved tickets"`
	PushEndDates   TicketsPushEndDatesCmd   `cmd:"" help:"Update planned_end_date to now + N business days on your unresolved tickets"`
}

type TicketsOverviewCmd struct {
	OlderThanDays     float64 `help:"Business days waiting on the customer before flagging for follow-up/resolution" default:"2"`
	IncludeUnassigned bool    `name:"include-unassigned" help:"Also list unassigned tickets (not your queue; costs one extra request)"`
	Page              int     `help:"Page number" default:"1"`
	PerPage           int     `help:"Tickets per page" default:"100"`
	QueryJSON         string  `name:"query-json" help:"Raw JSON query params to pass to the tickets list endpoint"`
	Filter            int64   `arg:"" help:"Ticket filter/view ID (optional; default: unresolved tickets)" optional:""`
}

var classifyColumns = []Column{
	{Header: "Subject", Path: "subject"},
	{Header: "Link", Path: "link"},
	{Header: "Days", Path: "days"},
}

type catTicket struct {
	id        int64
	ticket    Ticket
	lastMsgAt time.Time
}

// toCatTickets wraps tickets with no last-message timestamp (the unassigned
// list, which never had a conversation scan).
func toCatTickets(tickets []Ticket) []catTicket {
	out := make([]catTicket, len(tickets))
	for i, t := range tickets {
		out[i] = catTicket{id: t.ID, ticket: t}
	}
	return out
}

func (c *TicketsOverviewCmd) Run(ctx context.Context, client *Client) error {
	now := nowInTZ()

	// Targeted queries instead of scanning every unresolved ticket:
	//   1. self-assigned unresolved tickets (responder_id = 0) — the set that
	//      needs the expensive per-ticket conversation scan
	//   2. unassigned unresolved tickets (responder_id = -1), only when asked
	//      for: they are not the operator's queue, and skipping the view saves
	//      a request
	var unassigned, myTickets []Ticket
	queryMode := c.QueryJSON != "" || c.Filter != 0

	switch {
	case queryMode:
		var err error
		myTickets, err = TicketQuery{PerPage: c.PerPage, QueryJSON: c.QueryJSON, Filter: c.Filter}.List(ctx, client, c.Page)
		if err != nil {
			return err
		}
	case c.IncludeUnassigned:
		var wg sync.WaitGroup
		var errUn, errMy error
		wg.Add(2)
		go func() {
			defer wg.Done()
			unassigned, errUn = UnassignedTickets(c.PerPage).List(ctx, client, c.Page)
		}()
		go func() {
			defer wg.Done()
			myTickets, errMy = SelfAssignedTickets(c.PerPage).List(ctx, client, c.Page)
		}()
		wg.Wait()
		if errUn != nil {
			return errUn
		}
		if errMy != nil {
			return errMy
		}
	default:
		var err error
		myTickets, err = SelfAssignedTickets(c.PerPage).List(ctx, client, c.Page)
		if err != nil {
			return err
		}
	}

	staleAgent, awaitingCustomer, err := classifyTickets(ctx, client, myTickets, c.OlderThanDays, now)
	if err != nil {
		return err
	}

	// The unassigned bucket is reported only on request. In query mode there is
	// no separate view, so it is derived from the queried tickets instead.
	var unassignedCat []catTicket
	if c.IncludeUnassigned {
		unassignedCat = toCatTickets(unassigned)
		if unassigned == nil {
			for _, t := range myTickets {
				if t.ResponderID == nil || *t.ResponderID < 0 {
					unassignedCat = append(unassignedCat, catTicket{id: t.ID, ticket: t})
				}
			}
		}
	}

	sort.Slice(unassignedCat, func(i, j int) bool {
		return unassignedCat[i].ticket.CreatedAt.Before(unassignedCat[j].ticket.CreatedAt)
	})
	sort.Slice(staleAgent, func(i, j int) bool {
		return staleAgent[i].lastMsgAt.Before(staleAgent[j].lastMsgAt)
	})
	sort.Slice(awaitingCustomer, func(i, j int) bool {
		return awaitingCustomer[i].lastMsgAt.Before(awaitingCustomer[j].lastMsgAt)
	})

	switch {
	case !c.IncludeUnassigned:
		fmt.Printf("Scanned %d unresolved tickets (%d self-assigned)\n\n", len(myTickets), len(myTickets))
	case unassigned == nil:
		// Query mode: the unassigned bucket came out of the same set.
		fmt.Printf("Scanned %d unresolved tickets (%d self-assigned, %d unassigned)\n\n", len(myTickets), len(myTickets)-len(unassignedCat), len(unassignedCat))
	default:
		fmt.Printf("Scanned %d unresolved tickets (%d self-assigned, %d unassigned)\n\n", len(unassignedCat)+len(myTickets), len(myTickets), len(unassignedCat))
	}

	if c.IncludeUnassigned {
		fmt.Printf("## Unassigned (%d)\n\n", len(unassignedCat))
		printCatTable(unassignedCat, client)
		fmt.Print("\n")
	}
	fmt.Printf("## Waiting on customer > %g business days — follow up or resolve (%d)\n\n", c.OlderThanDays, len(staleAgent))
	printCatTable(staleAgent, client)
	fmt.Printf("\n## Last reply from someone else, awaiting agent (%d)\n\n", len(awaitingCustomer))
	printCatTable(awaitingCustomer, client)
	return nil
}

// classifyTickets assigns each ticket to a category using a bounded worker
// pool. The conversation fetch per ticket is the slow part and runs
// concurrently; results are collected under a mutex.
func classifyTickets(ctx context.Context, client *Client, tickets []Ticket, olderThanDays float64, now time.Time) (staleAgent, awaitingCustomer []catTicket, _ error) {
	if len(tickets) == 0 {
		return nil, nil, nil
	}
	workers := poolSize(len(tickets))

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	work := make(chan Ticket, len(tickets))
	for _, t := range tickets {
		work <- t
	}
	close(work)

	var (
		wg       sync.WaitGroup
		mu       sync.Mutex
		firstErr error
		errOnce  sync.Once
	)

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for t := range work {
				if ctx.Err() != nil {
					return
				}
				entry, kind, err := classifyTicket(ctx, client, t, olderThanDays, now)
				if err != nil {
					errOnce.Do(func() {
						firstErr = err
						cancel()
					})
					return
				}
				mu.Lock()
				switch kind {
				case CategoryStaleAgent:
					staleAgent = append(staleAgent, entry)
				case CategoryCustomer:
					awaitingCustomer = append(awaitingCustomer, entry)
				}
				mu.Unlock()
			}
		}()
	}

	wg.Wait()

	if firstErr != nil {
		return nil, nil, firstErr
	}
	return staleAgent, awaitingCustomer, nil
}

// classifyTicket decides which category a single ticket belongs to.
func classifyTicket(ctx context.Context, client *Client, t Ticket, olderThanDays float64, now time.Time) (catTicket, Category, error) {
	entry := catTicket{id: t.ID, ticket: t}

	latest, err := client.LatestConversation(ctx, t.ID)
	if err != nil {
		return entry, CategoryNone, fmt.Errorf("ticket %d: %w", t.ID, err)
	}

	lastMsg := time.Time{}
	var lastUserID int64
	if latest != nil {
		lastMsg = latest.CreatedAt
		lastUserID = latest.UserID
	}

	cat := Classify(t.ResponderID, lastMsg, lastUserID, t.CreatedAt, olderThanDays, now)
	if cat == CategoryCustomer || cat == CategoryStaleAgent {
		entry.lastMsgAt = lastMsg
		if lastMsg.IsZero() {
			entry.lastMsgAt = t.CreatedAt
		}
	}
	return entry, cat, nil
}

func printCatTable(entries []catTicket, client *Client) {
	if len(entries) == 0 {
		fmt.Println("(none)")
		return
	}
	rows := make([]map[string]any, len(entries))
	for i, e := range entries {
		ref := e.lastMsgAt
		if ref.IsZero() {
			ref = e.ticket.CreatedAt
		}
		rows[i] = map[string]any{
			"subject": truncate(e.ticket.Subject, 40),
			"link":    fmt.Sprintf("%s/a/tickets/%d", client.BaseURL(), e.id),
			"days":    fmt.Sprintf("%.1f", BusinessDaysBetween(ref, nowInTZ())),
		}
	}
	fmt.Print(RenderTable(classifyColumns, rows))
}

func truncate(s string, max int) string {
	if max <= 3 || utf8.RuneCountInString(s) <= max {
		return s
	}
	targetRunes := max - 3
	count := 0
	for idx := range s {
		if count == targetRunes {
			return s[:idx] + "..."
		}
		count++
	}
	return s
}

type pendingChange struct {
	id    int64
	field string
	from  string
	to    string
	body  map[string]any
}

func confirmApply(n int) bool {
	fmt.Printf("\nApply %d changes? [y/N] ", n)
	var answer string
	_, _ = fmt.Scanln(&answer)
	return strings.ToLower(answer) == "y"
}

// ---- fill-start-dates -------------------------------------------------------

type TicketsFillStartDatesCmd struct {
	Yes     bool `help:"Skip confirmation prompt" name:"yes" short:"y"`
	PerPage int  `help:"Tickets per page" default:"100"`
}

func (c *TicketsFillStartDatesCmd) Run(ctx context.Context, client *Client) error {
	var changes []pendingChange

	if err := forEachMyTicket(ctx, client, c.PerPage, func(t Ticket) error {
		if t.HasPlannedStartDate() {
			return nil
		}
		if t.CreatedAt.IsZero() {
			return nil
		}
		created := roundUpQuarterHour(t.CreatedAt).Format(time.RFC3339)
		changes = append(changes, pendingChange{
			id:    t.ID,
			field: "planned_start_date",
			from:  "nil",
			to:    created,
			body:  map[string]any{"planned_start_date": created},
		})
		return nil
	}); err != nil {
		return err
	}

	return previewAndApply(ctx, client, changes, c.Yes)
}

// ---- push-end-dates ---------------------------------------------------------

type TicketsPushEndDatesCmd struct {
	Yes        bool `help:"Skip confirmation prompt" name:"yes" short:"y"`
	Days       int  `arg:"" help:"Business days after the ticket's last message to set as planned end date" default:"3"`
	TargetHour int  `help:"Hour of day (0-23) to land on; earlier targets are clamped forward" default:"17"`
	PerPage    int  `help:"Tickets per page" default:"100"`
}

func (c *TicketsPushEndDatesCmd) Run(ctx context.Context, client *Client) error {
	list, err := SelfAssignedTickets(c.PerPage).List(ctx, client, 1)
	if err != nil {
		return err
	}

	// planned_end_date is interpreted in the account timezone: --time-zone when
	// set, otherwise the offset the ticket dates evidence.
	now := nowInTZ()
	if tz == "" {
		if loc := ticketLocation(list); loc != nil {
			now = now.In(loc)
		}
	}
	loc := now.Location()

	// Every ticket is recomputed from its own last message, so the scan is per
	// ticket; it runs on the shared worker pool.
	latest, err := latestMessageTimes(ctx, client, list)
	if err != nil {
		return err
	}

	var changes []pendingChange
	for _, t := range list {
		base := latest[t.ID]
		if base.IsZero() {
			base = t.CreatedAt
		}
		if base.IsZero() {
			continue // nothing to count business days from
		}

		target := TargetEndDate(base.In(loc), c.Days, c.TargetHour, now)
		if !EndDateNeedsUpdate(t.PlannedEndDate, target) {
			continue
		}

		value := target.Format(time.RFC3339)
		cur := ""
		if t.PlannedEndDate != nil {
			cur = t.PlannedEndDate.Format(time.RFC3339)
		}
		changes = append(changes, pendingChange{
			id:    t.ID,
			field: "planned_end_date",
			from:  cur,
			to:    value,
			body:  map[string]any{"planned_end_date": value},
		})
	}

	return previewAndApply(ctx, client, changes, c.Yes)
}

// latestMessageTimes returns each ticket's most recent conversation time, or
// the zero time when it has no conversations. Fetches run on the configured
// worker pool; the first failure aborts the run.
func latestMessageTimes(ctx context.Context, client *Client, tickets []Ticket) (map[int64]time.Time, error) {
	if len(tickets) == 0 {
		return map[int64]time.Time{}, nil
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	work := make(chan Ticket, len(tickets))
	for _, t := range tickets {
		work <- t
	}
	close(work)

	var (
		wg       sync.WaitGroup
		mu       sync.Mutex
		times    = make(map[int64]time.Time, len(tickets))
		firstErr error
		errOnce  sync.Once
	)

	for i := 0; i < poolSize(len(tickets)); i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for t := range work {
				if ctx.Err() != nil {
					return
				}
				latest, err := client.LatestConversation(ctx, t.ID)
				if err != nil {
					errOnce.Do(func() {
						firstErr = fmt.Errorf("ticket %d conversations: %w", t.ID, err)
						cancel()
					})
					return
				}
				var at time.Time
				if latest != nil {
					at = latest.CreatedAt
				}
				mu.Lock()
				times[t.ID] = at
				mu.Unlock()
			}
		}()
	}
	wg.Wait()

	if firstErr != nil {
		return nil, firstErr
	}
	return times, nil
}

// ticketLocation returns the account timezone as evidenced by the ticket
// dates (planned_end_date preferred, created_at fallback), or nil when no
// ticket carries a date.
func ticketLocation(tickets []Ticket) *time.Location {
	for _, t := range tickets {
		if t.PlannedEndDate != nil {
			return t.PlannedEndDate.Location()
		}
	}
	for _, t := range tickets {
		if !t.CreatedAt.IsZero() {
			return t.CreatedAt.Location()
		}
	}
	return nil
}

// ---- helpers ----------------------------------------------------------------

// forEachMyTicket paginates through self-assigned unresolved tickets and calls
// fn sequentially for each, using the list-level ticket data directly.
func forEachMyTicket(ctx context.Context, client *Client, perPage int, fn func(t Ticket) error) error {
	list, err := SelfAssignedTickets(perPage).List(ctx, client, 1)
	if err != nil {
		return err
	}

	for _, t := range list {
		if err := fn(t); err != nil {
			return err
		}
	}
	return nil
}

// ticketView is a saved view (query_hash) for the tickets list endpoint.
type ticketView string

const (
	viewSelfAssigned ticketView = `[{"condition":"status","operator":"is_in","value":["0"],"type":"default"},{"condition":"responder_id","operator":"is_in","value":["0"],"type":"default"}]`
	viewUnassigned   ticketView = `[{"condition":"status","operator":"is_in","value":["0"],"type":"default"},{"condition":"responder_id","operator":"is_in","value":["-1"],"type":"default"}]`
)

// TicketQuery describes a tickets-list request: a named saved view, a raw
// query-json object, or a filter ID — plus the shared list ordering.
type TicketQuery struct {
	PerPage   int
	View      ticketView
	QueryJSON string
	Filter    int64
}

// SelfAssignedTickets queries self-assigned unresolved tickets.
func SelfAssignedTickets(perPage int) TicketQuery {
	return TicketQuery{PerPage: perPage, View: viewSelfAssigned}
}

// UnassignedTickets queries unassigned unresolved tickets.
func UnassignedTickets(perPage int) TicketQuery {
	return TicketQuery{PerPage: perPage, View: viewUnassigned}
}

// baseQuery builds the request params (everything except page). Exactly one
// source wins: query-json, then filter, then the named view.
func (q TicketQuery) baseQuery() (url.Values, error) {
	v := url.Values{}
	v.Set("per_page", strconv.Itoa(q.PerPage))
	v.Set("order_by", "created_at")
	v.Set("order_type", "asc")

	switch {
	case q.QueryJSON != "":
		var extra map[string]any
		if err := json.Unmarshal([]byte(q.QueryJSON), &extra); err != nil {
			return nil, fmt.Errorf("invalid --query-json: %w", err)
		}
		for k, val := range extra {
			if s, ok := val.(string); ok {
				v.Set(k, s)
				continue
			}
			b, _ := json.Marshal(val)
			v.Set(k, string(b))
		}
	case q.Filter != 0:
		v.Set("filter", strconv.FormatInt(q.Filter, 10))
	default:
		v.Set("query_hash", string(q.View))
	}
	return v, nil
}

// List fetches every page of the query starting at startPage.
func (q TicketQuery) List(ctx context.Context, client *Client, startPage int) ([]Ticket, error) {
	base, err := q.baseQuery()
	if err != nil {
		return nil, err
	}
	return paginateTickets(ctx, client, base, startPage)
}

// paginateTickets walks every page of a tickets query, appending results until
// the API reports no further pages. baseQuery may omit the page parameter.
func paginateTickets(ctx context.Context, client *Client, baseQuery url.Values, startPage int) ([]Ticket, error) {
	var tickets []Ticket
	page := startPage
	q := make(url.Values, len(baseQuery)+1)
	for k, vs := range baseQuery {
		vs2 := make([]string, len(vs))
		copy(vs2, vs)
		q[k] = vs2
	}
	for {
		q.Set("page", strconv.Itoa(page))

		pageTickets, hasNext, err := client.ListTickets(ctx, q)
		if err != nil {
			return nil, err
		}
		tickets = append(tickets, pageTickets...)
		if !hasNext {
			break
		}
		page++
	}
	return tickets, nil
}

func previewAndApply(ctx context.Context, client *Client, changes []pendingChange, yes bool) error {
	cs := ChangeSet(changes)
	if len(cs) == 0 {
		fmt.Println("No changes needed.")
		return nil
	}
	fmt.Print(cs.Preview())
	if !yes && !confirmApply(len(cs)) {
		fmt.Println("Aborted.")
		return nil
	}
	return cs.Apply(ctx, client)
}

// ChangeSet is a batch of ticket updates that can be previewed and applied.
type ChangeSet []pendingChange

// Preview renders one line per change: "[field] ticket <id>: <from> -> <to>".
func (cs ChangeSet) Preview() string {
	var b strings.Builder
	for _, ch := range cs {
		fmt.Fprintf(&b, "[%s] ticket %d: %s -> %s\n", ch.field, ch.id, ch.from, ch.to)
	}
	return b.String()
}

// Apply PUTs every change to the API concurrently (bounded worker pool) and
// prints per-ticket confirmations plus a final summary. Errors from
// individual PUTs are reported wrapped; the batch is not rolled back.
func (cs ChangeSet) Apply(ctx context.Context, client *Client) error {
	// Build all payloads up front (sequential, cheap).
	payloads := make([][]byte, len(cs))
	for i, ch := range cs {
		payload, err := json.Marshal(ch.body)
		if err != nil {
			return fmt.Errorf("build payload for ticket %d: %w", ch.id, err)
		}
		payloads[i] = payload
	}

	// Apply PUTs concurrently.
	workers := poolSize(len(cs))
	work := make(chan int)
	var (
		wg       sync.WaitGroup
		mu       sync.Mutex
		firstErr error
		errOnce  sync.Once
	)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for idx := range work {
				ch := cs[idx]
				path := fmt.Sprintf("tickets/%d", ch.id)
				if _, err := client.Put(ctx, path, payloads[idx]); err != nil {
					errOnce.Do(func() { firstErr = fmt.Errorf("update ticket %d: %w", ch.id, err) })
					continue
				}
				mu.Lock()
				fmt.Printf("OK: ticket %d\n", ch.id)
				mu.Unlock()
			}
		}()
	}
	for i := range cs {
		work <- i
	}
	close(work)
	wg.Wait()

	if firstErr != nil {
		return firstErr
	}
	fmt.Printf("Done: %d applied\n", len(cs))
	return nil
}
