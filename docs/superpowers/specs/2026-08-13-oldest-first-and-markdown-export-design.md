# Ticket Extraction Ordering + Markdown Export Design

Date: 2026-08-13

## Summary

Two changes to `fsvc`:

1. Force every ticket/conversation extraction path to use **oldest-to-newest** ordering so output reads top-to-bottom in chronological order.
2. Remove PDF export support and replace it with **Markdown** (`.md`) export, keeping DOCX.

## Context

Ordering is currently inconsistent across commands:

| Flow | Current ordering | File |
|---|---|---|
| `tickets list` | newest-first (`order_type=desc` default) | `cmd/tickets.go:34` |
| `tickets conv` | server default (latest-first) | `cmd/tickets.go:85-97` |
| `tickets export` conversations | server default (latest-first) | `cmd/export.go:68` |
| `classify` bulk fetch | oldest-first (`asc`) | `cmd/tickets.go:224-225` |
| `fill`/`push`/`sync` bulk fetch | oldest-first (`asc`) | `cmd/tickets.go:602` |
| `LatestConversation` (classify) | newest-first (`desc`) — **required** | `cmd/api.go:24-25` |

PDF support lives in `cmd/pdf.go` (uses `github.com/go-pdf/fpdf` plus two embedded
DejaVu TTF fonts in `cmd/assets/`), dispatched from `cmd/export.go:41-42`, and is
covered by `TestTicketsExportCmd_PDF` in `cmd/export_test.go:78-94`. There is no
existing markdown document renderer (only `RenderTable` for terminal tables).

## Design

### 1. Oldest-to-newest ordering everywhere

- `cmd/tickets.go:34` — flip `TicketsListCmd.OrderType` default from `desc` to `asc`.
  The flag remains configurable via `enum:"desc,asc"`.
- `cmd/tickets.go:85-97` — `TicketsConvCmd.Run`: set `order_by=created_at` and
  `order_type=asc` on the query.
- `cmd/export.go:56-80` — `fetchExportDoc`: set `order_by=created_at` and
  `order_type=asc` on the conversations request so exports list conversations
  oldest-first.
- `cmd/api.go:21-38` — `LatestConversation` stays `desc`: classify relies on it to
  fetch the single most recent message. Not an extraction/reporting path.
- Bulk flows already use `asc`; no change.

### 2. Markdown export

- New file `cmd/markdown.go` with `renderMarkdown(doc *exportDoc) ([]byte, error)`.
- Output shape (mirrors the DOCX/PDF content, markdown syntax):

  ```markdown
  # Ticket #10100 — Printer not working

  Hello & hi

  ## Conversations

  ### Omar Saleh (incoming, 2026-08-01T10:30:00Z)
  Please fix the printer

  ### Nadia Rahman (outgoing, 2026-08-01T11:00:00Z)
  Will do
  ```

  - Title: `# Ticket #<display_id> — <subject>` (falls back to `id` like DOCX/PDF).
  - Description: stripped HTML, blank line after.
  - `## Conversations` heading; `(none)` paragraph when empty.
  - Per conversation an `### <author> (<incoming|outgoing>, <created_at>)` heading,
    then the stripped body text or `(no body)`.
- Reuses existing `stripHTML`, `exportField`, `conversationAuthor`.

### 3. PDF removal

- Delete `cmd/pdf.go`.
- Delete embedded fonts `cmd/assets/DejaVuSansCondensed.ttf` and
  `cmd/assets/DejaVuSansCondensed-Bold.ttf`.
- Remove `github.com/go-pdf/fpdf` from `go.mod` and `go.sum`.
- `cmd/export.go:17` — help: `Output file (.docx or .md)`.
- `cmd/export.go:28-29` — accept `.docx` and `.md`; error message:
  `unsupported output format %q (use .docx or .md)`.
- `cmd/export.go:41-42` — switch on `.docx` / `.md`.
- `cmd/tickets.go:20` — command help: `Export a ticket to a DOCX or Markdown file`.

### 4. Testing

- `cmd/export_test.go` — replace `TestTicketsExportCmd_PDF` with
  `TestTicketsExportCmd_Markdown`:
  - runs export to `ticket.md`;
  - asserts `# Ticket #10100 — Printer not working`, stripped description
    `Hello & hi`, `## Conversations`, both conversation headings, and bodies
    `Please fix the printer` / `Will do`;
  - asserts HTML tags (`<p>`) are absent.
- `cmd/tickets_test.go:55-56` — `TestTicketsListCmd_Table` asserts
  `order_type=asc` (was `desc`).
- `TestTicketsConvCmd` — assert `order_by=created_at` and `order_type=asc` on the
  request.
- `TestTicketsExportCmd_UnsupportedExt` unchanged (`.txt` still rejected).

### 5. Verification

- `go build ./...`
- `go test ./...`
- `go vet ./...`

## Out of scope

- No change to `LatestConversation` ordering (classify depends on newest-first).
- No change to bulk flows' `asc` ordering (already correct).
- No markdown table generation changes; `RenderTable` untouched.
