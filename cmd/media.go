package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
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

// mediaJob is one image to download. name is the attachment name when known;
// otherwise the file name is derived from the URL.
type mediaJob struct {
	owner string
	src   string
	name  string
}

// gatherMedia downloads images referenced by the ticket description and
// conversation bodies, plus image-type attachments, and records non-image
// attachments as metadata. Downloads run on the configured worker pool and
// failures are skipped without error. Results keep request order so exports
// stay deterministic.
func gatherMedia(ctx context.Context, client *Client, doc *exportDoc) error {
	var jobs []mediaJob

	collectAttachments := func(owner string, atts []map[string]any) {
		for _, m := range atts {
			contentType := exportField(m, "content_type")
			name := exportField(m, "name")
			canonical := exportField(m, "canonical_url")
			if canonical == "" {
				canonical = exportField(m, "attachment_url")
			}
			if strings.HasPrefix(contentType, "image/") {
				jobs = append(jobs, mediaJob{owner: owner, src: canonical, name: name})
				continue
			}
			doc.Attachments = append(doc.Attachments, exportAttachment{
				ID:          exportField(m, "id"),
				Name:        name,
				ContentType: contentType,
				Size:        int64Of(m["size"]),
				URL:         canonical,
			})
		}
	}

	for _, src := range imageSrcs(doc.DescHTML) {
		jobs = append(jobs, mediaJob{owner: "ticket", src: src})
	}
	collectAttachments("ticket", attachmentsOf(doc.Ticket))
	for _, conv := range doc.Conversations {
		owner := "conv-" + conv.ID
		for _, src := range imageSrcs(conv.BodyHTML) {
			jobs = append(jobs, mediaJob{owner: owner, src: src})
		}
		collectAttachments(owner, conv.Attachments)
	}
	if len(jobs) == 0 {
		return nil
	}

	// One slot per job so results can be reassembled in request order.
	downloaded := make([]*exportImage, len(jobs))
	work := make(chan int, len(jobs))
	for i := range jobs {
		work <- i
	}
	close(work)

	var wg sync.WaitGroup
	for i := 0; i < poolSize(len(jobs)); i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for idx := range work {
				if ctx.Err() != nil {
					return
				}
				job := jobs[idx]
				resolved := resolveImageURL(client.BaseURL(), job.src)
				data, err := client.Download(ctx, resolved)
				if err != nil {
					continue
				}
				name := job.name
				if name == "" {
					name = resolved[strings.LastIndex(resolved, "/")+1:]
				}
				downloaded[idx] = &exportImage{
					ID:    resolved,
					Data:  data,
					Mime:  http.DetectContentType(data),
					Name:  name,
					Owner: job.owner,
				}
			}
		}()
	}
	wg.Wait()

	seen := make(map[string]bool, len(jobs))
	for _, img := range downloaded {
		if img == nil || seen[img.ID] {
			continue
		}
		seen[img.ID] = true
		doc.Images = append(doc.Images, *img)
	}
	return nil
}

// sameMediaHost reports whether host may serve media for an API at baseHost.
// It allows the exact API host plus sibling hosts sharing its registrable
// domain (e.g. acme.attachments.freshservice.com for acme.freshservice.com),
// but rejects unrelated hosts. Hosts without a registrable domain (IPs, bare
// names) require an exact match.
func sameMediaHost(baseHost, host string) bool {
	if baseHost == host {
		return true
	}
	if net.ParseIP(baseHost) != nil || net.ParseIP(host) != nil {
		return false
	}
	baseLabels := strings.Split(baseHost, ".")
	if len(baseLabels) < 3 {
		return false
	}
	registrable := strings.Join(baseLabels[len(baseLabels)-2:], ".")
	return strings.HasSuffix(host, "."+registrable)
}

// Download fetches a raw URL on the same host (or registrable domain) as the
// API base, sending the session cookie.
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
	if !sameMediaHost(baseURL.Host, u.Host) {
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
	c.captureSession(resp)
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}()

	if err := c.CheckStatus(resp); err != nil {
		return nil, err
	}
	return io.ReadAll(resp.Body)
}
