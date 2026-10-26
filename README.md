# Tadbor

Quran understanding app — Next.js web, Go backend, React Native mobile,
MongoDB, RAG-grounded explanations with mandatory human review.

See `docs/` for the full architecture:
- `master-outline.md` — documentation structure and decision order
- `rag-architecture-part1.md` / `rag-architecture-part2.md` — the RAG design
- `build-deploy-guide.md` — free-tier build & deploy phases
- `ADDENDUM-mongodb-pivot.md` — **read this first** if cross-referencing the
  above docs against this repo's actual code (MongoDB, not Postgres)

## Run everything with one command

```bash
cp .env.example .env         # fill in LLM_API_KEY / EMBEDDING_API_KEY when you have them
make dev
```

`make dev` brings up MongoDB in Docker, then runs the Go API on `localhost:8080`
and the Next.js reader on `localhost:3000` from source, so both reload on edit.
Neither is containerized: only MongoDB is. Use `make up` if you want just the
database and run the two servers yourself.

In another terminal, load the Quran corpus once:

```bash
make seed
```

Open `http://localhost:3000/surah/12` — you should see all 111 ayahs of Surah
Yusuf. Explanations will be empty until you run the generation + review pipeline
described in `docs/build-deploy-guide.md` Phases 2–6.

`http://localhost:3000/admin` is the reviewer dashboard, where explanations get
approved before anyone can read them. It needs `REVIEWER_PASSWORD` in `.env` and
stays closed without it.

`docs/local-stack-verification.md` is the full walkthrough: what `make dev`
starts, how `make seed` and `make seed-check` differ, and what is still
placeholder.

## Mobile

Not part of `docker-compose.yml` — see `mobile/README.md` for why, and how
to run it against the same backend.

```bash
make mobile
```

## CI

`.github/workflows/ci.yml` runs on every push and pull request to `main`:

- **Backend:** API contract validation, `go build ./...`, `go vet ./...`, and
  `go test ./... -count=1` against a disposable MongoDB 7 service container.
- **Web:** TypeScript typecheck and production build.
- **Mobile:** TypeScript typecheck.

Mongo-backed tests skip locally when `MONGO_TEST_URI` is unset, but fail rather
than skip in CI. To reproduce the backend suite with a throwaway database:

```bash
docker run --rm -d --name tadbor-test-mongo -p 27018:27017 mongo:7
cd backend
CI=true MONGO_TEST_URI='mongodb://localhost:27018/?directConnection=true' \
  go test ./... -count=1
docker stop tadbor-test-mongo
```

Never point `MONGO_TEST_URI` at production or a working database; CI uses an
ephemeral service container that is discarded with the job.

## Repo layout

```
tadbor/
├── backend/        # Go — modular monolith (see backend/README.md)
├── web/             # Next.js reader app
├── mobile/          # React Native / Expo reader app
├── scripts/         # one-off jobs (seeding, future ingestion scripts)
├── docs/            # architecture documentation
├── docker-compose.yml
└── Makefile
```

## What's still placeholder / not wired up

This scaffold gets you a working `docker compose up` with real request paths
(reader → content API → MongoDB, retrieval → generation contract → review
queue), but these still need real implementation before it's a working
product, not just a running skeleton:

- **Quran corpus:** loaded by `backend/cmd/seed_quran` and verified against
  quran.com plus cross-check sources on every `make seed-check` — see
  `docs/quran-corpus-validation.md`. All 111 ayahs of Surah Yusuf are real
  Uthmani text.
- **Explanations:** none exist in the real database. The batch job is built and
  proven, but the pipeline is correctly refusing to generate from an unapproved
  source, so `/surah/12` shows no explanations yet.
- **Tafsir ingestion:** chunking, verse-mapping, and embedding are written
  (`backend/cmd/build_manifest`, `ingest`, `check_coverage`, `embed_chunks`) and
  verified against a throwaway probe database — see
  `docs/tafsir-chunk-mapping.md` and `docs/tafsir-embeddings.md`. The real
  database is still empty because the source itself is unverified: nobody has yet
  confirmed the committed text is Ibn Kathir and not a paraphrase
  (`docs/reviews/ibn-kathir-ar-surah-12.md`).
- **Recitation audio — loaded, one question open.** `make audio` writes all 444
  per-ayah recitations (111 ayahs × Alafasy, Abdul Basit, Al-Ghamdi, Al-Muaiqly)
  from EveryAyah.com, checking every URL before storing it, and `make
  audio-check` fails if any verse is missing, duplicated or still a placeholder.
  The surah page renders 111 playable `<audio>` elements. Unresolved: EveryAyah
  publishes no licence and no per-reciter rights, so whether a public app may
  hotlink these is a judgement call for a human, not something this repo decided
  — see `docs/recitation-audio-sources.md`.
- **Generation pipeline orchestration:** `make generate` loops all 111 ayahs —
  retrieve, simplify, validate, write pending — and is verified end to end
  against a throwaway database; see `docs/batch-generation.md`. It cannot write
  anything into the real database yet, because retrieval only returns chunks
  from a `verified` source and the Ibn Kathir text is still unverified. That is
  the guardrail in §12 working, not a bug.
- **Reviewer auth:** `internal/platform/auth.go` gates everything under
  `/internal` with a shared `REVIEWER_PASSWORD`, and fails closed when that is
  unset. It is a shared secret, not an identity system — no per-reviewer
  identity, expiry, or revocation. See `docs/reviewer-dashboard.md` for when
  that stops being enough.
- **Reviewer dashboard:** `web/app/admin/` lists pending explanations with the
  verse, the resolved evidence, and the citation, and supports Approve /
  Edit & Approve / Reject / Escalate against the §20 checklist — see
  `docs/reviewer-dashboard.md`. Its queue stays empty until the generation batch
  below exists.
