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
	"time"

	"fsvc/internal/fsapi"
)

type TicketsCmdGroup struct {
	List              TicketsListCmd              `cmd:"" help:"List tickets"`
	Conversations     TicketsConvCmd              `cmd:"" help:"List conversations for a ticket"`
	Categories        TicketsCategoriesCmd        `cmd:"" help:"Categorize tickets into unassigned / awaiting agent / awaiting customer"`
	FillStartDates    TicketsFillStartDatesCmd    `cmd:"" help:"Backfill planned_start_date from first_responded_at on your unresolved tickets"`
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

type TicketsCategoriesCmd struct {
	OlderThanDays int    `help:"Business days threshold for stale agent response" default:"2"`
	Page          int    `help:"Page number" default:"1"`
	PerPage       int    `help:"Tickets per page" default:"100"`
	QueryJSON     string `name:"query-json" help:"Raw JSON query params to pass to the tickets list endpoint"`
	Filter        int64  `arg:"" help:"Ticket filter/view ID (optional; default: unresolved tickets)" optional:""`
}

var categorizeColumns = []Column{
	{Header: "ID", Path: "id"},
	{Header: "Subject", Path: "subject"},
	{Header: "Priority", Path: "priority"},
	{Header: "Last msg", Path: "last_msg_at"},
	{Header: "Created", Path: "created_at"},
}

var categorizeColumnsNoMsg = []Column{
	{Header: "ID", Path: "id"},
	{Header: "Subject", Path: "subject"},
	{Header: "Priority", Path: "priority"},
	{Header: "Created", Path: "created_at"},
}

type catTicket struct {
	id        float64
	ticket    map[string]any
	lastMsgAt time.Time
	incoming  bool
}

func (c *TicketsCategoriesCmd) Run(ctx context.Context, client *fsapi.Client) error {
	threshold := subBusinessDays(nowInTZ(), c.OlderThanDays)

	var unassigned, staleAgent, awaitingCustomer []catTicket
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
				return fmt.Errorf("invalid --query-json: %w", err)
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
			q.Set("query_hash", `[{"condition":"status","operator":"is_in","value":["0"],"type":"default"}]`)
		}

		data, err := client.Get(ctx, "tickets", q)
		if err != nil {
			return err
		}

		var doc struct {
			Tickets []map[string]any `json:"tickets"`
			Meta    struct {
				HasNext bool `json:"has_next"`
			} `json:"meta"`
		}
		if err := json.Unmarshal(data, &doc); err != nil {
			return fmt.Errorf("parse tickets: %w", err)
		}

		for _, t := range doc.Tickets {
			id := t["id"].(float64)
			entry := catTicket{id: id, ticket: t}

			if r := t["responder_id"]; r == nil || r == float64(-1) {
				unassigned = append(unassigned, entry)
				continue
			}

			latest, hasMsg, err := fetchLatestConversation(ctx, client, id)
			if err != nil {
				return fmt.Errorf("ticket %.0f: %w", id, err)
			}
			if hasMsg {
				entry.incoming = latest.Incoming
				entry.lastMsgAt = latest.CreatedAt
			} else {
				created, ok := t["created_at"].(string)
				if !ok {
					return fmt.Errorf("ticket %.0f: missing created_at", id)
				}
				at, err := time.Parse(time.RFC3339, created)
				if err != nil {
					return fmt.Errorf("ticket %.0f: bad created_at %q: %w", id, created, err)
				}
				entry.lastMsgAt = at
			}

			if entry.incoming {
				awaitingCustomer = append(awaitingCustomer, entry)
			} else if entry.lastMsgAt.Before(threshold) {
				staleAgent = append(staleAgent, entry)
			}
		}

		if !doc.Meta.HasNext {
			break
		}
		page++
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

	fmt.Printf("# Unassigned (%d)\n", len(unassigned))
	printCatTable(unassigned, categorizeColumnsNoMsg)
	fmt.Printf("\n# Agent replied > %d business days, awaiting customer (%d)\n", c.OlderThanDays, len(staleAgent))
	printCatTable(staleAgent, categorizeColumns)
	fmt.Printf("\n# Customer replied, awaiting agent (%d)\n", len(awaitingCustomer))
	printCatTable(awaitingCustomer, categorizeColumns)
	return nil
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

func printCatTable(entries []catTicket, cols []Column) {
	if len(entries) == 0 {
		fmt.Println("(none)")
		return
	}
	rows := make([]map[string]any, len(entries))
	for i, e := range entries {
		row := map[string]any{
			"id":         e.id,
			"subject":    Lookup(e.ticket, "subject"),
			"priority":   Lookup(e.ticket, "priority"),
			"created_at": Lookup(e.ticket, "created_at"),
		}
		row["last_msg_at"] = e.lastMsgAt.Format("2006-01-02 15:04")
		rows[i] = row
	}
	fmt.Print(RenderTable(cols, rows))
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

	if err := forEachMyTicket(ctx, client, c.PerPage, func(id float64, ticket map[string]any) error {
		psd, hasPSD := ticket["planned_start_date"]
		if !hasPSD || psd != nil {
			return nil
		}
		stats, _ := ticket["stats"].(map[string]any)
		firstResp, _ := stats["first_responded_at"].(string)
		if firstResp == "" {
			return nil
		}
		changes = append(changes, pendingChange{id: id, field: "planned_start_date", from: "nil", to: firstResp})
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
	if err := forEachMyTicket(ctx, client, c.PerPage, func(id float64, ticket map[string]any) error {
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

	if err := forEachMyTicket(ctx, client, c.PerPage, func(id float64, ticket map[string]any) error {
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

	if err := forEachMyTicket(ctx, client, c.PerPage, func(id float64, ticket map[string]any) error {
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

func forEachMyTicket(ctx context.Context, client *fsapi.Client, perPage int, fn func(id float64, ticket map[string]any) error) error {
	page := 1
	for {
		q := url.Values{"page": {strconv.Itoa(page)}, "per_page": {strconv.Itoa(perPage)},
			"order_by": {"created_at"}, "order_type": {"asc"},
			"query_hash": {`[{"condition":"status","operator":"is_in","value":["0"],"type":"default"},{"condition":"responder_id","operator":"is_in","value":["0"],"type":"default"}]`}}
		data, err := client.Get(ctx, "tickets", q)
		if err != nil {
			return err
		}

		var doc struct {
			Tickets []struct {
				ID float64 `json:"id"`
			} `json:"tickets"`
			Meta struct {
				HasNext bool `json:"has_next"`
			} `json:"meta"`
		}
		if err := json.Unmarshal(data, &doc); err != nil {
			return fmt.Errorf("parse tickets list: %w", err)
		}

		for _, t := range doc.Tickets {
			fullData, err := client.Get(ctx, fmt.Sprintf("tickets/%.0f", t.ID), nil)
			if err != nil {
				return fmt.Errorf("get ticket %.0f: %w", t.ID, err)
			}
			var full struct {
				Ticket map[string]any `json:"ticket"`
			}
			if err := json.Unmarshal(fullData, &full); err != nil {
				return fmt.Errorf("parse ticket %.0f: %w", t.ID, err)
			}
			if err := fn(t.ID, full.Ticket); err != nil {
				return err
			}
		}

		if !doc.Meta.HasNext {
			break
		}
		page++
	}
	return nil
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

	// calls to Put happen after prompt; ctx comes from Run which stays valid.
	for _, ch := range changes {
		payload, err := buildBody(ch)
		if err != nil {
			return fmt.Errorf("build payload for ticket %.0f: %w", ch.id, err)
		}
		path := fmt.Sprintf("tickets/%.0f", ch.id)
		if _, err := client.Put(ctx, path, payload); err != nil {
			return fmt.Errorf("update ticket %.0f: %w", ch.id, err)
		}
		fmt.Printf("OK: ticket %.0f\n", ch.id)
	}
	fmt.Printf("Done: %d applied\n", len(changes))
	return nil
}
