package cmd

import (
	"context"
	"encoding/json"
	"errors"
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
	List              TicketsListCmd              `cmd:"" help:"List tickets"`
	Conversations     TicketsConvCmd              `cmd:"" help:"List conversations for a ticket"`
	Classify          TicketsClassifyCmd          `cmd:"" help:"Categorize tickets into unassigned / awaiting agent / awaiting customer"`
	FillStartDates    TicketsFillStartDatesCmd    `cmd:"" help:"Backfill planned_start_date from created_at on your unresolved tickets"`
	FillEndDates      TicketsFillEndDatesCmd      `cmd:"" help:"Bulk-set planned_end_date to now + N days on your unresolved tickets"`
	SyncPriority      TicketsSyncPriorityCmd      `cmd:"" help:"Sync priority from urgency+impact via standard matrix"`
	SyncUrgencyImpact TicketsSyncUrgencyImpactCmd `cmd:"" help:"Set urgency+impact to the minimum pair that satisfies the current priority"`
	Update            TicketsUpdateCmd            `cmd:"" help:"Update a ticket"`
}

type TicketsListCmd struct {
	Format    string `help:"Output format" enum:"table,json,csv" default:"table"`
	Filter    int64  `help:"Ticket filter/view id"`
	Include   string `help:"Comma-separated fields to include"`
	OrderBy   string `help:"Field to order by" default:"created_at"`
	OrderType string `help:"Sort order" enum:"desc,asc" default:"desc"`
	Page      int    `help:"Page number" default:"1"`
	PerPage   int    `help:"Tickets per page" default:"30"`
}

var ticketsListColumns = []Column{
	{Header: "ID", Path: "id"},
	{Header: "Subject", Path: "subject"},
	{Header: "Status", Path: "status"},
	{Header: "Priority", Path: "priority"},
	{Header: "Requester", Path: "requester.name"},
	{Header: "Group", Path: "group_id"},
	{Header: "Created", Path: "created_at"},
}

func (c *TicketsListCmd) Run(ctx context.Context, client *Client) error {
	q := url.Values{}
	q.Set("order_by", c.OrderBy)
	q.Set("order_type", c.OrderType)
	q.Set("page", strconv.Itoa(c.Page))
	q.Set("per_page", strconv.Itoa(c.PerPage))
	if c.Filter != 0 {
		q.Set("filter", strconv.FormatInt(c.Filter, 10))
	}
	if c.Include != "" {
		q.Set("include", c.Include)
	}

	data, err := client.Get(ctx, "tickets", q)
	if err != nil {
		return err
	}
	return Print(data, "tickets", ticketsListColumns, c.Format)
}

type TicketsConvCmd struct {
	Format  string `help:"Output format" enum:"table,json,csv" default:"table"`
	Include string `help:"Comma-separated fields to include"`
	PerPage int    `help:"Conversations per page" default:"3"`
	ID      int64  `arg:"" help:"Ticket ID"`
}

var ticketsConvColumns = []Column{
	{Header: "ID", Path: "id"},
	{Header: "User", Path: "user_id"},
	{Header: "Incoming", Path: "incoming"},
	{Header: "Private", Path: "private"},
	{Header: "Created", Path: "created_at"},
	{Header: "Body", Path: "body_text"},
}

func (c *TicketsConvCmd) Run(ctx context.Context, client *Client) error {
	q := url.Values{}
	q.Set("per_page", strconv.Itoa(c.PerPage))
	if c.Include != "" {
		q.Set("include", c.Include)
	}

	path := fmt.Sprintf("tickets/%d/conversations", c.ID)
	data, err := client.Get(ctx, path, q)
	if err != nil {
		return err
	}
	return Print(data, "conversations", ticketsConvColumns, c.Format)
}

type TicketsClassifyCmd struct {
	OlderThanDays float64 `help:"Business days without responder activity before a ticket is listed as stale" default:"2"`
	Page          int     `help:"Page number" default:"1"`
	PerPage       int     `help:"Tickets per page" default:"100"`
	QueryJSON     string  `name:"query-json" help:"Raw JSON query params to pass to the tickets list endpoint"`
	Filter        int64   `arg:"" help:"Ticket filter/view ID (optional; default: unresolved tickets)" optional:""`
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

const categoriesWorkers = 8

func (c *TicketsClassifyCmd) Run(ctx context.Context, client *Client) error {
	now := nowInTZ()

	// Two targeted queries instead of scanning every unresolved ticket:
	//   1. unassigned unresolved tickets (responder_id = -1)
	//   2. self-assigned unresolved tickets (responder_id = 0) — the only set
	//      that needs the expensive per-ticket conversation scan.
	var unassigned, myTickets []Ticket

	if c.QueryJSON != "" || c.Filter != 0 {
		var err error
		unassigned, err = c.collectTickets(ctx, client, "")
		if err != nil {
			return err
		}
		myTickets = unassigned
	} else {
		var wg sync.WaitGroup
		var errUn, errMy error
		wg.Add(2)
		go func() {
			defer wg.Done()
			unassigned, errUn = c.collectTickets(ctx, client, unassignedHash)
		}()
		go func() {
			defer wg.Done()
			myTickets, errMy = c.collectTickets(ctx, client, selfAssignedHash)
		}()
		wg.Wait()
		if errUn != nil {
			return errUn
		}
		if errMy != nil {
			return errMy
		}
	}

	staleAgent, awaitingCustomer, err := classifyTickets(ctx, client, myTickets, c.OlderThanDays, now)
	if err != nil {
		return err
	}

	sort.Slice(unassigned, func(i, j int) bool {
		return unassigned[i].CreatedAt.Before(unassigned[j].CreatedAt)
	})
	sort.Slice(staleAgent, func(i, j int) bool {
		return staleAgent[i].lastMsgAt.Before(staleAgent[j].lastMsgAt)
	})
	sort.Slice(awaitingCustomer, func(i, j int) bool {
		return awaitingCustomer[i].lastMsgAt.Before(awaitingCustomer[j].lastMsgAt)
	})

	fmt.Printf("Scanned %d unresolved tickets (%d self-assigned, %d unassigned)\n\n", len(unassigned)+len(myTickets), len(myTickets), len(unassigned))

	fmt.Printf("## Unassigned (%d)\n\n", len(unassigned))
	printCatTable(toCatTickets(unassigned), client)
	fmt.Printf("\n## No activity from you for > %g business days, awaiting customer (%d)\n\n", c.OlderThanDays, len(staleAgent))
	printCatTable(staleAgent, client)
	fmt.Printf("\n## Last reply from someone else, awaiting agent (%d)\n\n", len(awaitingCustomer))
	printCatTable(awaitingCustomer, client)
	return nil
}

const (
	selfAssignedHash = `[{"condition":"status","operator":"is_in","value":["0"],"type":"default"},{"condition":"responder_id","operator":"is_in","value":["0"],"type":"default"}]`
	unassignedHash   = `[{"condition":"status","operator":"is_in","value":["0"],"type":"default"},{"condition":"responder_id","operator":"is_in","value":["-1"],"type":"default"}]`
)

// collectTickets paginates through the tickets list, collecting every page.
func (c *TicketsClassifyCmd) collectTickets(ctx context.Context, client *Client, defaultHash string) ([]Ticket, error) {
	// Build the base query once; only the page number changes per iteration.
	q := url.Values{}
	q.Set("per_page", strconv.Itoa(c.PerPage))
	q.Set("order_by", "created_at")
	q.Set("order_type", "asc")

	if c.QueryJSON != "" {
		var extra map[string]any
		if err := json.Unmarshal([]byte(c.QueryJSON), &extra); err != nil {
			return nil, fmt.Errorf("invalid --query-json: %w", err)
		}
		for k, v := range extra {
			if s, ok := v.(string); ok {
				q.Set(k, s)
				continue
			}
			b, _ := json.Marshal(v)
			q.Set(k, string(b))
		}
	} else if c.Filter != 0 {
		q.Set("filter", strconv.FormatInt(c.Filter, 10))
	} else {
		q.Set("query_hash", defaultHash)
	}

	return paginateTickets(ctx, client, q, c.Page)
}

// classifyTickets assigns each ticket to a category using a bounded worker
// pool. The conversation fetch per ticket is the slow part and runs
// concurrently; results are collected under a mutex.
func classifyTickets(ctx context.Context, client *Client, tickets []Ticket, olderThanDays float64, now time.Time) (staleAgent, awaitingCustomer []catTicket, _ error) {
	workers := categoriesWorkers
	if len(tickets) < workers {
		workers = len(tickets)
	}
	if workers == 0 {
		return nil, nil, nil
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

type TicketsUpdateCmd struct {
	Format string   `help:"Output format" enum:"table,json,csv" default:"table"`
	Body   string   `help:"Raw JSON body (overrides key=value pairs)"`
	ID     int64    `arg:"" help:"Ticket ID"`
	Pairs  []string `arg:"" help:"key=value pairs to update (e.g. priority=1)"`
}

var ticketsUpdateColumns = []Column{
	{Header: "ID", Path: "id"},
	{Header: "Subject", Path: "subject"},
	{Header: "Status", Path: "status"},
	{Header: "Priority", Path: "priority"},
	{Header: "Group", Path: "group_id"},
	{Header: "Responder", Path: "responder_id"},
	{Header: "Department", Path: "department_id"},
	{Header: "Updated", Path: "updated_at"},
}

func (c *TicketsUpdateCmd) Run(ctx context.Context, client *Client) error {
	var payload []byte
	var err error

	switch {
	case c.Body != "":
		if !json.Valid([]byte(c.Body)) {
			return fmt.Errorf("invalid JSON in --body")
		}
		payload = []byte(c.Body)
	case len(c.Pairs) > 0:
		payload, err = BuildBody(c.Pairs)
		if err != nil {
			return err
		}
	default:
		return errors.New("nothing to update: provide key=value pairs or --body")
	}

	path := fmt.Sprintf("tickets/%d", c.ID)
	data, err := client.Put(ctx, path, payload)
	if err != nil {
		return err
	}
	return Print(data, "ticket", ticketsUpdateColumns, c.Format)
}

// pendingChange describes a single ticket update for preview and apply.
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
		created := t.CreatedAt.Format(time.RFC3339)
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

// ---- fill-end-dates ---------------------------------------------------------

type TicketsFillEndDatesCmd struct {
	Yes     bool `help:"Skip confirmation prompt" name:"yes" short:"y"`
	Days    int  `help:"Business days from now to set as planned end date" default:"3"`
	PerPage int  `help:"Tickets per page" default:"100"`
}

func (c *TicketsFillEndDatesCmd) Run(ctx context.Context, client *Client) error {
	base := nowInTZ()
	target := AddBusinessDays(base, c.Days).Format(time.RFC3339)

	var changes []pendingChange
	if err := forEachMyTicket(ctx, client, c.PerPage, func(t Ticket) error {
		cur := ""
		if t.PlannedEndDate != nil {
			cur = t.PlannedEndDate.Format(time.RFC3339)
		}

		if cur != "" {
			if at, err := time.Parse(time.RFC3339, cur); err == nil && at.After(base) {
				return nil // already in the future, leave alone
			}
		}

		changes = append(changes, pendingChange{
			id:    t.ID,
			field: "planned_end_date",
			from:  cur,
			to:    target,
			body:  map[string]any{"planned_end_date": target},
		})
		return nil
	}); err != nil {
		return err
	}

	return previewAndApply(ctx, client, changes, c.Yes)
}

// ---- sync-ui ----------------------------------------------------------------

type TicketsSyncUrgencyImpactCmd struct {
	Yes     bool `help:"Skip confirmation prompt" name:"yes" short:"y"`
	PerPage int  `help:"Tickets per page" default:"100"`
}

func (c *TicketsSyncUrgencyImpactCmd) Run(ctx context.Context, client *Client) error {
	var changes []pendingChange

	if err := forEachMyTicket(ctx, client, c.PerPage, func(t Ticket) error {
		targetU, targetI, ok := MinUrgencyImpactForPriority(t.Priority)
		if !ok {
			return nil
		}

		if t.Urgency == targetU && t.Impact == targetI {
			return nil
		}
		changes = append(changes, pendingChange{
			id:    t.ID,
			field: fmt.Sprintf("priority=%d", t.Priority),
			from:  fmt.Sprintf("urgency=%d impact=%d", t.Urgency, t.Impact),
			to:    fmt.Sprintf("urgency=%d impact=%d", targetU, targetI),
			body:  map[string]any{"urgency": targetU, "impact": targetI},
		})
		return nil
	}); err != nil {
		return err
	}

	return previewAndApply(ctx, client, changes, c.Yes)
}

// ---- sync-priority ----------------------------------------------------------

type TicketsSyncPriorityCmd struct {
	Yes     bool `help:"Skip confirmation prompt" name:"yes" short:"y"`
	PerPage int  `help:"Tickets per page" default:"100"`
}

func (c *TicketsSyncPriorityCmd) Run(ctx context.Context, client *Client) error {
	var changes []pendingChange

	if err := forEachMyTicket(ctx, client, c.PerPage, func(t Ticket) error {
		target := PriorityFor(t.Urgency, t.Impact)
		if target == 0 || target == t.Priority {
			return nil
		}

		changes = append(changes, pendingChange{
			id:    t.ID,
			field: fmt.Sprintf("urgency=%d impact=%d", t.Urgency, t.Impact),
			from:  fmt.Sprintf("priority=%d", t.Priority),
			to:    fmt.Sprintf("priority=%d", target),
			body:  map[string]any{"priority": target},
		})
		return nil
	}); err != nil {
		return err
	}

	return previewAndApply(ctx, client, changes, c.Yes)
}

// ---- helpers ----------------------------------------------------------------

// forEachMyTicket paginates through self-assigned unresolved tickets and calls
// fn sequentially for each, using the list-level ticket data directly.
func forEachMyTicket(ctx context.Context, client *Client, perPage int, fn func(t Ticket) error) error {
	list, err := collectMyTickets(ctx, client, perPage)
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

// collectMyTickets paginates the self-assigned unresolved ticket list,
// returning each ticket's list-level data.
func collectMyTickets(ctx context.Context, client *Client, perPage int) ([]Ticket, error) {
	q := url.Values{"per_page": {strconv.Itoa(perPage)},
		"order_by": {"created_at"}, "order_type": {"asc"},
		"query_hash": {`[{"condition":"status","operator":"is_in","value":["0"],"type":"default"},{"condition":"responder_id","operator":"is_in","value":["0"],"type":"default"}]`}}
	return paginateTickets(ctx, client, q, 1)
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
	if len(changes) == 0 {
		fmt.Println("No changes needed.")
		return nil
	}
	for _, ch := range changes {
		fmt.Printf("[%s] ticket %d: %s -> %s\n", ch.field, ch.id, ch.from, ch.to)
	}
	if !yes && !confirmApply(len(changes)) {
		fmt.Println("Aborted.")
		return nil
	}

	// Build all payloads up front (sequential, cheap).
	payloads := make([][]byte, len(changes))
	for i, ch := range changes {
		payload, err := json.Marshal(ch.body)
		if err != nil {
			return fmt.Errorf("build payload for ticket %d: %w", ch.id, err)
		}
		payloads[i] = payload
	}

	// Apply PUTs concurrently.
	workers := categoriesWorkers
	if len(changes) < workers {
		workers = len(changes)
	}
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
				ch := changes[idx]
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
	for i := range changes {
		work <- i
	}
	close(work)
	wg.Wait()

	if firstErr != nil {
		return firstErr
	}
	fmt.Printf("Done: %d applied\n", len(changes))
	return nil
}
