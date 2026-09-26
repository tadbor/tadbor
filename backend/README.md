# Tadbor Backend (Go)

## First-time setup

This repo ships `go.mod` but not `go.sum` (it's generated from your machine's
module cache, not something to hand-author). Before the Docker build works:

```bash
cd backend
go mod tidy
```

This creates `go.sum`. Commit it once generated.

## Module layout

Each folder under `internal/` is one service from the architecture doc,
kept as a Go package rather than a separate deployable — see
`docs/rag-architecture-part2.md` §25 for why (modular monolith, not
microservices, at this stage).

- `quran/` — read-only, immutable Quran corpus (§5)
- `content/` — public API, serves only `review_status: published` explanations
- `retrieval/` — exact verse mapping + brute-force cosine similarity fallback
- `generation/` — the structured LLM contract (§15)
- `review/` — reviewer queue and approve/reject/edit decisions
- `ingestion/` — source registry (the actual ingestion batch job is a separate
  script you run against this same MongoDB — see the top-level README)
- `platform/` — shared Mongo connection and auth middleware stub
