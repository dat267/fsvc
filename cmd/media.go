package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
)

var imgSrcRe = regexp.MustCompile(`(?i)<img\b[^>]*\bsrc=["']([^"']+)["']`)

// imageSrcs returns every <img src="..."> value in an HTML string.
func imageSrcs(html string) []string {
	matches := imgSrcRe.FindAllStringSubmatch(html, -1)
	out := make([]string, 0, len(matches))
	for _, m := range matches {
		out = append(out, m[1])
	}
	return out
}

// resolveImageURL resolves an <img> src against the API base URL. Absolute
// URLs and protocol-relative URLs are returned unchanged.
func resolveImageURL(base, src string) string {
	if strings.HasPrefix(src, "http://") || strings.HasPrefix(src, "https://") || strings.HasPrefix(src, "//") {
		return src
	}
	return strings.TrimSuffix(base, "/") + "/" + strings.TrimPrefix(src, "/")
}

// gatherMedia downloads images referenced by the ticket description and
// conversation bodies, plus image-type attachments, and records non-image
// attachments as metadata. Downloads that fail are skipped without error.
func gatherMedia(ctx context.Context, client *Client, doc *exportDoc) error {
	seen := map[string]bool{}
	add := func(owner, id string, data []byte, mime, name string) {
		if seen[id] {
			return
		}
		seen[id] = true
		doc.Images = append(doc.Images, exportImage{ID: id, Data: data, Mime: mime, Name: name, Owner: owner})
	}

	fetch := func(owner, src string) {
		resolved := resolveImageURL(client.BaseURL(), src)
		data, err := client.Download(ctx, resolved)
		if err != nil {
			return
		}
		name := resolved[strings.LastIndex(resolved, "/")+1:]
		add(owner, resolved, data, http.DetectContentType(data), name)
	}

	for _, src := range imageSrcs(exportField(doc.Ticket, "description")) {
		fetch("ticket", src)
	}
	walkAttachments(ctx, client, doc, "ticket", doc.Ticket, add)

	for _, c := range doc.Conversations {
		owner := "conv-" + exportField(c, "id")
		for _, src := range imageSrcs(exportField(c, "body")) {
			fetch(owner, src)
		}
		walkAttachments(ctx, client, doc, owner, c, add)
	}
	return nil
}

func walkAttachments(ctx context.Context, client *Client, doc *exportDoc, owner string, obj map[string]any, add func(owner, id string, data []byte, mime, name string)) {
	atts, _ := obj["attachments"].([]any)
	for _, a := range atts {
		m, ok := a.(map[string]any)
		if !ok {
			continue
		}
		contentType := exportField(m, "content_type")
		name := exportField(m, "name")
		canonical := exportField(m, "canonical_url")
		if canonical == "" {
			canonical = exportField(m, "attachment_url")
		}
		if strings.HasPrefix(contentType, "image/") {
			data, err := client.Download(ctx, canonical)
			if err != nil {
				continue
			}
			// Dedupe against <img> srcs by resolved URL.
			id := resolveImageURL(client.BaseURL(), canonical)
			add(owner, id, data, http.DetectContentType(data), name)
		} else {
			doc.Attachments = append(doc.Attachments, exportAttachment{
				ID:          exportField(m, "id"),
				Name:        name,
				ContentType: contentType,
				Size:        int64Of(m["size"]),
				URL:         canonical,
			})
		}
	}
}

// Download fetches a raw URL on the same host as the API base, sending the
// session cookie.
func (c *Client) Download(ctx context.Context, rawURL string) ([]byte, error) {
	base := c.BaseURL()
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("parse download URL %q: %w", rawURL, err)
	}
	if u.Scheme != "https" && u.Scheme != "http" {
		return nil, fmt.Errorf("refusing to download non-http URL %q", rawURL)
	}
	baseURL, err := url.Parse(base)
	if err != nil {
		return nil, fmt.Errorf("parse base URL %q: %w", base, err)
	}
	if u.Host != baseURL.Host {
		return nil, fmt.Errorf("refusing to download off-host URL %q", rawURL)
	}
	return c.getRaw(ctx, rawURL)
}

// getRaw performs a GET to a full URL with the session cookie and no
// /api/_/ prefix, returning the raw body.
func (c *Client) getRaw(ctx context.Context, rawURL string) ([]byte, error) {
	c.mu.RLock()
	itildeskSession := c.itildeskSession
	c.mu.RUnlock()
	if itildeskSession == "" {
		return nil, errors.New("no session configured (run 'fsvc config set itildesk-session <value>')")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Cookie", itildeskSessionCookie+"="+itildeskSession)

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
