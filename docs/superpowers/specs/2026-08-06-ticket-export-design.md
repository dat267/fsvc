# Ticket Export Command (tickets export)

Date: 2026-08-06
Status: Approved

## Goal

Add `fsvc tickets export <id> -o <file>` that extracts all data from a single
ticket and writes it to a DOCX or PDF file that copilot.com accepts for upload.

## Command shape

```
fsvc tickets export 22568 -o ticket.docx     # DOCX
fsvc tickets export 22568 -o ticket.pdf      # PDF
```

- `-o` is required; the file extension selects the format.
- Unsupported extension → error.

## Data gathered

Two private-API calls:

1. `GET /tickets/{id}` — full ticket object (90 fields: subject, status,
   priority, requester/responder names+ids, dates, tags, custom_fields,
   description, etc.). Kept as raw JSON; the export renders a curated subset.
2. `GET /tickets/{id}/conversations` — all conversations (`body_text`,
   `user_id`, `incoming`, `created_at`).

## Document structure

Minimal, AI-analysis focused: title (subject + ticket #), description, then the
conversation thread (author name, direction, timestamp, body). No summary table,
no tags, no custom fields, no raw IDs beyond the ticket number.

## Implementation

- `cmd/export.go` — `TicketsExportCmd`, gathers data, routes by extension.
- `cmd/docx.go` — hand-rolled DOCX writer (zip archive + minimal OOXML:
  `[Content_Types].xml`, `_rels/.rels`, `word/document.xml`). UTF-8 native.
- `cmd/pdf.go` — PDF writer using `github.com/go-pdf/fpdf`.
- HTML in `body_text`/`description` is stripped for the document.

## Fonts / encoding

- DOCX: UTF-8 native, no font asset needed.
- PDF: DejaVu Sans TTF (regular + bold) embedded via `//go:embed` from
  `cmd/assets/` and loaded with `AddUTF8FontFromBytes` — no runtime file
  resolution, works from any working directory.

## New dependency

`github.com/go-pdf/fpdf` (pure Go, no cgo).

## Files

- New: `cmd/export.go`, `cmd/docx.go`, `cmd/pdf.go`, `cmd/assets/DejaVuSans*.ttf`
- Tests: `cmd/export_test.go` (docx+pdf bytes non-empty, extension validation)
