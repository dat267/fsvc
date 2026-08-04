# Freshservice Private API Notes

Reverse-engineered knowledge from HAR captures and user verification. The
private API (`/api/_/`) is undocumented; these notes are the source of truth
until verified against the real instance.

## Filtering (query_hash)

`query_hash` accepts a URL-encoded array of condition objects. Verified from a
real request (the "unresolved" ticket view in the `get_tickets/22889` HAR's
Referer header):

```json
[{"condition":"status","operator":"is_in","value":["0"],"type":"default"}]
```

- `condition` — field name (`status`, `responder_id`, `created_at`, ...)
- `operator` — `is`, `is_in`, `is_greater_than`, and others
- `value` — array for `is_in` (strings), scalar for `is`/`is_greater_than`
- `type` — `default`
- `workspace_id` is NOT required in `query_hash` (the unresolved view omits it)

`tickets categories` queries **all unresolved** tickets (no responder_id
condition) so the unassigned list can be derived:
```json
[{"condition":"status","operator":"is_in","value":["0"],"type":"default"}]
```

The mutation commands (fill-*, sync-*) query **self-assigned** unresolved
tickets:
```json
[{"condition":"status","operator":"is_in","value":["0"],"type":"default"},
 {"condition":"responder_id","operator":"is_in","value":["0"],"type":"default"}]
```

### Status values

- `status = 0` — unresolved (user-confirmed; `is_in ["0"]` in the unresolved view)
- `status in [2, 3]` — the "My Open and Pending Tickets" saved filter from the
  HAR uses this; 2 = Open, 3 = Pending

### responder_id values (user-confirmed)

- `responder_id = -1` — **unassigned**
- `responder_id = 0` — **assigned to self**
- Note: the saved-filter HAR used `responder_id is_in [0]` for "My Open and
  Pending Tickets", which reads as "assigned to me" in that context.

## Unassigned detection

`tickets categories` treats a ticket as unassigned when the response
`responder_id` is `null` or `-1`.

## Endpoints

| Method | Path | Notes |
| --- | --- | --- |
| GET | `/api/_/tickets` | list; query: `filter`, `advanced_query_hash`, `query_hash`, `include`, `order_by`, `order_type`, `page`, `per_page`; returns `{tickets, meta}` |
| GET | `/api/_/tickets/{id}/conversations` | query: `include`, `per_page`, `order_by`, `order_type`; returns `{conversations, meta}` |
| GET | `/api/_/ticket_filters/{id}` | returns `{ticket_filter}` |
| GET | `/api/_/users/{id}` | returns `{user}` |
| PUT | `/api/_/tickets/{id}` | requires `X-CSRF-Token`; returns `{meta, ticket}` |

## Auth

- Session cookies (`helpdesk_node_session`, `_itildesk_session`); the server
  rotates `_itildesk_session` via `Set-Cookie` on responses.
- Writes additionally require `X-CSRF-Token`.
- Response header confirms the private API:
  `x-freshservice-api-version: latest=v2; requested=private`.

## Open questions (unverified)

- What message kinds appear in conversations (notes, phone, system messages)?
- Does the server accept `advanced_query_hash` with raw conditions JSON, or
  does it expect a different format (500 errors suggested the former)?
- `per_page` maximum for the private API.

## Resolved

- Conversations are returned **latest first** by default (user-confirmed). The
  `tickets categories` command explicitly requests
  `order_by=created_at&order_type=desc&per_page=1` and reads the first item as
  the latest message.
