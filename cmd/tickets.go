package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"time"

	"fsvc/internal/fsapi"
)

type TicketsCmdGroup struct {
	List          TicketsListCmd       `cmd:"" help:"List tickets"`
	Conversations TicketsConvCmd       `cmd:"" help:"List conversations for a ticket"`
	Categorize    TicketsCategorizeCmd `cmd:"" help:"Categorize tickets into unassigned / awaiting agent / awaiting customer"`
	Update        TicketsUpdateCmd     `cmd:"" help:"Update a ticket"`
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

type TicketsCategorizeCmd struct {
	OlderThanDays float64 `help:"Days threshold for stale agent response" default:"2"`
	Page          int     `help:"Page number" default:"1"`
	PerPage       int     `help:"Tickets per page" default:"30"`
	QueryJSON     string  `name:"query-json" help:"Raw JSON query params to pass to the tickets list endpoint"`
	Filter        int64   `arg:"" help:"Ticket filter/view ID (optional; default: unresolved tickets)" optional:""`
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
	hasMsg    bool
}

func (c *TicketsCategorizeCmd) Run(ctx context.Context, client *fsapi.Client) error {
	q := url.Values{}
	q.Set("page", strconv.Itoa(c.Page))
	q.Set("per_page", strconv.Itoa(c.PerPage))
	q.Set("order_by", "created_at")
	q.Set("order_type", "asc")

	if c.QueryJSON != "" {
		var extra map[string]any
		if err := json.Unmarshal([]byte(c.QueryJSON), &extra); err != nil {
			return fmt.Errorf("invalid --query-json: %w", err)
		}
		for k, v := range extra {
			b, _ := json.Marshal(v)
			q.Set(k, string(b))
		}
	} else if c.Filter != 0 {
		q.Set("filter", strconv.FormatInt(c.Filter, 10))
	} else {
		q.Set("query_hash", `[{"condition":"status","operator":"is","value":-1,"type":"default"}]`)
	}

	data, err := client.Get(ctx, "tickets", q)
	if err != nil {
		return err
	}

	var doc struct {
		Tickets []map[string]any `json:"tickets"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return fmt.Errorf("parse tickets: %w", err)
	}

	var unassigned, staleAgent, awaitingCustomer []catTicket
	threshold := time.Now().Add(-time.Duration(c.OlderThanDays * float64(time.Hour) * 24))

	for _, t := range doc.Tickets {
		id := t["id"].(float64)
		entry := catTicket{id: id, ticket: t}

		if r := t["responder_id"]; r == nil || r == float64(0) {
			unassigned = append(unassigned, entry)
			continue
		}

		latest, hasMsg, err := fetchLatestConversation(ctx, client, id)
		if err != nil {
			return fmt.Errorf("ticket %.0f: %w", id, err)
		}
		if !hasMsg {
			continue
		}

		entry.hasMsg = true
		entry.incoming = latest.Incoming
		entry.lastMsgAt = latest.CreatedAt

		if latest.Incoming {
			awaitingCustomer = append(awaitingCustomer, entry)
		} else if latest.CreatedAt.Before(threshold) {
			staleAgent = append(staleAgent, entry)
		}
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
	fmt.Printf("\n# Agent replied > %.1f days, awaiting customer (%d)\n", c.OlderThanDays, len(staleAgent))
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
		if e.hasMsg {
			row["last_msg_at"] = e.lastMsgAt.Format("2006-01-02 15:04")
		}
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
