package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
)

type SessionCmd struct{}

func (c *SessionCmd) Run(ctx context.Context, client *Client) error {
	data, err := client.Get(ctx, "tickets", url.Values{"per_page": {"1"}})
	if err != nil {
		return err
	}
	var resp struct {
		Meta struct {
			Count int `json:"count"`
		} `json:"meta"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return fmt.Errorf("unexpected response: %w", err)
	}
	fmt.Printf("OK: authenticated (visible tickets: %d)\n", resp.Meta.Count)
	return nil
}
