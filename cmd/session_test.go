package cmd

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSessionCmd_OK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/_/tickets" {
			t.Errorf("expected path /api/_/tickets, got %s", r.URL.Path)
		}
		if r.URL.Query().Get("per_page") != "1" {
			t.Errorf("expected per_page=1, got %v", r.URL.Query())
		}
		_, _ = fmt.Fprint(w, `{"meta":{"count":7},"tickets":[]}`)
	}))
	defer srv.Close()

	client := New(ClientConfig{BaseURL: srv.URL, ItildeskSession: "x=1"})
	out := captureStdout(t, func() {
		if err := (&SessionCmd{}).Run(context.Background(), client); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	if !strings.Contains(out, "OK: authenticated (visible tickets: 7)") {
		t.Errorf("expected success message, got %q", out)
	}
}

func TestSessionCmd_Unauthorized(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = fmt.Fprint(w, `{}`)
	}))
	defer srv.Close()

	client := New(ClientConfig{BaseURL: srv.URL, ItildeskSession: "x=1"})
	err := (&SessionCmd{}).Run(context.Background(), client)
	if err == nil || !strings.Contains(err.Error(), "session invalid or expired") {
		t.Errorf("expected session error, got %v", err)
	}
}
