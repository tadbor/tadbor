# Tadbor — Build & Deployment Guide (Free Stack → Production)

This is the execution checklist for the architecture in Parts 1–2. It's sequenced so you never build something before its prerequisite is validated — each phase ends with a concrete "done when" check.

**A note on "free":** free tiers change their limits often. Before committing to any specific provider below, check its current free-tier quota (compute minutes, request limits, sleep/cold-start behavior) — the architecture is designed so swapping providers later is a config change, not a rebuild, precisely because free-tier terms shift.

---

## Phase 0 — Lock the non-technical decisions
*(Nothing in Phase 1+ should start before this is done — see Part 2 §34.)*

- Choose the one canonical Quran text source you'll treat as immutable.
- Choose at least one Tier-1 Tafsir source for Surah Yusuf, and confirm you can legally use/digitize it (public domain, or licensed).
- Identify who the human reviewer is (even if it's just you for MVP) and write down the review checklist from Part 1 §20 in plain language they'll actually use.
- **Done when:** you have a source file/text for the Quran (Surah Yusuf) and a source file/text for your chosen Tafsir, and you know who signs off on content.

## Phase 1 — Foundational data store (free)

1. Create a free **Supabase** project (Postgres + `pgvector` pre-enabled) — or **Neon** free tier + manually `CREATE EXTENSION vector`.
2. Create the schema from Part 2 §23–24 (`surahs`, `ayahs`, `sources`, `source_versions`, `tafsir_documents`, `tafsir_chunks`, `chunk_ayah_mappings`, `translations`, `explanations`, `citations`, `reviews`, `content_versions`, `generation_runs`).
3. Load Surah Yusuf's 111 ayahs into `ayahs` from your chosen source, plus a `content_hash` per ayah and a corpus-level version tag.
4. Write a small integrity-check script that re-hashes the loaded text and confirms it matches — run this any time you touch the table.
- **Done when:** you can query all 111 ayahs of Surah Yusuf back out exactly as sourced, and the integrity check passes.

## Phase 2 — Ingest the Tafsir source

1. Get your chosen Tafsir's commentary on Surah Yusuf into digital text (if it's already digital text, skip OCR; if it's scanned, use a free OCR tool — e.g., Tesseract, run locally or in a free CI job — and manually spot-check the output against the source before trusting it).
2. Insert a `sources` row (Tier 1, your Tafsir), a `source_versions` row (the specific edition/printing), and a `tafsir_documents` row referencing the raw text.
3. Split the commentary into chunks following Part 1 §9 (verse-anchored, not fixed-token) and insert into `tafsir_chunks`.
4. For each chunk, create its `chunk_ayah_mappings` row(s) — this is manual, careful work at this scale (one surah): read each chunk, record which ayah(s) it actually discusses.
- **Done when:** every one of the 111 ayahs has at least one mapped Tafsir chunk (or you've explicitly noted which ayahs currently lack coverage).

## Phase 3 — Embeddings (free, batch)

1. Pick a free multilingual sentence-embedding model (something that handles Arabic well) — run it via the **Hugging Face free Inference API**, or run it yourself in a one-off free compute job (e.g., a free-tier GitHub Actions run, or Google Colab's free tier, since this is a one-time batch job, not a live service).
2. Generate an embedding for every chunk from Phase 2 and store it in `tafsir_chunks.embedding` (`pgvector` column).
3. Re-run this only when chunks change — it's a batch job, not a running service.
- **Done when:** every chunk has a non-null embedding vector.

## Phase 4 — Retrieval service

1. Build the retrieval logic from Part 1 §11 as a module in your backend: given `(surah, ayah)`, first query `chunk_ayah_mappings` for exact matches (this alone should answer most requests for a one-surah, well-mapped corpus); only fall back to vector similarity search over `tafsir_chunks.embedding` for thematic/no-exact-mapping cases.
2. Add the guardrails from Part 1 §12: exclude any source not marked verified/licensed; return an explicit `INSUFFICIENT_EVIDENCE` state when nothing qualifies.
3. Test this against all 111 ayahs manually — for each, confirm the retrieved evidence is actually about that verse.
- **Done when:** querying any of the 111 ayahs returns the correct, source-approved evidence (or a correct `INSUFFICIENT_EVIDENCE`).

## Phase 5 — Generation (offline, one style at a time)

1. Pick your MVP-stage LLM: a free-tier hosted model, or a capable open-weight model run via a free inference endpoint. Since generation is offline/reviewed (not live per-user), a lower-cost model is acceptable — the review gate is your quality backstop, not the model alone.
2. Implement the generation contract from Part 1 §15 (structured JSON output: content, source_refs, claims, warnings, confidence, requires_review=true) as a `GenerationService` function that takes assembled context (Part 1 §13) and a transformation instruction.
3. Run it for **Simplified Arabic only, first** — generate one explanation per ayah, storing each as an `explanations` row with `review_status = 'pending'` and full `generation_runs` audit data.
4. Run automated validation (Part 1 §19) on every generated row before it's eligible for human review.
- **Done when:** all 111 ayahs have a pending Simplified-Arabic explanation that's passed automated validation.

## Phase 6 — Human review

1. Build the minimal reviewer view (can be a simple internal page or even a spreadsheet-backed script for true MVP-of-MVP): show the ayah, the evidence chunks, the generated explanation, and Approve / Edit & Approve / Reject actions.
2. Apply your Phase 0 checklist to every explanation. Store the `reviews` row and flip `review_status` to `published` on approval.
3. Only after all 111 Simplified-Arabic explanations are reviewed, repeat Phase 5–6 for Egyptian Arabic, then English — reusing the same pipeline (Part 1 §16–17).
- **Done when:** all 111 ayahs have a published explanation in all 3 MVP styles.

## Phase 7 — Reader app

1. Build a minimal frontend (a simple web app is the fastest free path — a mobile-specific app can come later) that reads **only** from `explanations` where `review_status = 'published'`, joined with the Quran text from `ayahs`.
2. Implement the UX split from Part 1 §4/Part 1's Track D concept: Quran text visually separated from the explanation block, with a citation shown per explanation, and a style/dialect switcher.
3. Deploy the frontend to **Vercel** or **Netlify** free tier.
- **Done when:** you can open the deployed URL and read Surah Yusuf end-to-end in all 3 styles, with visible citations.

## Phase 8 — Backend deployment

1. Deploy your backend (the modular monolith containing `ContentService`, `QuranService`, and the internal services) to **Render** or **Fly.io** free tier.
2. Point it at your Supabase/Neon database via environment secrets (never hardcoded credentials).
3. Set up a scheduled free-tier job (e.g., a scheduled GitHub Actions workflow) for any recurring batch tasks (re-embedding if sources change, periodic integrity checks).
- **Done when:** the deployed frontend is reading live from the deployed backend/database, not a local dev setup.

## Phase 9 — Evaluate before real users

1. Run the evaluation suite from Part 2 §21 against your deployed MVP: verse accuracy, citation correctness, translation/dialect fidelity, and at least one adversarial prompt-injection test (Part 2 §22).
2. Fix anything that fails before inviting real users.
- **Done when:** the suite passes and your reviewer is confident in the published content.

## Phase 10 — Production migration (when ready to leave free tiers)

This is additive, not a rebuild, because of the choices made above:

1. **Database:** upgrade your Supabase/Neon project to its paid tier (or migrate the same schema to a self-managed Postgres) — same schema, same queries.
2. **LLM/embeddings/reranker:** swap the provider config in `GenerationService`/ingestion scripts to paid endpoints of the same or a stronger provider — the generation contract (Part 1 §15) doesn't change.
3. **Backend/frontend hosting:** upgrade Render/Fly and Vercel/Netlify to paid tiers for guaranteed uptime and no cold starts, or move to your own infrastructure if you have specific needs (custom domains, compliance, etc.).
4. **Add real observability** (Part 2 §28) once traffic justifies a dedicated tool, rather than the Postgres-log-query approach used at MVP.
5. **Re-run the evaluation suite** (Phase 9) after every infrastructure change before re-exposing it to users.
6. **Only then** begin Part 2 §31's scaling work — adding more surahs, languages, and reviewers — since scaling before production-grade infra is solid just moves the same free-tier limits to a bigger audience.

---

### Quick reference: what's free vs. what changes at scale

| Component | MVP (free) | First thing to upgrade at scale |
|---|---|---|
| Database | Supabase/Neon free | Same provider's paid plan |
| Embeddings/reranker | HF free inference or free batch compute | Paid inference tier of the same provider |
| LLM generation | Free/low-cost tier, used offline | Stronger paid model, still offline |
| Backend hosting | Render/Fly free | Same provider's paid plan (removes cold starts) |
| Frontend hosting | Vercel/Netlify free | Same provider's paid plan |
| Vector search | pgvector in the same Postgres | Dedicated vector DB — only once chunk count is very large *and* you need live semantic search |
