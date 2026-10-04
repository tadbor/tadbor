# Tafsir embeddings — Surah 12

Issue #6. Written 2026-10-04.

Issue #6 asks for a vector embedding on every chunk of `tafsir_chunks`, so the
semantic-fallback path in Part 1 §11 has something to rank. Without vectors, every
thematic lookup returns nothing and retrieval falls through to
`GAP_INSUFFICIENT_EVIDENCE`.

## Result

| Check | Result |
|---|---|
| Chunks audited | 454 |
| Chunks with a non-null, non-empty `embedding` | **454 / 454** |
| Width | 1024 floats, every chunk |
| `embedding_model` | one distinct value: `BAAI/bge-m3` |
| `embedding_normalized` | `true`, every chunk |
| `embedding_hash` recomputed from the stored vector | **454 / 454 match** |
| Provider calls for a full corpus | 15 at batch 32 (454 chunks) |
| Provider calls for a re-run with nothing to do | **0** |

The corpus was built and embedded in a throwaway probe database, then dropped. The
real `tadbor` database was never written to — `tafsir_chunks` there is still empty,
because `ibn-kathir-ar` is still `unverified` and issue #5 is still waiting on a
human. That is the same arrangement issue #5 used, and for the same reason: a probe
is not an approval.

## The model

`BAAI/bge-m3` through HuggingFace's router, 1024 dimensions, `normalize: true`.
Chosen and verified in `internal/embedding` (issue-era work that predates this issue);
this issue did not re-open the choice, it ran the batch over all 454 chunks and
verified the output.

Two properties of the provider were confirmed against the live endpoint rather than
assumed:

- **It handles Arabic.** The corpus is entirely `ar`, and every chunk came back at
  full width and unit length. A provider that silently truncated Arabic to empty
  would have produced vectors of the right shape and no meaning.
- **It is deterministic to ~1e-11, not bit-exact.** Three chunks in this corpus share
  a `content_hash` with another chunk (identical text, `dupIndex` distinguishing
  them). Two of those three groups have byte-identical vectors; one group differs at
  the 11th decimal place, cosine `0.999999999990`, purely because the two chunks
  landed in differently-shaped batches. This is what `embedding_hash` is and is not: it
  fingerprints the bytes as stored, so `-check` can prove a vector was not altered
  after the pipeline wrote it. It does **not** claim the vector is a reproducible
  function of the text.

## What was built

### `backend/cmd/embed_chunks`

The batch job. Preview by default; `-live` calls the provider and writes.

```
go run ./cmd/embed_chunks -check                  # audit only, no credentials needed
go run ./cmd/embed_chunks                         # preview the plan
go run ./cmd/embed_chunks -live                   # do it
go run ./cmd/embed_chunks -live -limit 32 -pace 2s  # bound the run, throttle harder
```

`-check` is the acceptance criterion turned into a gate. It answers three questions
and exits 1 if any chunk fails any of them:

1. **Coverage** — is there a vector at all?
2. **Provenance** — is it the current model, at the expected width, marked normalized?
3. **Integrity** — does `embedding_hash` still match the vector stored beside it?

Question 3 is the one that earns its keep. Every metadata field can look correct
while the vector next to it has been truncated or hand-edited, and retrieval computes
cosine similarity in Go over exactly that vector (docs/ADDENDUM-mongodb-pivot.md) —
there is no ANN index to reject it. Recomputing the fingerprint from the vector is the
only check available that needs neither the provider nor trust in an earlier run.

An empty collection is a **failure**, not a pass. "Every chunk has an embedding" is
vacuously true of zero chunks, and a green `-check` on the real `tadbor` database
today would be a light saying the pipeline had run when it has not:

```
$ go run ./cmd/embed_chunks -check
audited 0 chunk(s): 0 carry a vector, 0 need embedding
FAIL: tafsir_chunks holds no chunks. Ingestion has not run, so there is no vector to
verify — this is not a pass.
```

### `internal/ingestion/vectors.go`

`Store.AuditEmbeddings` reads the vector state of stored chunks; `Store.SetEmbeddings`
writes vectors onto chunks that already exist. Both exist because the ingest path
cannot do this job: `Service.IngestSource` refuses to persist a chunk it has not
embedded, so a vector can never be attached after the fact by the path that made it
necessary. A batch run also has to be interruptible, and an ingest that re-chunks the
whole document to fix 40 chunks is not.

`SetEmbeddings` is the only writer of `embedding` outside `UpsertChunks`, which makes
it the owner of the invariant the rest of the pipeline depends on: **a stored vector
always arrives with `embedding_model`, `embedding_dimensions`,
`embedding_normalized`, and `embedding_hash` beside it.** Retrieval reads a vector
with nothing to indicate which space it came from and nowhere to record that it could
not tell. It also refuses a vector that cannot be compared (zero, NaN, Inf,
unnormalized) at the write boundary, and fails loudly if an id is not in the index —
silently matching nothing is how a batch job reports success having embedded nothing.

### Why no registry gate

`cmd/ingest` refuses to write anything for a source that has not cleared
`SourceRegistry.IsUsable`, and that gate did not move. This job writes vectors, not
content: `internal/retrieval` filters on the `source_verified` flag denormalized onto
each chunk, and nothing here touches it, so embedding an unapproved source cannot
make it servable. Doing the arithmetic before approval also means approval is not
blocked on a job that re-runs in a minute.

## How it was verified

Against a probe database holding a copy of the registry with `ibn-kathir-ar` marked
verified, ingested from the committed manifest:

```
ingest -live        454 chunks written, 454 embedded, 15 provider calls
-check              PASS — 454/454, hash verified
```

Then the repair path, by nulling the vectors of 96 chunks (a run that died partway)
and corrupting one other chunk's `embedding_hash` (a bad hand edit):

| Step | `-check` | Provider calls | Written |
|---|---|---|---|
| after the damage | **FAIL** — 97 need embedding (96 no vector, 1 hash mismatch) | 0 | 0 |
| preview, no `-live` | **FAIL**, unchanged — the preview wrote nothing | 0 | 0 |
| `-live -limit 32` | 65 still need embedding | 1 | 32 |
| `-live` (resume) | **PASS** — 454/454 | 3 | 65 |
| `-live` again | PASS, `nothing to embed` | **0** | 0 |

That is the issue's three requirements in one table: null-embedding selection, safe
re-runs, and no spend on work already done. The bounded slice also shows the resume
path — the 32 chunks written first were not embedded again by the run that finished
the rest.

Unit coverage is offline except where it needs a database:

- `internal/ingestion/vectors_test.go` — the classification of a stored vector
  (present, foreign model, wrong width, unnormalized, missing hash, hash that no
  longer covers its vector) needs no MongoDB; `SetEmbeddings` end to end, its
  idempotency, its refusal of an unknown id, and its refusal of an unusable vector
  run against `MONGO_TEST_URI` and are skipped without it.
- `cmd/embed_chunks/main_test.go` — pending selection, stable ordering, `-limit`
  truncation, the provider-call count, and that pacing is interruptible.

## Open items

### Nothing is embedded in `tadbor` yet, so nothing is verified there

`tafsir_chunks` is empty in the real database and `ingestion_status` is `not_started`,
unchanged by this issue. Every number above comes from a probe that has been dropped.
Issue #7 stays blocked for the same reason as before: retrieval filters on
`source_verified`, and `ibn-kathir-ar` is still waiting on a human to confirm the text
is Ibn Kathir (docs/reviews/ibn-kathir-ar-surah-12.md).

The moment that source is approved, the order is `ingest -live` then
`embed_chunks -check`. Ingest already embeds what it writes, so `-check` should pass
on the first try — the job exists to establish and defend that property, not to be the
path that first satisfies it.

### Free-tier credit

HuggingFace's free tier is credit-metered ($0.10/month), and this corpus is a few
hundred chunks of a few hundred tokens. The full run above cost 19 provider calls
including the deliberate re-runs. The pacing flags exist so a much larger corpus
degrades into a slower run rather than a 429 spiral; `embedding.Client` retries a
throttled request three times with exponential backoff underneath.

### `.env.example` still names a retired host

`EMBEDDING_API_URL` in `.env.example` is `api-inference.huggingface.co`, which no
longer serves this model; the working value is on `router.huggingface.co` and is what
`.env` and `internal/embedding` are verified against. Corrected in this issue.