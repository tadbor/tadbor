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
cd backend && go mod tidy && cd ..   # first time only — generates go.sum
make up
```

This starts, in Docker:
- **MongoDB** on `localhost:27017`
- **Go backend** on `localhost:8080`
- **Next.js web app** on `localhost:3000`

Then, in a separate terminal, load the placeholder Quran data once:

```bash
make seed
```

Open `http://localhost:3000/surah/12` — you should see Surah Yusuf's first
two (placeholder) ayahs. Explanations will be empty until you run the
generation + review pipeline described in `docs/build-deploy-guide.md`
Phases 2–6.

## Mobile

Not part of `docker-compose.yml` — see `mobile/README.md` for why, and how
to run it against the same backend.

```bash
make mobile
```

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

- **Quran corpus:** `scripts/seed_quran.go` has 2 placeholder ayahs. Replace
  with your actual verified source (Phase 0/1 of the build guide).
- **Tafsir ingestion:** no ingestion script exists yet — chunking, embedding,
  and verse-mapping (Phase 2–3) still need to be written as a batch job
  against this same MongoDB instance.
- **Recitation audio:** `scripts/seed_recitation.go` loads 4 reciters
  (Alafasy, Abdul Basit, Al-Ghamdi, Al-Muaiqly) with placeholder audio URLs —
  see `docs/ADDENDUM-recitation-audio.md`. Replace with real per-ayah URLs
  from EveryAyah.com or a similar source before this plays real audio.
- **Generation pipeline orchestration:** `internal/generation/service.go` can
  call an LLM, but nothing yet loops over ayahs and writes `explanations`
  documents — that's Phase 5's batch job.
- **Reviewer auth:** `internal/platform/auth.go` is a no-op — required
  before deploying past localhost.
- **Admin dashboard UI:** the `review` API endpoints exist; no frontend for
  them yet (could live in `web/app/admin/` or its own app).
