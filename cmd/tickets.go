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

	"fsvc/internal/fsapi"
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

func (c *TicketsListCmd) Run(ctx context.Context, client *fsapi.Client) error {
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

func (c *TicketsConvCmd) Run(ctx context.Context, client *fsapi.Client) error {
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
	OlderThanDays int    `help:"Business days threshold for stale agent response" default:"2"`
	Page          int    `help:"Page number" default:"1"`
	PerPage       int    `help:"Tickets per page" default:"100"`
	QueryJSON     string `name:"query-json" help:"Raw JSON query params to pass to the tickets list endpoint"`
	Filter        int64  `arg:"" help:"Ticket filter/view ID (optional; default: unresolved tickets)" optional:""`
}

var classifyColumns = []Column{
	{Header: "Subject", Path: "subject"},
	{Header: "Link", Path: "link"},
	{Header: "Days", Path: "days"},
}

type catTicket struct {
	id        float64
	ticket    map[string]any
	lastMsgAt time.Time
	incoming  bool
}

const categoriesWorkers = 8

func (c *TicketsClassifyCmd) Run(ctx context.Context, client *fsapi.Client) error {
	threshold := subBusinessDays(nowInTZ(), c.OlderThanDays)

	// All unresolved tickets: used only to detect the unassigned list.
	// No conversation fetches happen here.
	allTickets, err := c.collectTickets(ctx, client, unresolvedHash)
	if err != nil {
		return err
	}
	var unassigned []catTicket
	for _, t := range allTickets {
		if isUnassigned(t) {
			unassigned = append(unassigned, catTicket{id: idOf(t), ticket: t})
		}
	}

	// Self-assigned unresolved tickets only: the expensive conversation
	// scan runs against this smaller set.
	myTickets, err := c.collectTickets(ctx, client, selfAssignedHash)
	if err != nil {
		return err
	}
	staleAgent, awaitingCustomer, err := classifyTickets(ctx, client, myTickets, threshold)
	if err != nil {
		return err
	}

	sort.Slice(unassigned, func(i, j int) bool {
		return ts(unassigned[i].ticket["created_at"]) < ts(unassigned[j].ticket["created_at"])
	})
	sort.Slice(staleAgent, func(i, j int) bool {
		return staleAgent[i].lastMsgAt.Before(staleAgent[j].lastMsgAt)
	})
	sort.Slice(awaitingCustomer, func(i, j int) bool {
		return awaitingCustomer[i].lastMsgAt.Before(awaitingCustomer[j].lastMsgAt)
	})

	fmt.Printf("# Unassigned (%d)\n\n", len(unassigned))
	printCatTable(unassigned, client)
	fmt.Printf("\n# Agent replied > %d business days, awaiting customer (%d)\n\n", c.OlderThanDays, len(staleAgent))
	printCatTable(staleAgent, client)
	fmt.Printf("\n# Customer replied, awaiting agent (%d)\n\n", len(awaitingCustomer))
	printCatTable(awaitingCustomer, client)
	return nil
}

const (
	unresolvedHash   = `[{"condition":"status","operator":"is_in","value":["0"],"type":"default"}]`
	selfAssignedHash = `[{"condition":"status","operator":"is_in","value":["0"],"type":"default"},{"condition":"responder_id","operator":"is_in","value":["0"],"type":"default"}]`
)

func idOf(t map[string]any) float64 {
	if id, ok := t["id"].(float64); ok {
		return id
	}
	return 0
}

func isUnassigned(t map[string]any) bool {
	return t["responder_id"] == nil || t["responder_id"] == float64(-1)
}

// collectTickets paginates through the tickets list, collecting every page.
func (c *TicketsClassifyCmd) collectTickets(ctx context.Context, client *fsapi.Client, defaultHash string) ([]map[string]any, error) {
	var tickets []map[string]any
	page := c.Page

	for {
		q := url.Values{}
		q.Set("page", strconv.Itoa(page))
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

		data, err := client.Get(ctx, "tickets", q)
		if err != nil {
			return nil, err
		}

		var doc struct {
			Tickets []map[string]any `json:"tickets"`
			Meta    struct {
				HasNext bool `json:"has_next"`
			} `json:"meta"`
		}
		if err := json.Unmarshal(data, &doc); err != nil {
			return nil, fmt.Errorf("parse tickets: %w", err)
		}

		tickets = append(tickets, doc.Tickets...)
		if !doc.Meta.HasNext {
			break
		}
		page++
	}

	return tickets, nil
}

// classifyTickets assigns each ticket to a category using a bounded worker
// pool. The conversation fetch per ticket is the slow part and runs
// concurrently; results are collected under a mutex.
func classifyTickets(ctx context.Context, client *fsapi.Client, tickets []map[string]any, threshold time.Time) (staleAgent, awaitingCustomer []catTicket, _ error) {
	workers := categoriesWorkers
	if len(tickets) < workers {
		workers = len(tickets)
	}
	if workers == 0 {
		return nil, nil, nil
	}

	work := make(chan map[string]any)
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
				entry, kind, err := classifyTicket(ctx, client, t, threshold)
				if err != nil {
					errOnce.Do(func() { firstErr = err })
					continue
				}
				mu.Lock()
				switch kind {
				case catStale:
					staleAgent = append(staleAgent, entry)
				case catCustomer:
					awaitingCustomer = append(awaitingCustomer, entry)
				}
				mu.Unlock()
			}
		}()
	}

	for _, t := range tickets {
		work <- t
	}
	close(work)
	wg.Wait()

	if firstErr != nil {
		return nil, nil, firstErr
	}
	return staleAgent, awaitingCustomer, nil
}

type catKind int

const (
	catNone catKind = iota
	catStale
	catCustomer
)

// classifyTicket decides which category a single ticket belongs to.
func classifyTicket(ctx context.Context, client *fsapi.Client, t map[string]any, threshold time.Time) (catTicket, catKind, error) {
	id := idOf(t)
	entry := catTicket{id: id, ticket: t}

	latest, hasMsg, err := fetchLatestConversation(ctx, client, id)
	if err != nil {
		return entry, catNone, fmt.Errorf("ticket %.0f: %w", id, err)
	}
	if hasMsg {
		entry.incoming = latest.Incoming
		entry.lastMsgAt = latest.CreatedAt
	} else {
		created, ok := t["created_at"].(string)
		if !ok {
			return entry, catNone, fmt.Errorf("ticket %.0f: missing created_at", id)
		}
		at, err := time.Parse(time.RFC3339, created)
		if err != nil {
			return entry, catNone, fmt.Errorf("ticket %.0f: bad created_at %q: %w", id, created, err)
		}
		entry.lastMsgAt = at
	}

	if entry.incoming {
		return entry, catCustomer, nil
	}
	if entry.lastMsgAt.Before(threshold) {
		return entry, catStale, nil
	}
	return entry, catNone, nil
}

type latestConversation struct {
	Incoming  bool
	CreatedAt time.Time
}

func fetchLatestConversation(ctx context.Context, client *fsapi.Client, ticketID float64) (latestConversation, bool, error) {
	q := url.Values{
		"per_page":   {"1"},
		"order_by":   {"created_at"},
		"order_type": {"desc"},
	}
	data, err := client.Get(ctx, fmt.Sprintf("tickets/%.0f/conversations", ticketID), q)
	if err != nil {
		return latestConversation{}, false, err
	}
	var doc struct {
		Conversations []struct {
			Incoming  bool   `json:"incoming"`
			CreatedAt string `json:"created_at"`
		} `json:"conversations"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return latestConversation{}, false, err
	}
	if len(doc.Conversations) == 0 {
		return latestConversation{}, false, nil
	}
	at, err := time.Parse(time.RFC3339, doc.Conversations[0].CreatedAt)
	if err != nil {
		return latestConversation{}, false, err
	}
	return latestConversation{Incoming: doc.Conversations[0].Incoming, CreatedAt: at}, true, nil
}

func printCatTable(entries []catTicket, client *fsapi.Client) {
	if len(entries) == 0 {
		fmt.Println("(none)")
		return
	}
	rows := make([]map[string]any, len(entries))
	for i, e := range entries {
		ref := e.lastMsgAt
		if ref.IsZero() {
			if created, ok := Lookup(e.ticket, "created_at").(string); ok {
				if at, err := time.Parse(time.RFC3339, created); err == nil {
					ref = at
				}
			}
		}
		subject, _ := Lookup(e.ticket, "subject").(string)
		rows[i] = map[string]any{
			"subject": truncate(subject, 40),
			"link":    fmt.Sprintf("%s/a/tickets/%.0f", client.BaseURL(), e.id),
			"days":    fmt.Sprintf("%.1f", businessDaysBetween(ref, nowInTZ())),
		}
	}
	fmt.Print(RenderTable(classifyColumns, rows))
}

func truncate(s string, max int) string {
	if max <= 3 || utf8.RuneCountInString(s) <= max {
		return s
	}
	runes := []rune(s)
	return string(runes[:max-3]) + "..."
}

func ts(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
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

func (c *TicketsUpdateCmd) Run(ctx context.Context, client *fsapi.Client) error {
	var payload []byte
	var err error

	switch {
	case c.Body != "":
		if !json.Valid([]byte(c.Body)) {
			return fmt.Errorf("invalid JSON in --body")
		}
		payload = []byte(c.Body)
	case len(c.Pairs) > 0:
		payload, err = fsapi.BuildBody(c.Pairs)
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

type pendingChange struct {
	id    float64
	field string
	from  string
	to    string
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

func (c *TicketsFillStartDatesCmd) Run(ctx context.Context, client *fsapi.Client) error {
	var changes []pendingChange

	if err := forEachMyTicket(ctx, client, c.PerPage, true, func(id float64, ticket map[string]any) error {
		psd, hasPSD := ticket["planned_start_date"]
		if !hasPSD || psd != nil {
			return nil
		}
		created, _ := ticket["created_at"].(string)
		if created == "" {
			return nil
		}
		changes = append(changes, pendingChange{id: id, field: "planned_start_date", from: "nil", to: created})
		return nil
	}); err != nil {
		return err
	}

	return previewAndApply(ctx, client, changes, c.Yes, func(ch pendingChange) ([]byte, error) {
		return json.Marshal(map[string]string{ch.field: ch.to})
	})
}

// ---- fill-end-dates ---------------------------------------------------------

type TicketsFillEndDatesCmd struct {
	Yes     bool `help:"Skip confirmation prompt" name:"yes" short:"y"`
	Days    int  `help:"Business days from now to set as planned end date" default:"3"`
	PerPage int  `help:"Tickets per page" default:"100"`
}

func (c *TicketsFillEndDatesCmd) Run(ctx context.Context, client *fsapi.Client) error {
	base := nowInTZ()
	target := addBusinessDays(base, c.Days).Format(time.RFC3339)

	var changes []pendingChange
	if err := forEachMyTicket(ctx, client, c.PerPage, true, func(id float64, ticket map[string]any) error {
		ped := ticket["planned_end_date"]
		cur := ""
		if s, ok := ped.(string); ok {
			cur = s
		}

		if cur != "" {
			if at, err := time.Parse(time.RFC3339, cur); err == nil && at.After(base) {
				return nil // already in the future, leave alone
			}
		}

		changes = append(changes, pendingChange{id: id, field: "planned_end_date", from: cur, to: target})
		return nil
	}); err != nil {
		return err
	}

	return previewAndApply(ctx, client, changes, c.Yes, func(ch pendingChange) ([]byte, error) {
		return json.Marshal(map[string]string{ch.field: ch.to})
	})
}

// ---- sync-ui ----------------------------------------------------------------

var minUIForPriority = map[float64]struct{ urgency, impact float64 }{
	1: {1, 1},
	2: {3, 1},
	3: {3, 2},
	4: {3, 3},
}

type TicketsSyncUrgencyImpactCmd struct {
	Yes     bool `help:"Skip confirmation prompt" name:"yes" short:"y"`
	PerPage int  `help:"Tickets per page" default:"100"`
}

func (c *TicketsSyncUrgencyImpactCmd) Run(ctx context.Context, client *fsapi.Client) error {
	var changes []pendingChange

	if err := forEachMyTicket(ctx, client, c.PerPage, false, func(id float64, ticket map[string]any) error {
		p, ok := ticket["priority"].(float64)
		if !ok {
			return nil
		}
		target, has := minUIForPriority[p]
		if !has {
			return nil
		}
		curU, _ := ticket["urgency"].(float64)
		curI, _ := ticket["impact"].(float64)

		if curU == target.urgency && curI == target.impact {
			return nil
		}
		changes = append(changes, pendingChange{
			id:    id,
			field: fmt.Sprintf("priority=%.0f", p),
			from:  fmt.Sprintf("urgency=%.0f impact=%.0f", curU, curI),
			to:    fmt.Sprintf("urgency=%.0f impact=%.0f", target.urgency, target.impact),
		})
		return nil
	}); err != nil {
		return err
	}

	return previewAndApply(ctx, client, changes, c.Yes, func(ch pendingChange) ([]byte, error) {
		parts := strings.Split(ch.to, " ")
		m := map[string]any{}
		for _, p := range parts {
			kv := strings.Split(p, "=")
			v, _ := strconv.ParseFloat(kv[1], 64)
			m[kv[0]] = v
		}
		return json.Marshal(m)
	})
}

// ---- sync-priority ----------------------------------------------------------

var priorityByUI = map[float64]map[float64]float64{
	1: {1: 1, 2: 1, 3: 2}, // Urgency Low
	2: {1: 1, 2: 2, 3: 3}, // Urgency Medium
	3: {1: 2, 2: 3, 3: 4}, // Urgency High
}

type TicketsSyncPriorityCmd struct {
	Yes     bool `help:"Skip confirmation prompt" name:"yes" short:"y"`
	PerPage int  `help:"Tickets per page" default:"100"`
}

func (c *TicketsSyncPriorityCmd) Run(ctx context.Context, client *fsapi.Client) error {
	var changes []pendingChange

	if err := forEachMyTicket(ctx, client, c.PerPage, false, func(id float64, ticket map[string]any) error {
		u, _ := ticket["urgency"].(float64)
		i, _ := ticket["impact"].(float64)
		p, _ := ticket["priority"].(float64)

		target := priorityByUI[u][i]
		if target == 0 || target == p {
			return nil
		}

		changes = append(changes, pendingChange{
			id:    id,
			field: fmt.Sprintf("urgency=%.0f impact=%.0f", u, i),
			from:  fmt.Sprintf("priority=%.0f", p),
			to:    fmt.Sprintf("priority=%.0f", target),
		})
		return nil
	}); err != nil {
		return err
	}

	return previewAndApply(ctx, client, changes, c.Yes, func(ch pendingChange) ([]byte, error) {
		parts := strings.Split(ch.to, "=")
		v, _ := strconv.ParseFloat(parts[1], 64)
		return json.Marshal(map[string]any{parts[0]: v})
	})
}

// ---- helpers ----------------------------------------------------------------

// forEachMyTicket paginates through self-assigned unresolved tickets and calls
// fn sequentially for each. When full is true, each ticket's complete data is
// fetched via /tickets/{id} concurrently first (needed for planned_* fields);
// otherwise the list-level ticket data is used directly.
func forEachMyTicket(ctx context.Context, client *fsapi.Client, perPage int, full bool, fn func(id float64, ticket map[string]any) error) error {
	list, err := collectMyTickets(ctx, client, perPage)
	if err != nil {
		return err
	}

	ids := make([]float64, len(list))
	for i, t := range list {
		ids[i] = idOf(t)
	}

	if !full {
		for i, id := range ids {
			if err := fn(id, list[i]); err != nil {
				return err
			}
		}
		return nil
	}

	fullTickets, err := fetchFullTickets(ctx, client, ids)
	if err != nil {
		return err
	}
	for i, id := range ids {
		if err := fn(id, fullTickets[i]); err != nil {
			return err
		}
	}
	return nil
}

// collectMyTickets paginates the self-assigned unresolved ticket list,
// returning each ticket's list-level data.
func collectMyTickets(ctx context.Context, client *fsapi.Client, perPage int) ([]map[string]any, error) {
	var tickets []map[string]any
	page := 1
	for {
		q := url.Values{"page": {strconv.Itoa(page)}, "per_page": {strconv.Itoa(perPage)},
			"order_by": {"created_at"}, "order_type": {"asc"},
			"query_hash": {`[{"condition":"status","operator":"is_in","value":["0"],"type":"default"},{"condition":"responder_id","operator":"is_in","value":["0"],"type":"default"}]`}}
		data, err := client.Get(ctx, "tickets", q)
		if err != nil {
			return nil, err
		}

		var doc struct {
			Tickets []map[string]any `json:"tickets"`
			Meta    struct {
				HasNext bool `json:"has_next"`
			} `json:"meta"`
		}
		if err := json.Unmarshal(data, &doc); err != nil {
			return nil, fmt.Errorf("parse tickets list: %w", err)
		}

		tickets = append(tickets, doc.Tickets...)
		if !doc.Meta.HasNext {
			break
		}
		page++
	}
	return tickets, nil
}

// fetchFullTickets GETs each ticket concurrently, returning them in input order.
func fetchFullTickets(ctx context.Context, client *fsapi.Client, ids []float64) ([]map[string]any, error) {
	full := make([]map[string]any, len(ids))
	if len(ids) == 0 {
		return full, nil
	}

	workers := categoriesWorkers
	if len(ids) < workers {
		workers = len(ids)
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
				id := ids[idx]
				data, err := client.Get(ctx, fmt.Sprintf("tickets/%.0f", id), nil)
				if err != nil {
					errOnce.Do(func() { firstErr = fmt.Errorf("get ticket %.0f: %w", id, err) })
					continue
				}
				var doc struct {
					Ticket map[string]any `json:"ticket"`
				}
				if err := json.Unmarshal(data, &doc); err != nil {
					errOnce.Do(func() { firstErr = fmt.Errorf("parse ticket %.0f: %w", id, err) })
					continue
				}
				mu.Lock()
				full[idx] = doc.Ticket
				mu.Unlock()
			}
		}()
	}

	for i := range ids {
		work <- i
	}
	close(work)
	wg.Wait()

	if firstErr != nil {
		return nil, firstErr
	}
	return full, nil
}

func previewAndApply(ctx context.Context, client *fsapi.Client, changes []pendingChange, yes bool, buildBody func(pendingChange) ([]byte, error)) error {
	if len(changes) == 0 {
		fmt.Println("No changes needed.")
		return nil
	}
	for _, ch := range changes {
		fmt.Printf("[%s] ticket %.0f: %s -> %s\n", ch.field, ch.id, ch.from, ch.to)
	}
	if !yes && !confirmApply(len(changes)) {
		fmt.Println("Aborted.")
		return nil
	}

	// Build all payloads up front (sequential, cheap).
	payloads := make([][]byte, len(changes))
	for i, ch := range changes {
		payload, err := buildBody(ch)
		if err != nil {
			return fmt.Errorf("build payload for ticket %.0f: %w", ch.id, err)
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
				path := fmt.Sprintf("tickets/%.0f", ch.id)
				if _, err := client.Put(ctx, path, payloads[idx]); err != nil {
					errOnce.Do(func() { firstErr = fmt.Errorf("update ticket %.0f: %w", ch.id, err) })
					continue
				}
				mu.Lock()
				fmt.Printf("OK: ticket %.0f\n", ch.id)
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
