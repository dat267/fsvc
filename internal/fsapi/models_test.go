package fsapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestParseTicket(t *testing.T) {
	var raw map[string]any
	body := `{"id":10100,"subject":"Hi","priority":2,"urgency":1,"impact":3,"status":0,"responder_id":-1,"created_at":"2026-07-29T16:42:48+04:00","planned_start_date":null,"planned_end_date":"2026-08-01T00:00:00Z"}`
	if err := json.Unmarshal([]byte(body), &raw); err != nil {
		t.Fatal(err)
	}

	tk := ParseTicket(raw)
	if tk.ID != 10100 || tk.Subject != "Hi" || tk.Priority != 2 || tk.Urgency != 1 || tk.Impact != 3 {
		t.Errorf("scalar fields wrong: %+v", tk)
	}
	if tk.ResponderID == nil || *tk.ResponderID != -1 {
		t.Errorf("expected responder -1, got %v", tk.ResponderID)
	}
	if tk.CreatedAt.IsZero() {
		t.Error("expected created_at parsed")
	}
	if tk.HasPlannedStartDate() {
		t.Error("planned_start_date null should report not set")
	}
	if tk.PlannedEndDate == nil {
		t.Error("expected planned_end_date set")
	}
}

func TestParseTicket_NullResponder(t *testing.T) {
	var raw map[string]any
	_ = json.Unmarshal([]byte(`{"id":1,"responder_id":null}`), &raw)
	tk := ParseTicket(raw)
	if tk.ResponderID != nil {
		t.Errorf("expected nil responder for null, got %v", tk.ResponderID)
	}
}

func TestClient_ListTicketsTyped(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"tickets":[{"id":5,"subject":"A","priority":1}],"meta":{"has_next":false}}`))
	}))
	defer srv.Close()

	c := New(ClientConfig{BaseURL: srv.URL, ItildeskSession: "x"})
	tickets, hasNext, err := c.ListTickets(context.Background(), url.Values{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if hasNext {
		t.Error("expected has_next false")
	}
	if len(tickets) != 1 || tickets[0].ID != 5 || tickets[0].Subject != "A" {
		t.Errorf("unexpected tickets: %+v", tickets)
	}
}

func TestClient_LatestConversation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"conversations":[{"id":9,"incoming":true,"created_at":"2026-08-01T00:00:00Z"}]}`))
	}))
	defer srv.Close()

	c := New(ClientConfig{BaseURL: srv.URL, ItildeskSession: "x"})
	conv, err := c.LatestConversation(context.Background(), 7)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if conv == nil || conv.ID != 9 || !conv.Incoming {
		t.Errorf("unexpected conversation: %+v", conv)
	}
}

func TestClient_LatestConversationEmpty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"conversations":[],"meta":{"count":0}}`))
	}))
	defer srv.Close()

	c := New(ClientConfig{BaseURL: srv.URL, ItildeskSession: "x"})
	conv, err := c.LatestConversation(context.Background(), 7)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if conv != nil {
		t.Errorf("expected nil conversation, got %+v", conv)
	}
}
