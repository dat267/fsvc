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

func TestAutoCacheTrue(t *testing.T) {
	// tickets GET: cache=true added.
	q := autoCacheTrue(http.MethodGet, "tickets", url.Values{"per_page": {"30"}})
	if q.Get("cache") != "true" || q.Get("per_page") != "30" {
		t.Errorf("expected cache=true added to tickets GET, got %v", q)
	}

	// tickets conversations GET: cache=true added.
	q = autoCacheTrue(http.MethodGet, "tickets/5/conversations", nil)
	if q.Get("cache") != "true" {
		t.Errorf("expected cache=true on conversations, got %v", q)
	}

	// Non-tickets GET: untouched.
	q = autoCacheTrue(http.MethodGet, "users/1", url.Values{"x": {"y"}})
	if q.Get("cache") != "" {
		t.Errorf("expected no cache on users GET, got %v", q)
	}

	// tickets PUT: untouched (writes must not request cached responses).
	q = autoCacheTrue(http.MethodPut, "tickets/5", url.Values{"x": {"y"}})
	if q.Get("cache") != "" {
		t.Errorf("expected no cache on tickets PUT, got %v", q)
	}

	// Explicit cache=false is overridden (auto cache=true wins).
	q = autoCacheTrue(http.MethodGet, "tickets", url.Values{"cache": {"false"}})
	if q.Get("cache") != "true" {
		t.Errorf("expected explicit cache value overridden to true, got %v", q)
	}
}

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

	c := New(ClientConfig{BaseURL: srv.URL, Cookie: "helpdesk_node_session=abc", CSRF: "tok"})
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
	if gotCookie != "helpdesk_node_session=abc" {
		t.Errorf("expected verbatim cookie, got %q", gotCookie)
	}
	if gotCSRF != "tok" {
		t.Errorf("expected X-CSRF-Token tok, got %q", gotCSRF)
	}
}

func TestClient_SubdomainBaseURL(t *testing.T) {
	c := New(ClientConfig{Subdomain: "acme", Cookie: "x=1"})
	if c.baseURL != "https://acme.freshservice.com" {
		t.Errorf("expected https://acme.freshservice.com, got %s", c.baseURL)
	}
}

func TestClient_Update(t *testing.T) {
	c := New(ClientConfig{Subdomain: "acme", Cookie: "helpdesk_node_session=abc", CSRF: "tok"})

	c.Update(ClientConfig{Subdomain: "beta", Cookie: "new=1", CSRF: "newtok"})
	if c.baseURL != "https://beta.freshservice.com" {
		t.Errorf("expected beta base URL, got %s", c.baseURL)
	}
	if c.cookie != "new=1" || c.csrf != "newtok" {
		t.Errorf("expected updated credentials, got cookie=%q csrf=%q", c.cookie, c.csrf)
	}

	// Empty cookie/CSRF clear the previous credentials.
	c.Update(ClientConfig{Subdomain: "beta"})
	if c.cookie != "" || c.csrf != "" {
		t.Errorf("expected cleared credentials, got cookie=%q csrf=%q", c.cookie, c.csrf)
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
	if err == nil || !strings.Contains(err.Error(), "no session cookie") {
		t.Errorf("expected cookie error, got %v", err)
	}
}

func TestClient_Unauthorized(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = fmt.Fprint(w, `{"errors":[]}`)
	}))
	defer srv.Close()

	c := New(ClientConfig{BaseURL: srv.URL, Cookie: "x=1"})
	_, err := c.Get(context.Background(), "tickets", nil)
	if err == nil || !strings.Contains(err.Error(), "session cookie invalid or expired") {
		t.Errorf("expected session hint in error, got %v", err)
	}
}

func TestClient_Non2xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = fmt.Fprint(w, `{"errors":["nope"]}`)
	}))
	defer srv.Close()

	c := New(ClientConfig{BaseURL: srv.URL, Cookie: "x=1"})
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

	c := New(ClientConfig{BaseURL: srv.URL, Cookie: "helpdesk_node_session=abc", CSRF: "tok"})
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
	if gotCookie != "helpdesk_node_session=abc" {
		t.Errorf("expected verbatim cookie, got %q", gotCookie)
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

	c := New(ClientConfig{BaseURL: srv.URL, Cookie: "helpdesk_node_session=abc"})
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
