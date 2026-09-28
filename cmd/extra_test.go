package cmd

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

func TestTicketFiltersShowCmd(t *testing.T) {
	srv := serveFixture(t, "ticket_filter.json", func(r *http.Request) {
		if r.URL.Path != "/api/_/ticket_filters/1100" {
			t.Errorf("expected path /api/_/ticket_filters/1100, got %s", r.URL.Path)
		}
	})

	out := captureStdout(t, func() {
		err := (&TicketFiltersShowCmd{ID: 1100}).Run(context.Background(), newTestClient(srv.URL))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	if !strings.Contains(out, "My Open and Pending Tickets") {
		t.Errorf("expected filter name in output:\n%s", out)
	}
}

func TestUsersShowCmd(t *testing.T) {
	srv := serveFixture(t, "user.json", func(r *http.Request) {
		if r.URL.Path != "/api/_/users/2100" {
			t.Errorf("expected path /api/_/users/2100, got %s", r.URL.Path)
		}
	})

	out := captureStdout(t, func() {
		err := (&UsersShowCmd{ID: 2100}).Run(context.Background(), newTestClient(srv.URL))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	if !strings.Contains(out, "Nadia Rahman") {
		t.Errorf("expected user name in output:\n%s", out)
	}
	if !strings.Contains(out, "nadia@example.com") {
		t.Errorf("expected user email in output:\n%s", out)
	}
}
