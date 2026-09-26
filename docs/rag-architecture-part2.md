# Tadbor — RAG Architecture (Part 2: Operations, Infra & ADRs)

*Continues directly from Part 1. This part assumes a hard constraint added by the team: the entire stack must run on free tiers first, without sacrificing the safety/quality guarantees from Part 1, then migrate to paid production infrastructure later without a redesign.*

## 21. Evaluation Framework

**Test-case categories** (a fixed suite re-run whenever a source, prompt, or model changes): straightforward single-ayah verses; multi-ayah range commentary; verses with genuine scholarly disagreement; historical-context-heavy verses; linguistically dense verses (lexical explanation needed); Egyptian-dialect conversion pairs; English translation pairs; questions with no retrievable evidence (must return `INSUFFICIENT_EVIDENCE`); adversarial cases — a document containing an embedded instruction ("ignore previous instructions...") to confirm prompt-injection defense holds (§22).

**Metrics** (directional, not target percentages — targets should be set by the reviewer team once baseline numbers exist): retrieval recall (did the exact-mapped chunk get retrieved), verse-mapping accuracy, citation correctness (does every claim trace to a real chunk), unsupported-claim rate (from the LLM-assisted validator in §19), human-reviewer agreement rate (how often two reviewers would agree on approve/reject — a proxy for how well the automated gate is doing), translation adequacy and dialect fidelity (checklist-scored, not automated similarity — per Part 1 §17's reasoning).

Run this suite manually for MVP (it's one surah); automate the harness once more surahs are added.

## 22. Prompt Injection Defense (detail)

Every LLM call has three structurally separate inputs, and the model is told this in the system prompt: **system instructions** (fixed, never influenced by retrieved content), **the transformation task** (simplify/translate/adapt — also fixed per call type), and **retrieved evidence** (explicitly labeled as untrusted data to transform, never to obey). If an ingested Tafsir document contains something that reads like an instruction, it is treated as text to be summarized/simplified, not executed — this holds even for Tier 1 sources, since a source can be religiously authoritative and still contain a corrupted or tampered file. The adversarial test cases in §21 exist specifically to catch regressions here.

## 23–24. Database Concepts (concrete schema sketch)

```
surahs(id, name, ayah_count, ...)
ayahs(id, surah_id, ayah_number, text_uthmani, text_simple, corpus_version)
sources(id, title, author, language, authority_tier, licensing_status,
        ingestion_status, verification_status, metadata jsonb)
source_versions(id, source_id, edition, version_label, content_hash, created_at)
tafsir_documents(id, source_version_id, raw_text_ref, ingestion_status)
tafsir_chunks(id, document_id, parent_chunk_id, text, embedding vector,
              content_type, page, chapter)
chunk_ayah_mappings(chunk_id, surah_id, ayah_start, ayah_end, mapping_type)
translations(id, base_content_id, language, text, review_status)
explanations(id, surah_id, ayah_start, ayah_end, style /*simplified_ar|egyptian_ar|en*/,
             level /*beginner|tadabbur|advanced*/, text, source_refs jsonb,
             review_status, source_version_snapshot, prompt_version, model_version)
citations(id, explanation_id, chunk_id, display_order)
reviews(id, explanation_id, reviewer_id, decision, comments, created_at,
        source_version_at_review, prompt_version_at_review)
content_versions(id, explanation_id, version_number, text_snapshot, created_at)
generation_runs(id, explanation_id, model, prompt_version, retrieved_chunk_ids jsonb,
                 raw_output jsonb, validation_result jsonb, created_at)
```

Ownership: `explanations` is the only table the public API reads from directly, and only rows with `review_status = 'published'`. Everything upstream (`tafsir_chunks`, `generation_runs`) is internal-only. This mirrors Part 1 §4's "reader never touches retrieval/generation at runtime" decision at the schema level.

## 25. API Boundaries

Conceptual services (implemented as **modules in a modular monolith for MVP**, not separate microservices — splitting into real services only becomes worth the operational cost once a specific module needs independent scaling, which won't happen at one-surah scale):

`QuranService` (read-only, serves ayahs) · `ContentService` (public: serves published explanations/citations) · `RetrievalService` (internal: hybrid search over chunks) · `GenerationService` (internal: calls the LLM with assembled context) · `ReviewService` (internal: reviewer queue, decisions) · `IngestionService` (internal/offline: the pipeline from Part 1 §7).

## 26. Free-Tier Infrastructure (MVP)

Every piece below is chosen so the *architecture* doesn't change when you later swap in a paid tier — only connection strings/credentials change.

| Layer | Free-tier choice | Why it fits | Migration path |
|---|---|---|---|
| Relational DB + vector store | **Supabase free tier** (Postgres + `pgvector` built in) or **Neon free tier** (Postgres, add `pgvector` extension) | One database for everything in §23–24; no separate vector DB needed at this scale (Part 1 §11) | Upgrade the same provider's paid plan, or migrate the same schema to a self-hosted Postgres |
| Embeddings | **Open-source model run for free**: a multilingual sentence-embedding model (e.g., a `sentence-transformers` multilingual model) run via **Hugging Face's free Inference API** or self-hosted in a free-tier compute job | Needs to run once per chunk at ingestion time, not per user request — free-tier rate limits are tolerable for a one-surah corpus | Move to a paid inference endpoint or self-host on a small paid GPU/CPU instance only when ingesting many more surahs |
| Reranker | Open-source cross-encoder (e.g., a small multilingual cross-encoder), run the same way as embeddings — batch job, not live | Same reasoning: offline generation means reranking latency doesn't hit users | Same as embeddings |
| LLM (generation) | A **free tier of a hosted model API** (check current offers — providers' free quotas change; at minimum, a capable free-tier or open-weight model is enough since output is always evidence-constrained and human-reviewed, not depended on for raw creativity) | Because generation is offline and reviewed, a lower-cost/free model is acceptable — quality risk is caught by the review gate, not carried by the model alone | Swap in a stronger paid model later purely by changing the `GenerationService`'s configured provider; the contract (Part 1 §15) doesn't change |
| Backend hosting (API + services) | **Render free web service** or **Fly.io free allowance** | Both support a containerized Node/Python backend with a persistent free tier suitable for low-traffic MVP | Upgrade the same provider's paid tier when traffic grows |
| Admin/review dashboard | Same backend host, or a separate small free instance if you want it isolated from the public API | Keeps reviewer tooling simple for one reviewer | Split out only if reviewer team grows |
| Frontend (reader app) | **Vercel** or **Netlify free tier** for a web reader; if mobile, a free Expo/React Native build pipeline | Static/SSR hosting for the reading UI is well within free-tier limits | Move to paid tier or your own infra when traffic/build-minutes exceed free limits |
| Background jobs (ingestion, embedding, batch generation) | A scheduled free-tier worker (e.g., Render's free background worker, or GitHub Actions scheduled workflow for periodic batch jobs) | Ingestion/generation for one surah is a small, infrequent batch job — doesn't need always-on paid compute | Move to a dedicated queue/worker (e.g., a paid worker dyno) once batch volume grows |
| Object storage (raw source PDFs, OCR artifacts) | Free tier of a storage provider (e.g., Supabase Storage free tier, or a free-tier S3-compatible provider) | Needed for §7's ingestion audit trail | Move to paid storage tier when volume grows |

**Free-tier risk to plan for explicitly:** free tiers often sleep/cold-start inactive services and cap monthly compute minutes/requests — acceptable for a pre-launch MVP with low traffic, but the deployment guide (separate document) treats "detect and handle cold starts" and "stay under monthly quotas" as first-class steps, not afterthoughts.

## 27. Security & Privacy

Reviewer/admin authentication via the hosting platform's built-in auth or a free-tier auth provider (e.g., Supabase Auth) — no custom auth system for MVP. API keys for the LLM/embedding providers stored as environment secrets, never in the repo. Rate limiting on the public read API to prevent abuse of a free-tier backend (a simple per-IP limit is sufficient at MVP scale). Uploaded/ingested source documents are scanned for basic integrity (checksum) before entering the pipeline, given §22's concern about tampered files. Minimal user data collection for MVP (Part 1 doesn't require accounts to read) keeps privacy scope small; revisit if personalization is added later.

## 28. Observability

At minimum for MVP: log every `generation_run` (Part 1's audit fields) to the same Postgres instance rather than a separate observability stack — a dedicated logging/observability service is unnecessary complexity at this scale. A simple dashboard query ("show me all pending reviews," "show me all `INSUFFICIENT_EVIDENCE` cases") is enough; add a real observability tool only once traffic or team size justifies it.

## 29–30. Cost & MVP Architecture Decision

Because generation happens offline and is reviewed once per verse/style combination (not per reader), the entire MVP (one surah × 3 styles) requires generating roughly 111 ayahs × 3 styles = ~333 explanation drafts *total*, ever — not per user. This is the single fact that makes the free-tier stack viable: free-tier rate limits on embeddings/LLM calls are a non-issue against a few hundred one-time generation calls, even though they'd be completely inadequate for live per-request generation at any real traffic volume.

**Recommended MVP stack, summarized:** Postgres+pgvector (Supabase/Neon free) → open-source embeddings + reranker run as batch jobs (HF free inference or free compute) → free/low-cost LLM for generation, called only during the offline pipeline → modular-monolith backend on Render/Fly free tier → Vercel/Netlify frontend → all review and audit data in the same Postgres instance.

## 31. Scaling Strategy

Scaling to 114 surahs, more languages, more sources is additive under this design specifically because nothing above assumes "one surah": the schema (§23–24), retrieval logic (Part 1 §11), and review workflow (Part 1 §20) are already keyed by `surah_id`/`language`/`style` as first-class fields, not hardcoded. What *does* need attention at scale: (1) free-tier compute minutes/API quotas will be exceeded — migrate embeddings/reranking/LLM calls to paid tiers of the *same* providers first, before considering a different architecture; (2) a dedicated vector database becomes worth its complexity once chunk count reaches the high tens-of-thousands to low-hundreds-of-thousands with a need for fast live semantic search (still not needed if generation stays offline); (3) reviewer capacity becomes the real bottleneck before infrastructure does — plan for more reviewers before more compute.

## 32. Architecture Decision Records (condensed)

- **Why RAG over fine-tuning:** grounding must be verse-specific and swappable per source without retraining; RAG keeps the Quran/Tafsir corpus and the LLM decoupled, which is required for the source-versioning/invalidation rule in Part 1 §20.
- **Why curated sources over open ingestion:** unverified sources make the human-review gate meaningless — trust has to start at ingestion, not just at publish.
- **Why lexical/exact retrieval leads over pure vector search:** verse identification must be exact; semantic similarity is the wrong tool for "which verse is this," only the right tool for "what else discusses this theme."
- **Why offline/pre-generated over live generation:** it's what makes the review gate enforceable at all, and it's what makes a free-tier LLM/embedding budget sufficient.
- **Why Postgres+pgvector over a dedicated vector DB:** corpus size doesn't warrant it yet, and one database simplifies the free-tier stack and the schema's referential integrity.
- **Why a modular monolith over microservices:** one small team, one surah, low traffic — service boundaries add operational cost with no corresponding benefit yet; the module boundaries in §25 keep a future split cheap if it's ever needed.

## 33. Risks & Mitigations

- **Free-tier cold starts/quota limits causing ingestion or review-dashboard downtime** → batch jobs scheduled during low-usage windows; monitor quota usage manually at MVP scale.
- **A single reviewer becoming a bottleneck or a single point of failure** → document the review checklist thoroughly enough that a second reviewer can be onboarded quickly.
- **Source licensing issues discovered after ingestion** → licensing_status gate (§6/Part 1) blocks use in generation until cleared, so a licensing problem never reaches published content, only wastes ingestion effort.
- **Free LLM/embedding provider deprecating its free tier** → the provider-swap path in §26's table exists precisely so this is a config change, not a redesign.

## 34. Open Questions (product/religious, not technical)

Which specific Mushaf digital source to adopt; which Tafsir(s) qualify as Tier 1 for Surah Yusuf; who the human reviewer(s) are and their qualification; whether a second reviewer is required before any content is ever published, even at MVP.

## 35. Recommended Implementation Order

1. Resolve the open questions in §34 — nothing below should start before the Quran source and at least one Tier-1 Tafsir source are locked.
2. Stand up Postgres (Supabase/Neon free) and implement the schema (§23–24).
3. Load the Quran corpus (Part 1 §5) and verify its integrity hash.
4. Ingest one approved Tafsir source through the pipeline (Part 1 §7) for Surah Yusuf only, including verse mapping (§8) and chunking (§9).
5. Stand up retrieval (§11) and confirm exact-verse retrieval works correctly for all 111 ayahs before touching generation at all.
6. Implement the generation contract (Part 1 §15) and run it for one explanation style first (e.g., Simplified Arabic) end-to-end through automated validation (§19) and manual review (§20).
7. Once one style is proven, add Egyptian Arabic and English using the same pipeline (Part 1 §16–17).
8. Build the minimal reader app reading only from `explanations` where `review_status = 'published'`.
9. Run the evaluation suite (§21) against the full MVP before any real users see it.
10. Deploy reader app + API on free-tier hosting (§26); only then plan the production migration.
