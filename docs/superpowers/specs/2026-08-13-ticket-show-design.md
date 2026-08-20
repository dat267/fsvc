# Ticket Show Command Design

Date: 2026-08-13

## Summary

Add `fsvc tickets show <id>` to print a single ticket's full contents and its
conversation trace to stdout as readable Markdown. It complements the existing
`tickets export` (which writes an artifact to disk) and `tickets conversations`
(which prints a table).

## Design

### Command

- New `TicketsShowCmd` in `cmd/tickets.go`, registered on `TicketsCmdGroup.Show`:

  ```go
  type TicketsShowCmd struct {
      ID int64 `arg:"" help:"Ticket ID"`
  }
  ```

- Help text: `Show a ticket and its conversation trace as Markdown`.

### Flow

`TicketsShowCmd.Run(ctx, client) error`:

1. `fetchExportDoc(ctx, client, id)` — fetches the ticket and all conversations
   (conversations already fetched oldest-first).
2. `gatherMedia(ctx, client, doc)` — downloads inline `<img>` images and image
   attachments via the existing media pipeline, err is fatal on failure.
3. `renderTicketMarkdown(doc, outPath)` — render to a Markdown string.
4. Print the Markdown to stdout.

### Renderer — `cmd/markdown.go`

`renderTicketMarkdown(doc *exportDoc, outPath string) ([]byte, []exportAsset, error)`

Reuses existing helpers (`exportField`, `stripHTML`, `conversationAuthor`,
`writeMarkdownAttachments`, `imagesFor`, `markdownImage`, `assetRelPath`).

Output shape:

```
# Ticket #10100 — Printer not working

| Field      | Value
|------------|-------
| Status     | Open (2)
| Priority   | Medium (2)
| Urgency    | 1
| Impact     | 1
| Group      | Support (4001)
| Requester  | Omar Saleh (5001)
| Responder  | Nadia Rahman (3100)
| Department | IT
| Created    | 2026-08-01T10:00:00Z
| Updated    | 2026-08-02T09:00:00Z

Description

## Attachments

- manual.pdf (application/pdf, 192838 bytes): https://...

## Conversations

### Omar Saleh (incoming, 2026-08-01T10:30:00Z)
Please fix the printer

![assets/printer.png](assets/printer.png)

### Nadia Rahman (outgoing, 2026-08-01T11:00:00Z)
Will do
```

Metadata table rows are built from a fixed ordered list of `(label, key, nameKey)`
tuples; the name-key fields (`priority_name`, `status` string names, group name,
requester name, responder name) are preferred when the API provides them, falling
back to the raw id value via `exportField`.

Images referenced as `assets/...` relative paths, consistent with the export
command; assets are returned for callers that wish to write them alongside a
saved .md, and omitted (not embedded) for stdout.

### Being explicit

- Images are *referenced* by relative path, not base64-embedded, per decision.
- The show command prints to stdout and does not write files; `tickets export`
  remains the artifact-producing path.

## Tests

- `cmd/show_test.go`:
  - fixture serves a ticket (with all metadata fields + description + one
    image attachment) and two conversations (oldest and newest).
  - `TestTicketsShowCmd` runs `TicketsShowCmd{ID: 10100}` against the fixture and
    asserts: h1 title, metadata table rows (requester, status, created), the
    description text, the attachment list, both conversation headings in
    oldest-first order with stripped bodies, and that no `<p>` tags leak.
- Existing tests unchanged and passing.

## Verification

- `go build ./...`, `go test ./...`, `go vet ./...`, golangci-lint.