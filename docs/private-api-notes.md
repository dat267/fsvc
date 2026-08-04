# Freshservice Private API Notes

Reverse-engineered knowledge from HAR captures and user verification. The
private API (`/api/_/`) is undocumented; these notes are the source of truth
until verified against the real instance.

## Filtering (advanced_query_hash)

`advanced_query_hash` is the ad-hoc filter parameter to use (user-preference;
`query_hash` is reserved for saved-filter semantics and is not used here). It
is an array of condition objects sent URL-encoded as a query parameter:

```json
[{"condition": "status", "operator": "is", "value": 0, "type": "default"}]
```

- `condition` — field name (`status`, `responder_id`, `workspace_id`, ...)
- `operator` — `is`, `is_in`, and others (unverified)
- `value` — scalar for `is`, array for `is_in`
- `type` — `default` (and likely `advanced`)

The `tickets categorize` command sends its default unresolved filter via
`advanced_query_hash`:
```json
[{"condition":"status","operator":"is","value":0,"type":"default"},
 {"condition":"workspace_id","operator":"is","value":2,"type":"default"}]
```

### Status values

- `status = 0` — unresolved (user-confirmed)
- `status in [2, 3]` — the "My Open and Pending Tickets" saved filter from the
  HAR uses this; 2 = Open, 3 = Pending

### responder_id values (user-confirmed, not yet used)

- `responder_id = -1` — **unassigned** (in advanced_query_hash)
- `responder_id = 0` — **assigned to self**
- Note: the saved-filter HAR used `responder_id is_in [0]` for "My Open and
  Pending Tickets", which reads as "assigned to me" in that context.

## Unassigned detection

Currently `tickets categorize` treats a ticket as unassigned when the response
`responder_id` is `null` or `0`. This may need revisiting against the
`advanced_query_hash` semantics above once we act on them.

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
- Does the server accept raw conditions JSON as the `advanced_query_hash`
  query param (URL-encoded), or does it expect a hash string?
- `per_page` maximum for the private API.

## Resolved

- Conversations are returned **latest first** by default (user-confirmed). The
  `tickets categorize` command explicitly requests
  `order_by=created_at&order_type=desc&per_page=1` and reads the first item as
  the latest message.
