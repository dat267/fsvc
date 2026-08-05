package fsapi

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"sync"
	"time"
)

type ClientConfig struct {
	Subdomain string
	Cookie    string
	CSRF      string
	BaseURL   string
}

type Client struct {
	mu      sync.RWMutex
	baseURL string
	cookie  string
	csrf    string
	http    *http.Client
}

func New(cfg ClientConfig) *Client {
	jar, _ := cookiejar.New(nil)
	baseURL := cfg.BaseURL
	if baseURL == "" && cfg.Subdomain != "" {
		baseURL = "https://" + cfg.Subdomain + ".freshservice.com"
	}
	return &Client{
		baseURL: strings.TrimSuffix(baseURL, "/"),
		cookie:  cfg.Cookie,
		csrf:    cfg.CSRF,
		http:    &http.Client{Timeout: 30 * time.Second, Jar: jar},
	}
}

// Update reconfigures the connection credentials. Called after Kong resolves
// flags/env/config-file values, before commands run. Empty cookie/CSRF values
// clear the previous credentials.
func (c *Client) Update(cfg ClientConfig) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if cfg.BaseURL != "" {
		c.baseURL = strings.TrimSuffix(cfg.BaseURL, "/")
	} else if cfg.Subdomain != "" {
		c.baseURL = "https://" + cfg.Subdomain + ".freshservice.com"
	}
	c.cookie = cfg.Cookie
	c.csrf = cfg.CSRF
}

func (c *Client) SetCSRF(token string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.csrf = token
}

func (c *Client) BaseURL() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.baseURL
}

func (c *Client) Get(ctx context.Context, path string, query url.Values) ([]byte, error) {
	return c.Do(ctx, http.MethodGet, path, query, nil)
}

func (c *Client) Put(ctx context.Context, path string, body []byte) ([]byte, error) {
	return c.Do(ctx, http.MethodPut, path, nil, body)
}

// ticketsListParams decorates the tickets list endpoint with cache=true for
// cached reads (observed in HAR). The API only accepts cache=true when paired
// with a named filter view; query-hash-only scans are rejected with 403
// access_denied. It only applies to the exact /api/_/tickets path;
// sub-resources like conversations or single-ticket GETs reject it.
func ticketsListParams(method, path string, query url.Values) url.Values {
	if method != http.MethodGet || path != "tickets" {
		return query
	}
	if query.Get("filter") == "" {
		return query
	}
	q := url.Values{}
	for k, vs := range query {
		for _, v := range vs {
			q.Add(k, v)
		}
	}
	q.Set("cache", "true")
	return q
}

func (c *Client) Do(ctx context.Context, method, path string, query url.Values, body []byte) ([]byte, error) {
	c.mu.RLock()
	baseURL := c.baseURL
	cookie := c.cookie
	csrf := c.csrf
	c.mu.RUnlock()

	if baseURL == "" {
		return nil, errors.New("no API base URL configured (set subdomain or --base-url; run 'fsvc config set subdomain <domain>')")
	}
	if cookie == "" {
		return nil, errors.New("no session cookie configured (run 'fsvc config set cookie <cookie>')")
	}

	query = ticketsListParams(method, path, query)
	u := baseURL + "/api/_/" + strings.TrimPrefix(path, "/")
	if len(query) > 0 {
		u += "?" + query.Encode()
	}

	var rd io.Reader
	if body != nil {
		rd = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, u, rd)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Cookie", cookie)
	if body != nil {
		req.Header.Set("Content-Type", "application/json; charset=utf-8")
	}
	if csrf != "" {
		req.Header.Set("X-CSRF-Token", csrf)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}()

	if err := c.CheckStatus(resp); err != nil {
		return nil, err
	}
	return io.ReadAll(resp.Body)
}

func (c *Client) CheckStatus(resp *http.Response) error {
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
	msg := fmt.Sprintf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return fmt.Errorf("%s (session cookie invalid or expired; run 'fsvc config set cookie ...')", msg)
	}
	return errors.New(msg)
}
