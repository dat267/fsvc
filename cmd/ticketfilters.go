package cmd

import (
	"context"
	"fmt"

	"fsvc/internal/fsapi"
)

type TicketFiltersCmdGroup struct {
	Show TicketFiltersShowCmd `cmd:"" help:"Show a ticket filter"`
}

type TicketFiltersShowCmd struct {
	Format string `help:"Output format" enum:"table,json,csv" default:"table"`
	ID     int64  `arg:"" help:"Ticket filter ID"`
}

var ticketFilterColumns = []Column{
	{Header: "ID", Path: "id"},
	{Header: "Name", Path: "name"},
	{Header: "Visibility", Path: "visibility"},
	{Header: "User", Path: "user_id"},
	{Header: "Group", Path: "group_id"},
	{Header: "Order By", Path: "order_by"},
	{Header: "Order Type", Path: "order_type"},
	{Header: "Per Page", Path: "per_page"},
}

func (c *TicketFiltersShowCmd) Run(ctx context.Context, client *fsapi.Client) error {
	path := fmt.Sprintf("ticket_filters/%d", c.ID)
	data, err := client.Get(ctx, path, nil)
	if err != nil {
		return err
	}
	return Print(data, "ticket_filter", ticketFilterColumns, c.Format)
}
