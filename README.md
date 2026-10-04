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
- **Tafsir ingestion:** chunking, verse-mapping, and embedding are written
  (`backend/cmd/build_manifest`, `ingest`, `check_coverage`, `embed_chunks`) and
  verified against a throwaway probe database — see
  `docs/tafsir-chunk-mapping.md` and `docs/tafsir-embeddings.md`. The real
  database is still empty because the source itself is unverified: nobody has yet
  confirmed the committed text is Ibn Kathir and not a paraphrase
  (`docs/reviews/ibn-kathir-ar-surah-12.md`).
- **Recitation audio:** `scripts/seed_recitation.go` loads 4 reciters
  (Alafasy, Abdul Basit, Al-Ghamdi, Al-Muaiqly) but only ayahs 1–2, with
  placeholder audio URLs — see `docs/ADDENDUM-recitation-audio.md`. Replace with
  real per-ayah URLs from EveryAyah.com or a similar source before this plays
  real audio (issue #14).
- **Generation pipeline orchestration:** `internal/generation/service.go` can
  call an LLM, but nothing yet loops over ayahs and writes `explanations`
  documents — that's Phase 5's batch job.
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
