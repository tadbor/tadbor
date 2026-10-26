# Tadbor Project Status Audit

**Audit date:** 2026-10-25  
**Repository:** `tadbor/tadbor`  
**Audit scope:** Read-only status audit. No application code, tests, configuration, dependencies, or GitHub issues were modified. This report and the synchronized [project status and plan](PROJECT_STATUS_AND_PLAN.md) are the documentation outputs; pre-existing local changes were preserved.

## 1. Project Overview

Tadbor is intended to be a Quran reading and understanding application: immutable Uthmani Quran text, source-grounded explanations, citations, and human review before publication. The checkout is a multi-app modular-monolith repository:

- **Web:** Next.js 14, React and TypeScript. Server-rendered reader/admin routes.
- **Backend:** Go 1.22, Gin and MongoDB Go driver. MongoDB, not the Postgres/pgvector described in older architecture documents, is the actual storage implementation.
- **Mobile:** Expo / React Native with `expo-audio`.
- **Data and batch tools:** committed Quran/tafsir corpus material plus Go commands for seeding, registration, ingestion, embedding, validation, explanation generation, and introduction generation.
- **Local infrastructure:** Docker Compose runs MongoDB only; Make targets run Go and Next.js from source. No deployment manifest or active hosted deployment was verified.

**Current stage: PARTIAL / pre-release development.** There is meaningful API, reader, ingestion, generation, and review code, but the repository alone does not establish the live database contents or user-visible behavior. The mobile typecheck currently fails. Audio rights remain unresolved. Introduction approval is intentionally disabled pending trusted reviewer identity. Issue closures are not reliable evidence of deployed data or completion where closure comments conflict with acceptance criteria.

## 2. Executive Summary

### Genuinely present

- Public API handlers for Quran ayahs, explanations, reciters/recitations, and introductions.
- A web Surah reader that requests real backend data and renders Uthmani text, published explanation text, and direct-stream audio controls when API data exists.
- A mobile screen that requests similar data and contains audio playback code, but only launches Surah 12 and has nonfunctional navigation/preferences UI elements.
- A substantial offline source registry, chunking, embedding, retrieval, generation, and review implementation with tests.
- A Surah 12 introduction pipeline scaffold and typed generation workflow in local uncommitted changes. The generation CLI requires a database and has an explicit `-live` opt-in; live generation was not run.
- The public introduction API fails closed on source attribution issues and hides internal model/review fields. Its Mongo integration tests exist but were skipped because `MONGO_TEST_URI` is not configured.

### Not established as usable today

- A surah library/discovery endpoint: the API registers `GET /surahs/:surahId/ayahs`, but no `GET /surahs` route was found. Web home is a static landing/demo page.
- Current Mongo contents, published explanations, introductions, or recitations: no Mongo URI was configured or queried in this audit.
- Fully reviewed/approved introduction publication: the internal introduction decision endpoint returns `503` until trusted reviewer identity and authorization are implemented. The dashboard displays drafts but no approval controls.
- Qualified reviewer identity: current shared password is a shared secret, while the typed reviewer label is caller-controlled display text.
- Mobile build health: `npx tsc --noEmit` fails with a `StatusBar` prop type error.
- Public deployment, rights clearance for EveryAyah audio, offline operation, or end-to-end UI playback in this audit.

**Important evidence conflict:** `corpus/sources/ibn-kathir-ar.json` says `verification_status: verified` and names a verification note, while `README.md` says the source is unverified and the review sheet calls the reviewer `automated-pilot`, explains that fidelity against a printed edition is not established, and still lists 19 entries as needing human review. Issue #30 is CLOSED with a comment claiming human verification. The artifacts do not agree; a qualified content owner must reconcile them before treating the source as approved. No live DB status was checked.

## 3. Product Capability Matrix

| Feature | Implementation | Automated tests | Real-data/service verification | UI verification | Actual capability / remaining work |
|---|---|---|---|---|---|
| Surah discovery/selection | PARTIAL: backend has per-ID ayah route; no list route registered. Mobile hardcodes ID 12. | Backend route/service tests exist; no library route tests. | UNVERIFIED: no DB connection. | UNVERIFIED in browser/device during this audit. Web home is static, not a functional library. | Add metadata-backed `GET /surahs` and a real selector with availability states. |
| Quran ayah display | IMPLEMENTED in code for any ID with data; committed corpus is Surah 12. | Quran and seed/check command tests exist. | Historical docs report all 111 verses checked; not rerun against a DB here. | Web and mobile contain render components; not opened against live API here. | Other 113 surahs are not established in seeded content. Quran route does not explicitly sort query results. |
| Introduction and historical context | PARTIAL: public API/model and local generation pipeline exist; no verified introduction content evidence was proven in DB. | Fake-backed API/pipeline tests; Mongo tests are skipped. | UNVERIFIED: no DB. Source policy requires registered, verified sources and attribution; classification-specific source evidence is absent from the committed tafsir manifest as inspected. | Web admin can render a pending introduction, but it is not a public reader UI. No public introduction UI was verified. | Approval endpoint is deliberately disabled. Need trusted reviewer identity, adequate evidence, semantic human review, and real DB integration. |
| Per-ayah explanations | IMPLEMENTED in code, published-only API and rendering. | Extensive Go unit tests. | UNVERIFIED current data. Historical issue #39 closure claims all styles published but says review records were absent; current database unavailable. | Web requests only `simplified_ar`; mobile selector offers style values that do not match backend (`egyptian`, `english` vs `egyptian_ar`, `en`). | Do not assert readers currently have explanations. Confirm database content and audit records. |
| Explanation style/language selection | PARTIAL: backend takes exact style and generation validates scripts; no style registry/availability API found. | Generation validation tests. | No style data in DB verified. | Web hardcodes `simplified_ar`; mobile exposes mismatched IDs. | No cross-style fallback in current content query. Add registry, availability and consistent style IDs. |
| Tafsir navigation/citations | PARTIAL: internal review queue resolves cited chunks; public explanation response includes chunk refs but web reader does not resolve/display a citation. | Retrieval/review tests exist. | No Mongo chunks inspected. | Admin shows review evidence; reader citation display not present in inspected Surah page. | Add reader citations/navigation and verify user-facing source attribution. |
| Web | IMPLEMENTED as code: `/surah/[id]`, API client, `/admin`, login/decision handlers. | `npm run build` passed in this audit. No frontend behavior test suite found. | UNVERIFIED: API/database not run. | Build-only; no browser runtime check performed. API client turns failed/unparseable list fetches into `[]`, which can look like empty content. | Loading/error/empty states and real backend behavior need end-to-end verification. |
| Mobile | PARTIAL: Expo app, Surah 12 screen, API client and `expo-audio` player code. | CI typecheck uses `|| true`; no mobile tests found. Direct typecheck failed in this audit. | UNVERIFIED: no device/backend session. | UNVERIFIED on device. | `App.tsx` `StatusBar` has unsupported `backgroundColor`; tabs/back/bookmark are presentation-only; style IDs mismatch API; “Playing…” button restarts instead of pausing; API has no status/schema validation. |
| Audio playback | PARTIAL: API returns URLs; web uses native `<audio>`, mobile uses `expo-audio`. | Backend ingestion/recitation tests exist. | Historical docs claim 444 URLs checked; no current URL or database check performed. | Not tested in this audit. Issue #14 says mobile spot-check was unverified. | Direct stream only; no bytes hosted locally and no download/offline feature. EveryAyah rights remain unresolved. |
| Audio downloads/offline | NOT IMPLEMENTED in inspected clients/API. | None found. | Not applicable. | No download control found. | ADR 0002 explicitly says never auto-download all Quran; offline per-surah download requires separate scope and rights clearance. |
| Backend APIs | PARTIAL: Gin routes/services and OpenAPI exist. Actual public paths are limited to health, ayahs by surah ID, explanations, introductions, reciters, recitations. | Go suite passes. | No live API or database connected during audit. | No runtime requests observed. | No public list/readiness/style registry endpoint. OpenAPI is incomplete/stale for several actual routes. |
| MongoDB/data | IMPLEMENTED storage code and Docker Compose Mongo service; no migration runner found. Collections/indexes are created by service/commands as needed. | DB-backed tests exist and skip without `MONGO_TEST_URI`. | BLOCKED/UNVERIFIED: `MONGO_TEST_URI` and `MONGO_URI` are unset; database not queried. | Not applicable. | Current data, indexes, and counts cannot be asserted. Seed/ingestion history in docs is not a live DB check. |
| AI generation/retrieval | IMPLEMENTED interfaces and batch workflow; Gemini provider and optional embedding provider. Exact-mapped evidence preferred; thematic fallback is brute-force cosine. | Unit tests pass; opt-in live provider tests are skipped unless explicitly enabled. | UNVERIFIED live credentials/provider/database. No generation was run. | No generated content UI checked. | Structural claim/ref validation does not prove semantic entailment. Introduction pipeline supplies all source-verified chunks for Surah 12, potentially a large context, and has no measured production run. |
| Human review/publication | Explanation review flow is exposed. Introduction queue is added locally; registered intro decision HTTP endpoint returns 503. | Fake auth/handler tests and Go review tests pass. Mongo transition tests skip. | No DB workflow was run. | Admin build passes; introduction card is read-only and says decisions are disabled. | Shared-secret login is not per-person identity or qualification. Introduction content cannot be published through registered API route until identity provider and role mapping are configured. |
| Error handling | PARTIAL: public intro 400/202/404/500 shapes; generic public 500 message. Web list API degrades errors to empty arrays. | Handler tests cover some response states. | No external failure injection beyond unit fakes. | Not runtime tested. | Other public API handlers commonly echo `err.Error()`; evaluate error sanitization more broadly. |
| Deployment/release | PARTIAL: Docker Compose for Mongo, Make dev, backend/web Dockerfiles and CI workflow. | CI config runs Go tests/web build. | No cloud deployment verified; issue #15 remains OPEN. | Local UI not started in this audit. | Backend/web not services in compose. No deployment workflow/production topology verified. |

## 4. Frontend Status

### Web

**Status: PARTIAL.** `web/app/surah/[id]/page.tsx` fetches ayahs, `simplified_ar` explanations, reciters, and recitations in parallel and renders Uthmani text, optional audio, and explanation blocks. Reciter switching uses a query link. There is no demonstrated library screen; the main `/` page is a static landing/demo. There is no public introduction fetch/render path in the inspected page. The reader does not resolve explanation `source_refs` into citations.

`web/app/api-client/index.ts` parses JSON and narrows list rows, but does not reject non-2xx responses; failed or malformed responses become empty arrays, potentially presenting an API outage as empty content. `npm run build` passed in this audit. No browser interaction or real API session was performed.

Admin work is server-rendered. It lists explanation and introduction queues. Because shared-password/free-text name login does not establish qualified identity, introduction decisions have been intentionally disabled in current local changes; the internal route returns `503`, and the UI states why. Existing explanation approval still uses the weaker shared-secret architecture.

### Mobile

**Status: PARTIAL / BUILD BLOCKED.** `mobile/App.tsx` mounts only `SurahScreen` for Surah 12. The screen fetches data and offers style buttons, reciter selection/playback code, and visible tabs/bookmark/back controls. Several controls are decorative, style IDs do not match backend IDs, playback “pause” text calls a function that pauses/releases then starts playback again, and response parsing lacks status/type validation. These findings come from source inspection; no device test was performed.

Direct check `cd mobile && npx tsc --noEmit` failed: `mobile/App.tsx:9` passes `backgroundColor` to the installed React Native `StatusBar` type, which does not accept that prop. `.github/workflows/ci.yml` masks mobile typecheck failure with `|| true`, so this cannot fail CI.

## 5. Backend and Data Status

- `backend/cmd/api/main.go` wires Mongo-backed Quran/content/recitation routes and mounts retrieval/review under `/internal` with `ReviewerAuthMiddleware`.
- Public routes verified by registration: `GET /health`, `/surahs/:surahId/ayahs`, `/surahs/:surahId/explanations`, `/surahs/:surahId/introduction`, `/reciters`, `/surahs/:surahId/recitations`. No public `GET /surahs` library route was found.
- `content.Service.GetExplanations` filters `review_status: published` and exact style. Introduction lookup filters `surah_id`, exact style, and published state; it distinguishes pending/processing from unavailable and refuses missing/stale/unusable attribution. Internal JSON fields use `json:"-"`.
- Introduction Mongo integration tests cover published/exact style, processing/unavailable, source downgrade, stale source-version hash, rights downgrade, missing chunk reference. They were not executed against Mongo here.
- Mongo connection defaults database name to `tadbor`; services use direct collections. No centralized migration runner was found; index creation occurs in command/service code.
- Committed Qur’anic data is scoped to Surah 12 in the source/manifests inspected. `GET /surahs/:id/ayahs` queries by ID without explicit sort. No live DB count or integrity query was run.
- The repository contains a verification and coverage toolchain. `MONGO_URI` and `MONGO_TEST_URI` are both unset in this audit environment.

## 6. AI and Content Pipeline

**Explanation pipeline: PARTIAL/IMPLEMENTED in code.** Retrieval checks `source_verified`, tries exact verse mapping, then thematic vector candidates if embedding is available. Generation uses Gemini HTTP structured output, fixed task briefs, reference resolution, retry handling for 429/503, script/length/citation checks, and `requires_review` forced true. `cmd/generate_explanations` persists pending outputs and audit rows, and does source/ref/mapping/Quran-text checks. These are code and unit-test facts, not verification that production generation currently succeeds.

**Introduction pipeline: PARTIAL local implementation.** `cmd/generate_introduction` defaults to preview and accepts explicit `-live`; the pipeline retrieves source-verified Surah 12 chunks through `retrieval.GetVerifiedSurahEvidence`, rechecks current source status and pinned hash, requests typed section/claim output, resolves references, checks structure/ref existence/script, and stores `review_status: pending`. Unit tests use fakes. Mongo draft and review tests are present but skipped here. There is no proof the current database contains suitable verified introduction evidence or a dedicated surah-level introduction source.

**Semantic support limitation:** checking that a citation resolves to an approved chunk does not show that the chunk entails the attached claim. No deterministic semantic entailment validator exists. Human review is required; the introduction approval route is currently disabled.

**Provenance discrepancy:** the source JSON says verified and cites human review; the review sheet records an “automated-pilot” reviewer, says quran.com digitization is not tied to a specific printed edition, and describes 19 entries needing a human decision. Issue #30 is CLOSED with a comment claiming human verification. Resolve this discrepancy with the qualified content owner before trusting the source’s verification flag.

## 7. Review and Publication Status

- Public explanation/introduction paths filter to `review_status: published`; introduction JSON hides review status, source snapshot, prompt/model versions, and timestamps.
- Explanation review service can approve/edit/reject/escalate and records audit rows. Its checklist enforcement is partly dashboard-side; direct API calls authenticated only by the shared password can bypass the UI checklist.
- Existing dashboard authentication stores the shared `REVIEWER_PASSWORD` itself in an HttpOnly cookie and forwards it to the API. Login also stores a free-text name. There is no verified user identity, qualification, role mapping, expiry, or individual revocation.
- To avoid misrepresenting that name as identity, current local changes make introduction decision endpoint return `503`; dashboard presents no approval controls. Queue remains available for review preparation.
- Direct `review.Service.DecideIntroduction` still contains a Mongo-backed approval implementation; it is not reachable through the registered introduction decision HTTP handler in current code. Any future enablement must add verified identity/authorization, pass that trusted identity to audit, and make API checklist enforcement authoritative.
- Source status/hash is revalidated at approval in that service, but the database behavior was not executed here. Public API source attribution performs current registry and pinned-version checks.
- Content publication from Mongo/manual operations cannot be ruled out absent production database access. No such operations were performed in this audit.

## 8. GitHub Issue Snapshot

**GitHub access was available.** Repository-wide issue search returned 30 issues (#1–30); exact-title searches and direct issue fetches covered #31–43. Current issue states are from GitHub as observed 2026-10-25. Acceptance criteria below are condensed from issue bodies; closure status is not proof that the current checkout, DB, or external UI satisfies them.

| # | State | Title | Acceptance criteria (condensed) | Audit view |
|---:|---|---|---|---|
| 1 | CLOSED | Day 1: Set up free-tier infrastructure accounts | Configure credentials/accounts and health endpoint. | PARTIAL: local `.env` exists but was not read; no current health request. |
| 2 | CLOSED | Day 1: Load verified Quran corpus for Surah Yusuf | 111 ayahs, hash integrity, independent source checks. | PARTIAL: tools/docs/tests present; no DB validation this audit. |
| 3 | CLOSED | Day 1: Populate Source Registry with one Tier-1 Tafsir source | Source/version/raw text registered; initially unverified. | PARTIAL: artifacts exist; verification conflict remains. |
| 4 | CLOSED | Day 1: Verify docker-compose local stack end-to-end | Fresh local stack, seed, health, reader works. | PARTIAL: historical verification doc; not rerun. |
| 5 | CLOSED | Day 2: Verse-level chunking and mapping for Tafsir | Correct chunk/mapping coverage and tests. | PARTIAL: code/tests exist; no live DB check. |
| 6 | CLOSED | Day 2: Batch-generate embeddings via Hugging Face | Every chunk has vector. | UNVERIFIED: DB/vector state not queried. |
| 7 | CLOSED | Day 2: Validate retrieval against all 111 ayahs | Per-ayah correct source-approved retrieval/gaps. | UNVERIFIED current DB; historical comments report validation. |
| 8 | CLOSED | Day 3: Wire up free LLM provider | Successful inspected live generation call, grounded output. | PARTIAL: Gemini integration and opt-in smoke test; not run now. |
| 9 | CLOSED | Day 3: Run automated validation | Tests reject bad outputs; validator wired to batch flow. | IMPLEMENTED in code/tests; does not establish semantic correctness. |
| 10 | CLOSED | Day 3: Batch-generate Simplified Arabic explanations | Pending outputs for 111/gaps plus audit runs. | CLOSED comment claims generated, but #39 later notes review records absent; current DB unknown. |
| 11 | CLOSED | Day 4: Build minimal reviewer dashboard | Authenticated queue, checklist, decisions, published status. | PARTIAL: explanation workflow exists; shared password is not identity and checklist API gate is weak. |
| 12 | CLOSED | Day 4: Review and publish all Simplified Arabic explanations | Reader displays reviewed explanations for covered ayahs. | UNVERIFIED current DB; do not infer from closure. |
| 13 | CLOSED | Day 4: Generate and review Egyptian Arabic and English styles | All styles reviewed/published, switcher works. | PARTIAL: backend styles exist; web hardcodes one style, mobile style keys mismatch. |
| 14 | CLOSED | Day 5: Load real recitation audio for Surah Yusuf | 444 entries, rights confirmed, playback web/mobile. | PARTIAL: code/doc report URL checks, but license unresolved and mobile playback unverified. |
| 15 | OPEN | Day 5: Deploy the full stack to free-tier hosting | Public URL, all styles/audio, no localhost. | NOT IMPLEMENTED/UNVERIFIED: no deployment verified. |
| 16 | OPEN | Day 5: Final QA pass before calling MVP done | Grounding, adversarial, slow/offline tests and QA note. | NOT COMPLETE: this audit did not run UI/offline/adversarial external test. |
| 17 | OPEN | Epic A - Content foundation | Verified sources, introductions, scale-out. | PARTIAL; source review/data state unresolved. |
| 18 | OPEN | Epic B - Backend and content APIs | Library, introduction, style, audio readiness APIs. | PARTIAL; library/style availability APIs absent. |
| 19 | OPEN | Epic C - Web reading experience | Library, introduction, style switch, audio/error states. | PARTIAL; actual reader has no library/introduction and limited states. |
| 20 | OPEN | Epic D - Mobile reading experience | Mobile parity: library, intro, ayahs, preferences, audio. | PARTIAL; only hardcoded Surah 12 screen. |
| 21 | OPEN | Epic E - Local preferences and privacy | Device-only language/dialect preferences with manual override. | NOT IMPLEMENTED beyond transient style state; no persistent preference evidence. |
| 22 | OPEN | Epic F - Audio and offline experience | Rights, readiness, ingestion scale, offline strategy. | PARTIAL; direct stream only; rights unresolved, no offline/download. |
| 23 | OPEN | Epic G - Quality and release readiness | CI, safeguard regressions, E2E, accessibility, release gate. | PARTIAL; CI exists, mobile failures suppressed, DB/E2E unavailable. |
| 24 | CLOSED | Decide audio delivery strategy | Per-platform strategy, no auto-download, failure/licensing rules. | IMPLEMENTED as ADR; licensing decision still unresolved by its own notes. |
| 25 | CLOSED | Decide translation-versus-explanation policy and style registry | Style kinds/labels/qualifications/extensibility. | Policy/ADR exists; runtime data-driven registry endpoint absent. |
| 26 | CLOSED | Define source/review policy for introductions and Asbab | Sources, disputed reports, checklist, qualified approver. | Policy exists but reviewer owner is “to be assigned”; Asbab evidence not established. |
| 27 | CLOSED | Establish CI and verified baseline | CI tests/build/lint/mobile check. | PARTIAL: CI backend/web; mobile command has `|| true`; actual typecheck fails. |
| 28 | CLOSED | Regression tests for public exposure | All public routes covered and internal metadata hidden. | PARTIAL: content tests exist; no router enumeration test; Mongo tests skipped. |
| 29 | CLOSED | Shared content-readiness/API contract | Availability states, OpenAPI, fixtures, contract tests. | PARTIAL: intro has processing/unavailable; full readiness endpoints/spec validation absent. |
| 30 | CLOSED | Human verification of Ibn Kathir Surah 12 | Resolve 19 entries, edition, reviewer/date, then verify. | CONFLICTED: closed comment says done; review artifact still calls reviewer automated-pilot and asks human review. |
| 31 | CLOSED | Ingest/embed verified Surah 12 Tafsir into MongoDB | Verified chunks, coverage, repeatable commands. | UNVERIFIED current DB; comment claims chunks/coverage but no live DB access. |
| 32 | CLOSED | Seed Quran and surah metadata for all 114 surahs | Full verified corpus and attributed metadata. | NOT IMPLEMENTED per code inspection: only per-ID ayah API, corpus artifacts scoped to Surah 12. |
| 33 | CLOSED | Introduction model/source registry/verification templates | Structured sections, refs, status/version, docs/tests. | PARTIAL: introduction model/template exist; source types/policy integration limited. |
| 34 | CLOSED | `GET /surahs` library endpoint/readiness | Metadata, attributed classification/readiness, unavailable state, cache headers. | NOT IMPLEMENTED: no route registration found. |
| 35 | CLOSED | Surah introduction public API | Published-only exact-style, hidden review data, unavailable/processing, attribution. | PARTIAL: handler/model/tests locally present; Mongo tests skipped; no public introduction UI. |
| 36 | CLOSED | Explanation style availability/status/ranges/cache headers | Registry/readiness, range correctness, ETag/cache, no fallback. | NOT IMPLEMENTED/PARTIAL: exact-style filter exists; requested availability/cache endpoints absent. |
| 37 | CLOSED | Audio metadata and delivery | Contract attribution/rights/status and ADR behavior. | PARTIAL: basic URL metadata exists; rights/status fields incomplete; licensing unresolved. |
| 38 | CLOSED | Measure retrieval scaling | Reproducible benchmark and recommendation threshold. | PARTIAL: brute-force is documented; no benchmark evidence inspected. |
| 39 | CLOSED | Generate/publish all three explanation styles | Pending, reviewed records, qualified review, public API. | CONFLICTED: closure comment admits review records missing; current DB unavailable. |
| 40 | CLOSED | Per-surah readiness report tooling | Report all content readiness types shared with library logic. | PARTIAL: `check_coverage` is source/ayah coverage, not a full cross-content readiness report. |
| 41 | CLOSED | Local preference specification | Locale suggestion, explicit override, privacy, fallback. | PARTIAL: design claims only; app has transient style state, no persistent prefs. |
| 42 | CLOSED | RAG grounding evaluation set/harness | Reviewer-approved set, per-style metrics, prompt-change regression. | SCAFFOLD/PARTIAL: eval files and runner exist; approval/CI gating not verified. |
| 43 | CLOSED | Introduction generation/review/Asbab pilot | Surah 12 typed grounded output, blocked unsupported claims, checklist, published via #35. | PARTIAL/BLOCKED: pipeline/tests local; public API exists; approval now deliberately returns 503 pending trusted identity; Mongo and real content unverified. |

GitHub status is reproduced as observed. Do not automatically change issue state based on this report.

## 9. Verification Results

Commands actually run for this audit, from the listed working directories:

| Command | Working directory | Outcome |
|---|---|---|
| `git branch --show-current`, `git rev-parse HEAD`, `git status --short --untracked-files=all`, `git log --oneline -12`, `git remote -v` | repository root | PASS; `main`, HEAD `eeb9e0fa5b5b88a88a22817504065bd1c80f41fc`; existing local changes preserved; `origin` configured. |
| `go test ./... -count=1` | `backend` | PASS. Go tests requiring Mongo skipped by their `MONGO_TEST_URI` checks; URI was not configured. No live DB verification. |
| `go build ./...` | `backend` | PASS; no output/errors. |
| `npm run build` | `web` | PASS; Next compile, lint/type validation, static generation and production build completed. |
| `npx tsc --noEmit` | `mobile` | FAIL: `mobile/App.tsx:9`, `StatusBar` does not accept `backgroundColor` under installed types. CI suppresses this with `|| true`. |
| `git diff --check` | repository root | PASS. |
| Check `MONGO_TEST_URI` and `MONGO_URI` presence | repository root | Both NOT CONFIGURED. No Mongo connection attempt. |
| Browser/device/manual playback, network tests, seed/audio/generation commands | — | NOT RUN. Avoided because this audit is read-only and no safe DB/service target was configured. |

The Go suite includes Mongo integration tests that skip when `MONGO_TEST_URI` is unset. A green `go test ./...` therefore does not constitute Mongo-backed or end-to-end workflow verification.

## 10. Known Gaps and Blockers

1. **P0 — reconcile religious source verification.** `corpus/sources/ibn-kathir-ar.json` says verified; `docs/reviews/ibn-kathir-ar-surah-12.md` says an automated-pilot consistency check and leaves 19 entries requiring human review; issue #30 claims human verification. Resolve with a qualified reviewer and a reproducible record before treating content as approved.
2. **P0 — no qualified reviewer identity.** Shared password plus typed name is not identity or qualification. Introduction approvals fail closed via 503. Existing explanation route still relies on the shared secret and UI-side checklist; direct API access can bypass the UI checklist.
3. **P0 — real DB/content state unknown.** No Mongo URI was available. Claims of loaded embeddings, published explanations, review audits, or recitations in GitHub comments cannot be confirmed in this checkout audit.
4. **P0 — issue #39 audit trail discrepancy.** Its closure comment states explanations were published while review records were absent. This conflicts with the policy that all published entries have reviews/citations.
5. **P1 — mobile typecheck fails.** `StatusBar` prop mismatch; CI masks the failure. The mobile UI is hardcoded to Surah 12 and several visible controls are placeholders.
6. **P1 — missing library/readiness APIs.** No `/surahs` list, style registry/availability, or general per-content readiness API was found. OpenAPI does not comprehensively cover all current routes/contracts.
7. **P1 — no public introduction experience.** Public API exists, but web reader calls only ayahs/explanations/reciters/recitations and does not fetch/render introductions.
8. **P1 — no verified Asbab/classification evidence workflow.** Introduction policy requires dedicated attributed classification/context evidence; current source manifest is tafsir verse-mapped and this audit did not verify suitable DB chunks.
9. **P1 — semantic validation is human-only.** Structure and refs can be checked, but citation existence does not establish entailment. This limitation is acknowledged in docs; review route remains disabled.
10. **P1 — audio rights unresolved; no downloads/offline.** URL metadata/direct playback code exist. Rights for EveryAyah URLs are not established. No offline/download feature is present.
11. **P2 — mobile style/status/error handling.** Style values `egyptian`/`english` differ from backend `egyptian_ar`/`en`; API clients often map errors to empty lists or parse without guards. No browser/device tests were run.
12. **P2 — deployment and operations.** Issue #15 open; Docker Compose runs Mongo only. No production deployment or backup/rollback configuration was verified.
13. **P2 — stale documentation.** `docs/local-stack-verification.md` still describes placeholder audio, contradicted by newer audio ingestion docs/code. Part 2 and deployment guide describe Postgres/Supabase, with Mongo pivot addendum explaining actual storage.

## 11. Recommended Next Steps

1. Reconcile source #30 verification with the actual review sheet; identify a qualified human owner and correct source registry metadata only after documented approval.
2. Configure an identity provider and explicit reviewer-role/qualification mapping. Then derive review identity from trusted auth, enforce the introduction checklist server-side, audit that identity, and test unauthorized/spoof attempts.
3. Provision an explicitly isolated Mongo test instance; run API, draft-store, and review-transition integration tests there. Verify current production content only under an authorized, separately controlled procedure.
4. Reconcile #39 published explanations with review audit records before serving them as reviewed content.
5. Fix the mobile StatusBar type error and remove CI’s `|| true`; align mobile style IDs and implement/label actual controls accurately.
6. Implement the library/introduction reader flow and style/readiness APIs from #32, #34–36, without cross-style fallback; validate OpenAPI in CI.
7. Make mobile/web API failures visible as errors rather than silently empty content; add end-to-end browser/device checks.
8. Resolve audio rights and attribution with rights holders before public release; only then decide whether streaming remains appropriate.
9. Reconcile docs and GitHub issue closure evidence against verified code/data, then complete issue #16 QA and #15 deployment planning.

## 12. Evidence and Unknowns

### Key implementation paths

- API wiring: `backend/cmd/api/main.go`; Mongo setup: `backend/internal/platform/db.go`; shared password: `backend/internal/platform/auth.go`.
- Quran: `backend/internal/quran/handlers.go`, `backend/internal/quran/service.go`, `backend/cmd/seed_quran/`.
- Explanations/API: `backend/internal/content/handlers.go`, `models.go`, `service.go`.
- Recitations: `backend/internal/recitation/service.go`, `backend/cmd/ingest_recitations/`, `scripts/seed_recitation.go`.
- Ingestion/source registry: `backend/internal/ingestion/`, `corpus/sources/ibn-kathir-ar.json`, `corpus/manifests/ibn-kathir-ar-surah-12.json`, `corpus/tafsir/ar-tafsir-ibn-kathir-surah-12.md`.
- Retrieval/generation: `backend/internal/retrieval/service.go`, `backend/internal/generation/{service.go,gemini.go,validation.go}`, `backend/cmd/generate_explanations/`.
- Introduction local changes: `backend/internal/introduction/`, `backend/cmd/generate_introduction/main.go`, `docs/surah-introduction-generation.md`.
- Review: `backend/internal/review/`, `web/app/admin/`, `docs/reviewer-dashboard.md`.
- Web/mobile surfaces: `web/app/surah/[id]/page.tsx`, `web/app/api-client/index.ts`, `mobile/App.tsx`, `mobile/src/screens/SurahScreen.tsx`, `mobile/src/api/client.ts`.
- CI/local runtime: `.github/workflows/ci.yml`, `Makefile`, `docker-compose.yml`.

### Unknowns not resolved by this audit

- Current Mongo collection counts, active source flags, embeddings, published content, recitations, and review audits.
- Whether any claimed real DB records are from production, a disposable database, or have since been cleared.
- Actual rendered web behavior with live API and playback on web/mobile devices.
- Rights-holder permission for the four linked reciters.
- Whether issue closure comments correspond to current state or qualified review artifacts.
- Production deployment, monitoring, backup, and recovery posture.

### Current Git baseline and preserved preexisting edits

Before creating this report, branch was `main`, HEAD and `origin/main` both `eeb9e0fa5b5b88a88a22817504065bd1c80f41fc`. The worktree already contained uncommitted changes; none were reset, reverted, stashed, cleaned, or overwritten. The report is an additional untracked file. No commit, push, database write, content generation, publication, or GitHub issue-state change was performed.
