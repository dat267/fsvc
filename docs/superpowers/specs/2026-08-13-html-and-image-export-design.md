# Ticket Export: HTML Format + Image/Attachment Support Design

Date: 2026-08-13

## Summary

Extend `fsvc tickets export` to:

1. Support a fourth output format: **HTML** (`.html`).
2. Embed **images** in all three rich formats (HTML, DOCX, Markdown).
3. Include **attachments** metadata (and images) in the output.

Images are downloaded at export time via the authenticated session and embedded
so the output is self-contained. Non-image attachments are listed by metadata,
not downloaded.

## Context

The private API returns, per ticket and conversation:

- `description` / `body` — rich HTML that may contain `<img>` tags
- `description_text` / `body_text` — plain text
- `attachments` — array of objects:
  `id`, `name`, `content_type`, `size`, `canonical_url`,
  `attachment_url` (pre-signed, expires)

The client (`cmd/client.go`) currently only performs `/api/_/` JSON requests
with the `_itildesk_session` cookie. There is no plain file-download helper.

Current export formats: `.docx` (cmd/docx.go) and `.md` (cmd/markdown.go),
wired through `cmd/export.go` (`TicketsExportCmd.Run`, `fetchExportDoc`).
Neither renders images or attachments today; `stripHTML` drops `<img>` tags.

## Design

### 1. Export document model

Extend `exportDoc` in `cmd/export.go`:

```go
type exportDoc struct {
    Ticket        map[string]any
    Conversations []map[string]any
    Images        []exportImage
    Attachments   []exportAttachment
}

type exportImage struct {
    ID    string // stable dedupe key (resolved URL or attachment id)
    Data  []byte // image bytes
    Mime  string // e.g. "image/png"
    Name  string // e.g. "printer.png"
    Owner string // "ticket" or "conv-<id>"; which section renders it
}

type exportAttachment struct {
    ID          string
    Name        string
    ContentType string
    Size        int64
    URL         string // canonical_url
}
```

### 2. Client.Download

New method in `cmd/client.go`:

```go
func (c *Client) Download(ctx context.Context, rawURL string) ([]byte, error)
```

- Only fetch URLs on the same host as `baseURL` (security guard).
- GET with the `_itildesk_session` cookie header and `CheckStatus`.
- Returns the raw body bytes.

### 3. Media gathering — `cmd/media.go` (new)

`gatherMedia(ctx, client, doc) error`:

- Extract `<img src="...">` from `description` and each conversation `body`
  (regex `(?i)<img\b[^>]*\bsrc="([^"]+)"`).
  - Resolve relative srcs against `client.BaseURL()`.
  - Dedupe by resolved URL.
  - Download via `Client.Download`; on failure, skip that image (don't abort
    the export) and continue.
  - Store `Mime` from the resolved URL / `Content-Type` guess via `http.DetectContentType`.
  - Owner is `"ticket"` for description images, `"conv-<id>"` for
    conversation body images.
- Walk `attachments` on the ticket and each conversation:
  - `content_type` starting with `image/` → download and add to `Images`
    (Owner set to the owning section; dedupe by attachment id).
  - everything else → add metadata to `Attachments` (name, content_type,
    size, canonical URL), no download.
- `gatherMedia` is called from `fetchExportDoc` (or `TicketsExportCmd.Run`)
  after the raw data is fetched. To keep `fetchExportDoc` testable without a
  real download server, call `gatherMedia` separately in `Run`.

### 4. HTML export — `cmd/html.go` (new)

`renderHTML(doc *exportDoc) ([]byte, error)` — a bare fragment, no stylesheet:

- `<h1>Ticket #<display> — <subject></h1>`
- Description: the raw `description` HTML with each `<img>` `src` rewritten to
  `data:<mime>;base64,<b64>`; if no rich description, fall back to escaped
  `description_text`. Since the fragment passes through trusted server HTML,
  embed it as-is (sanitized by the server), rewriting only `src`.
- `Attachments` for the ticket rendered as `<h2>Attachments</h2><ul>` of
  `name` / `size` / `URL` when non-empty.
- `<h2>Conversations</h2>`; each conversation as `<h3>` header (author,
  direction, timestamp) + its `body` HTML with image srcs rewritten, then
  `Images` with `Owner == "conv-<id>"` appended after the body, then an
  attachment `<ul>` when the conversation has non-empty `attachments`.
- Ticket-level `Images` (Owner `"ticket"`) appended after the description.

Implementation: add a helper `rewriteImageSrcs(html string, images map[string]string) string`
that replaces each `src` with the data URI keyed by resolved URL.

### 5. DOCX images — `cmd/docx.go`

- `renderDocx(doc *exportDoc)`.
- After the description paragraph, append each `Owner == "ticket"` image as a
  `<w:p>` with a `<w:drawing>` run. After each conversation body, append its
  `Owner == "conv-<id>"` images the same way.
- `buildDocx(documentXML string, images []exportImage)`:
  - write `word/media/<i>.<ext>` zip entries with image bytes
    (ext from mime: png/jpg/gif/webp),
  - add `<Relationship>` entries to `word/_rels/document.xml.rels`
    (type `.../image`), continue from rId2,
  - add `<Default Extension="<ext>" ContentType="<mime>"/>` entries to
    `[Content_Types].xml`,
  - document XML gains the proper namespaces (`r`, `wp`, `a`, `pic`).

### 6. Markdown images — `cmd/markdown.go`

- `renderMarkdown(doc *exportDoc, outPath string) ([]byte, []exportAsset, error)`
  where `exportAsset` is `{Name string; Data []byte}`.
- Write each image to `assets/<name>` next to the output file; the returned
  document references `assets/<name>`.
- Ticket-level images placed after the description; per-conversation images
  after each conversation body.
- `TicketsExportCmd.Run` writes the markdown body and each asset file to disk.
- Non-image attachments appended as a markdown list
  (`- <name> (<size>): <url>`).

### 7. Export plumbing — `cmd/export.go`

- `TicketsExportCmd.Out` help: `Output file (.docx, .md, or .html)`.
- Extension whitelist: `.docx`, `.md`, `.html`.
- `Run`:
  1. `fetchExportDoc` (raw ticket + conversations)
  2. `gatherMedia(ctx, client, doc)` (downloads images)
  3. switch on ext → `renderDocx`, `renderMarkdown`, or `renderHTML`
  4. write output file; for markdown also write asset files.

### 8. Tests

- `cmd/media_test.go`:
  - img src extraction from HTML description
  - relative src resolution against base URL
  - dedupe by URL
  - attachment walk: image → downloaded, non-image → metadata only
  - download failure → skipped, export continues
- `cmd/html_test.go`:
  - fragment contains `<h1>`, `data:image/...;base64`, rewritten srcs
  - attachment `<ul>` present
- `cmd/export_test.go`:
  - `TestTicketsExportCmd_HTML`: fixture server serves ticket +
    conversations + one image; asserts `.html` written with data URI and no error
  - `TestTicketsExportCmd_Docx`: assert `word/media/` and rels contain an image
    relationship (extend existing test or new one)
  - `TestTicketsExportCmd_Markdown`: assert `assets/` file written next to `.md`
- Keep existing tests passing.

### 9. Verification

- `go build ./...`
- `go test ./...`
- `go vet ./...`

## Out of scope

- No full HTML→OOXML fidelity (styled prose, links, exact image positions).
- No downloading of non-image attachments.
- No changes to `stripHTML` behavior for text-only outputs.
- No changes to ordering, classify, or other commands.
