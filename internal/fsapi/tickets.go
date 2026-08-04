package fsapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
)

// ListTickets fetches tickets matching query, returning them and whether more
// pages exist.
func (c *Client) ListTickets(ctx context.Context, query url.Values) ([]Ticket, bool, error) {
	data, err := c.Get(ctx, "tickets", query)
	if err != nil {
		return nil, false, err
	}
	return decodeTickets(data)
}

// LatestConversation returns the most recent conversation for a ticket, or
// nil when the ticket has none. Conversations are latest-first per the API.
func (c *Client) LatestConversation(ctx context.Context, ticketID int64) (*Conversation, error) {
	q := url.Values{
		"per_page":   {"1"},
		"order_by":   {"created_at"},
		"order_type": {"desc"},
	}
	data, err := c.Get(ctx, fmt.Sprintf("tickets/%d/conversations", ticketID), q)
	if err != nil {
		return nil, err
	}
	convs, err := decodeConversations(data)
	if err != nil {
		return nil, err
	}
	if len(convs) == 0 {
		return nil, nil
	}
	return &convs[0], nil
}

// UpdateTicket sends a PUT to update ticket fields.
func (c *Client) UpdateTicket(ctx context.Context, ticketID int64, body any) error {
	payload, err := json.Marshal(body)
	if err != nil {
		return err
	}
	_, err = c.Put(ctx, fmt.Sprintf("tickets/%d", ticketID), payload)
	return err
}
