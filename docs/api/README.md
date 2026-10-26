# Public API contract

`openapi.yaml` is the source of truth for the public backend contract used by
web and mobile. It documents the existing public routes and the planned
`GET /surahs` library response.

## Content states

- `200` with content: available.
- `200` with `[]`: available content type, no items for this exact selection.
- `202` with `{"status":"processing"}`: generation or review is in progress.
- `404` with `{"status":"unavailable"}`: no content for this exact surah/style.
- `5xx` with `{"error":"..."}`: an error, never rendered as empty content.

A missing style never falls back to another style. The canonical style IDs are
`simplified_ar`, `egyptian_ar` and `en`.

## Validation

Backend fixtures live in `backend/testdata/fixtures/contract/`. The manifest
maps each fixture to an operation and status in the spec. Contract tests load
and validate the spec, validate every fixture against its response schema, and
validate the real handlers for the routes that already exist.

```sh
cd backend && go test ./internal/contract -count=1 -v
```

CI runs this before the full backend suite. Any public response-shape change
must update the spec, the fixtures and the handler tests in the same pull
request.

Issues #34, #35 and #36 must add handler-conformance tests against this contract
when they implement their endpoints.
