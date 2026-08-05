package fsapi

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestClient_Get(t *testing.T) {
	var gotMethod, gotPath, gotCookie, gotCSRF string
	var gotQuery url.Values

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		gotQuery = r.URL.Query()
		gotCookie = r.Header.Get("Cookie")
		gotCSRF = r.Header.Get("X-CSRF-Token")
		w.Header().Set("Set-Cookie", "_itildesk_session=rotated123")
		_, _ = fmt.Fprint(w, `{"ok":true}`)
	}))
	defer srv.Close()

	c := New(ClientConfig{BaseURL: srv.URL, ItildeskSession: "abc", CSRF: "tok"})
	data, err := c.Get(context.Background(), "tickets", url.Values{"per_page": {"1"}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if string(data) != `{"ok":true}` {
		t.Errorf("expected body, got %q", data)
	}
	if gotMethod != http.MethodGet {
		t.Errorf("expected GET, got %s", gotMethod)
	}
	if gotPath != "/api/_/tickets" {
		t.Errorf("expected path /api/_/tickets, got %s", gotPath)
	}
	if gotQuery.Get("per_page") != "1" {
		t.Errorf("expected per_page=1, got %v", gotQuery)
	}
	if gotCookie != "_itildesk_session=abc" {
		t.Errorf("expected _itildesk_session cookie, got %q", gotCookie)
	}
	if gotCSRF != "tok" {
		t.Errorf("expected X-CSRF-Token tok, got %q", gotCSRF)
	}
}

func TestClient_SubdomainBaseURL(t *testing.T) {
	c := New(ClientConfig{Subdomain: "acme", ItildeskSession: "x"})
	if c.baseURL != "https://acme.freshservice.com" {
		t.Errorf("expected https://acme.freshservice.com, got %s", c.baseURL)
	}
}

func TestClient_Update(t *testing.T) {
	c := New(ClientConfig{Subdomain: "acme", ItildeskSession: "abc", CSRF: "tok"})

	c.Update(ClientConfig{Subdomain: "beta", ItildeskSession: "new", CSRF: "newtok"})
	if c.baseURL != "https://beta.freshservice.com" {
		t.Errorf("expected beta base URL, got %s", c.baseURL)
	}
	if c.itildeskSession != "new" || c.csrf != "newtok" {
		t.Errorf("expected updated credentials, got session=%q csrf=%q", c.itildeskSession, c.csrf)
	}

	// Empty session/CSRF clear the previous credentials.
	c.Update(ClientConfig{Subdomain: "beta"})
	if c.itildeskSession != "" || c.csrf != "" {
		t.Errorf("expected cleared credentials, got session=%q csrf=%q", c.itildeskSession, c.csrf)
	}

	// Explicit BaseURL wins over Subdomain.
	c.Update(ClientConfig{Subdomain: "beta", BaseURL: "http://x"})
	if c.baseURL != "http://x" {
		t.Errorf("expected BaseURL to win, got %s", c.baseURL)
	}
}

func TestClient_MissingConfig(t *testing.T) {
	c := New(ClientConfig{})
	_, err := c.Get(context.Background(), "tickets", nil)
	if err == nil || !strings.Contains(err.Error(), "no API base URL") {
		t.Errorf("expected base URL error, got %v", err)
	}

	c = New(ClientConfig{BaseURL: "http://x"})
	_, err = c.Get(context.Background(), "tickets", nil)
	if err == nil || !strings.Contains(err.Error(), "no session configured") {
		t.Errorf("expected session error, got %v", err)
	}
}

func TestClient_Unauthorized(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = fmt.Fprint(w, `{"errors":[]}`)
	}))
	defer srv.Close()

	c := New(ClientConfig{BaseURL: srv.URL, ItildeskSession: "x"})
	_, err := c.Get(context.Background(), "tickets", nil)
	if err == nil || !strings.Contains(err.Error(), "session invalid or expired") {
		t.Errorf("expected session hint in error, got %v", err)
	}
}

func TestClient_Non2xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = fmt.Fprint(w, `{"errors":["nope"]}`)
	}))
	defer srv.Close()

	c := New(ClientConfig{BaseURL: srv.URL, ItildeskSession: "x"})
	_, err := c.Get(context.Background(), "tickets/1", nil)
	if err == nil || !strings.Contains(err.Error(), "HTTP 404") {
		t.Errorf("expected HTTP 404 in error, got %v", err)
	}
}

func TestClient_Put(t *testing.T) {
	var gotMethod, gotPath, gotCookie, gotContentType string
	var gotBody []byte

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		gotCookie = r.Header.Get("Cookie")
		gotContentType = r.Header.Get("Content-Type")
		gotBody = make([]byte, r.ContentLength)
		_, _ = r.Body.Read(gotBody)
		w.Header().Set("X-CSRF-Token", "newtok")
		_, _ = fmt.Fprint(w, `{"ticket":{}}`)
	}))
	defer srv.Close()

	c := New(ClientConfig{BaseURL: srv.URL, ItildeskSession: "abc", CSRF: "tok"})
	data, err := c.Put(context.Background(), "tickets/10100", []byte(`{"priority":1}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if string(data) != `{"ticket":{}}` {
		t.Errorf("unexpected body %q", data)
	}
	if gotMethod != http.MethodPut {
		t.Errorf("expected PUT, got %s", gotMethod)
	}
	if gotPath != "/api/_/tickets/10100" {
		t.Errorf("expected path /api/_/tickets/10100, got %s", gotPath)
	}
	if !strings.HasPrefix(gotContentType, "application/json") {
		t.Errorf("expected application/json content type, got %q", gotContentType)
	}
	if string(gotBody) != `{"priority":1}` {
		t.Errorf("expected body, got %q", gotBody)
	}
	if gotCookie != "_itildesk_session=abc" {
		t.Errorf("expected _itildesk_session cookie, got %q", gotCookie)
	}
}

func TestClient_CookieJarRotation(t *testing.T) {
	var secondCookie string
	call := 0

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		call++
		if call == 1 {
			w.Header().Set("Set-Cookie", "_itildesk_session=rotated")
		} else {
			secondCookie = r.Header.Get("Cookie")
		}
		_, _ = fmt.Fprint(w, `{}`)
	}))
	defer srv.Close()

	c := New(ClientConfig{BaseURL: srv.URL, ItildeskSession: "abc"})
	if _, err := c.Get(context.Background(), "tickets", nil); err != nil {
		t.Fatalf("first request failed: %v", err)
	}
	if _, err := c.Get(context.Background(), "tickets", nil); err != nil {
		t.Fatalf("second request failed: %v", err)
	}

	if !strings.Contains(secondCookie, "_itildesk_session=rotated") {
		t.Errorf("expected rotated session cookie in second request, got %q", secondCookie)
	}
}
