package cmd

import (
	"encoding/json"
	"time"
)

// Ticket is a Freshservice ticket with the fields the CLI uses.
type Ticket struct {
	ID               int64
	Subject          string
	Priority         int
	Urgency          int
	Impact           int
	Status           int
	ResponderID      *int64
	CreatedAt        time.Time
	PlannedStartDate *time.Time
	PlannedEndDate   *time.Time
}

// HasPlannedStartDate reports whether the ticket's planned_start_date is set.
func (t Ticket) HasPlannedStartDate() bool { return t.PlannedStartDate != nil }

// Conversation is a single ticket conversation.
type Conversation struct {
	ID        int64
	Incoming  bool
	CreatedAt time.Time
}

// ParseTicket converts a decoded JSON ticket object into a typed Ticket.
func ParseTicket(raw map[string]any) Ticket {
	return Ticket{
		ID:               int64Of(raw["id"]),
		Subject:          stringOf(raw["subject"]),
		Priority:         intOf(raw["priority"]),
		Urgency:          intOf(raw["urgency"]),
		Impact:           intOf(raw["impact"]),
		Status:           intOf(raw["status"]),
		ResponderID:      ptrInt64Of(raw["responder_id"]),
		CreatedAt:        timeOf(raw["created_at"]),
		PlannedStartDate: ptrTimeOf(raw["planned_start_date"]),
		PlannedEndDate:   ptrTimeOf(raw["planned_end_date"]),
	}
}

func int64Of(v any) int64 {
	if f, ok := v.(float64); ok {
		return int64(f)
	}
	return 0
}

func stringOf(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

func intOf(v any) int {
	if f, ok := v.(float64); ok {
		return int(f)
	}
	return 0
}

// ptrInt64Of returns nil for absent/null values and negative values map to -1
// (the private API uses -1 for unassigned).
func ptrInt64Of(v any) *int64 {
	if f, ok := v.(float64); ok {
		i := int64(f)
		return &i
	}
	return nil
}

func timeOf(v any) time.Time {
	if s, ok := v.(string); ok {
		if t, err := time.Parse(time.RFC3339, s); err == nil {
			return t
		}
	}
	return time.Time{}
}

func ptrTimeOf(v any) *time.Time {
	if s, ok := v.(string); ok && s != "" {
		if t, err := time.Parse(time.RFC3339, s); err == nil {
			return &t
		}
	}
	return nil
}

// decodeTickets parses a tickets list response body into tickets + hasNext.
func decodeTickets(body []byte) ([]Ticket, bool, error) {
	var doc struct {
		Tickets []map[string]any `json:"tickets"`
		Meta    struct {
			HasNext bool `json:"has_next"`
		} `json:"meta"`
	}
	if err := json.Unmarshal(body, &doc); err != nil {
		return nil, false, err
	}
	tickets := make([]Ticket, len(doc.Tickets))
	for i, t := range doc.Tickets {
		tickets[i] = ParseTicket(t)
	}
	return tickets, doc.Meta.HasNext, nil
}

// decodeConversations parses a conversations response body.
func decodeConversations(body []byte) ([]Conversation, error) {
	var doc struct {
		Conversations []struct {
			ID        int64  `json:"id"`
			Incoming  bool   `json:"incoming"`
			CreatedAt string `json:"created_at"`
		} `json:"conversations"`
	}
	if err := json.Unmarshal(body, &doc); err != nil {
		return nil, err
	}
	convs := make([]Conversation, len(doc.Conversations))
	for i, c := range doc.Conversations {
		convs[i] = Conversation{ID: c.ID, Incoming: c.Incoming, CreatedAt: timeOf(c.CreatedAt)}
	}
	return convs, nil
}
