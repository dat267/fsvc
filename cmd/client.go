package cmd

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
	Subdomain       string
	ItildeskSession string
	CSRF            string
	BaseURL         string
}

// itildeskSessionCookie is the cookie name whose value authenticates the
// private API (the other session cookies, e.g. helpdesk_node_session, are not
// required).
const itildeskSessionCookie = "_itildesk_session"

type Client struct {
	mu              sync.RWMutex
	baseURL         string
	itildeskSession string
	csrf            string
	http            *http.Client
}

func New(cfg ClientConfig) *Client {
	jar, _ := cookiejar.New(nil)
	baseURL := cfg.BaseURL
	if baseURL == "" && cfg.Subdomain != "" {
		baseURL = "https://" + cfg.Subdomain + ".freshservice.com"
	}
	return &Client{
		baseURL:         strings.TrimSuffix(baseURL, "/"),
		itildeskSession: cfg.ItildeskSession,
		csrf:            cfg.CSRF,
		http:            &http.Client{Timeout: 30 * time.Second, Jar: jar},
	}
}

// Update reconfigures the connection credentials. Called after Kong resolves
// flags/env/config-file values, before commands run. Empty session/CSRF values
// clear the previous credentials.
func (c *Client) Update(cfg ClientConfig) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if cfg.BaseURL != "" {
		c.baseURL = strings.TrimSuffix(cfg.BaseURL, "/")
	} else if cfg.Subdomain != "" {
		c.baseURL = "https://" + cfg.Subdomain + ".freshservice.com"
	}
	c.itildeskSession = cfg.ItildeskSession
	c.csrf = cfg.CSRF
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

func (c *Client) Do(ctx context.Context, method, path string, query url.Values, body []byte) ([]byte, error) {
	c.mu.RLock()
	baseURL := c.baseURL
	itildeskSession := c.itildeskSession
	csrf := c.csrf
	c.mu.RUnlock()

	if baseURL == "" {
		return nil, errors.New("no API base URL configured (set subdomain or --base-url; run 'fsvc config set subdomain <domain>')")
	}
	if itildeskSession == "" {
		return nil, errors.New("no session configured (run 'fsvc config set itildesk-session <value>')")
	}

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
	req.Header.Set("Cookie", itildeskSessionCookie+"="+itildeskSession)
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
		return fmt.Errorf("%s (session invalid or expired; run 'fsvc config set itildesk-session <value>')", msg)
	}
	return errors.New(msg)
}

// clientConfigFromCLI maps resolved CLI values to a ClientConfig.
func clientConfigFromCLI(cli *CLI) ClientConfig {
	return ClientConfig{
		Subdomain:       cli.Subdomain,
		ItildeskSession: cli.ItildeskSession,
		CSRF:            cli.CSRFToken,
		BaseURL:         cli.BaseURL,
	}
}
