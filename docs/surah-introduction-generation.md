# Surah Introduction Generation

The introduction pilot is limited to Surah 12 and writes only to
`surah_introductions` with `review_status: pending`. It does not publish.

From `backend/`, preview the available source evidence without calling the model
or writing a draft:

```sh
go run ./cmd/generate_introduction
```

Generation and pending-draft persistence require an explicit opt-in:

```sh
go run ./cmd/generate_introduction -live
```

The command checks that the chunks are marked verified, the source is currently
verified and rights-cleared, and each chunk matches the source registry's pinned
version and content hash. Missing, stale, or conflicting provenance stops the
run before model invocation. Configure `MONGO_URI`, `LLM_API_URL`, and
`LLM_API_KEY` for the live path.

Model output is structured into sections and individually cited claims. The
pipeline rejects empty sections, empty claims, missing citations, and references
that do not resolve to evidence supplied to the model. Those mechanical checks
do not establish semantic entailment: a citation can still fail to support its
claim. Every draft therefore remains pending, and an authenticated reviewer
must inspect the evidence, complete the introduction checklist, and approve it
before the public API can return it.

**Approval is currently disabled.** The existing dashboard login uses one shared
password and a free-text display name; neither establishes an individual
reviewer identity or qualification. The internal introduction decision endpoint
returns `503` even for callers with the shared password, and the dashboard does
not offer approval controls. Configure an identity provider with verified user
identities and an explicit reviewer authorization/qualification mapping before
enabling that endpoint. Do not treat `reviewer_id` from a request as identity.

No production generation or publication is part of tests. Tests use controlled
evidence and generator/store doubles; Mongo-backed review tests run only when
`MONGO_TEST_URI` is explicitly configured.