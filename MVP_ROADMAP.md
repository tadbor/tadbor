# Tadbor Internal MVP Roadmap

**Prepared:** 2026-10-25  
**Purpose:** Prioritize a functioning internal end-to-end reading MVP; this is not a production release approval.  
**Evidence boundary:** Current source and local tests were inspected. No Mongo URI was configured, so database contents and Mongo integration behavior remain unverified. No content was generated, approved, or published during this planning audit.

## MVP Definition

The internal-testing MVP is the smallest coherent user journey that works from stored test data on both supported clients:

1. Start the Go API and MongoDB with an isolated, seeded test dataset.
2. Discover the seeded surah and open it.
3. Read ayahs in Quranic order and view surah information/introduction when available.
4. Select only a style the backend actually supports and request/read its explanation, with experimental status made unmistakable when the content has not been reviewed.
5. Play an available recitation and change reciter.
6. Navigate away and return without relying on hardcoded Surah 12 labels/IDs or dead controls.
7. See loading, empty, unavailable, and API-error states as different states.

**Internal-only content policy:** Experimental generated content may be used against controlled internal test data, but must be visibly labeled `Experimental / not reviewed` and must not be represented as verified, approved, or published trusted content. This roadmap does not authorize public serving of unreviewed religious content. Existing public endpoints should continue to expose only approved/published content unless a separate internal-only route, environment, and access boundary is explicitly designed and reviewed.

**Data-readiness distinction:** Internal engineering readiness does not require treating the disputed Ibn Kathir source as officially verified. Use the canonical Surah 12 text only as already seeded/validated by its corpus checks, and use clearly synthetic, non-Quranic fixture explanations/metadata for deterministic end-to-end tests. If testing experimental model output derived from tafsir is later approved for internal use, display the experimental label and do not change `verification_status`, create review records, or expose it through public published-content routes. Official source verification remains a specialist/release gate, not a prerequisite for mobile, navigation, sorting, error-state, or fixture-driven API work.

## Verified Current State

### Git and local work

- Current branch at audit: `main`; HEAD and `origin/main` were both `eeb9e0fa5b5b88a88a22817504065bd1c80f41fc`.
- Worktree already contains extensive uncommitted #35/#43 implementation and tests; `PROJECT_STATUS.md` and `PROJECT_STATUS_AND_PLAN.md` are also local documentation changes. Preserve these; do not reset/stash/clean or silently rewrite.
- No `MONGO_URI` or `MONGO_TEST_URI` was configured. No live DB was inspected.

### What code currently supports

- Backend public routes: `/health`, `/surahs/:surahId/ayahs`, `/surahs/:surahId/explanations`, `/surahs/:surahId/introduction`, `/reciters`, and `/surahs/:surahId/recitations`. No `GET /surahs` library route was found.
- Web reader requests one hardcoded explanation style, `simplified_ar`; the page renders Quran text, optional explanations, and direct-stream audio when API data is returned.
- Mobile starts at Surah 12, contains fetch/player code, and exposes style values `simplified_ar`, `egyptian`, and `english`; the latter two do not match backend style IDs `egyptian_ar` and `en`.
- Quran service queries by surah without explicit sort. Web/mobile rendering trusts response order, so wrong ayah order is a real correctness risk.
- API client in web converts failed/non-list responses into empty lists; mobile API client parses JSON without checking HTTP status or validating response shape. Both can make a service failure look like no content.
- Introduction CLI defaults to a no-generation preview and `-live` opts into LLM generation plus pending-draft persistence. Pipeline uses the retrieval and source-registry code, requires structural per-claim refs and pinned provenance, and stores pending drafts. No evidence from a real DB proves the end-to-end flow is presently runnable.
- Introduction decision HTTP endpoint deliberately returns 503 because shared-password login plus caller-typed name is not a trusted reviewer identity/qualification. This is correct for approval security, but should not prevent unrelated reading/API/mobile work.
- Mongo integration tests exist and skip when `MONGO_TEST_URI` is absent. CI does not configure Mongo and therefore cannot verify database behavior as written.
- Source state is inconsistent in repository records: source JSON says verified, while the review sheet describes automated-pilot review and 19 entries still needing human decisions; README/policy text also says unverified. This must be reconciled before using that source as approved test evidence. Internal experimental fixtures can be synthetic and clearly labeled; do not flip the registry state or fabricate reviewer records.

## Core User Journey and Phase Gates

### Internal MVP journey

| Step | Required behavior | Current status | MVP verification |
|---|---|---|---|
| Testability baseline | Mobile typecheck passes; CI has isolated Mongo integration service and fixture lane. | BLOCKED: mobile typecheck currently fails; CI masks it and provides no Mongo. | CI demonstrates type errors fail and DB integration tests execute rather than skip. |
| Read ayahs | Correct canonical order and clear ayah identity. | PARTIAL: data route exists but Mongo query lacks explicit sort. | Unit/query test asserts order 1..N; integration fixture inserts shuffled order and returns canonical order. |
| Synthetic end-to-end data | Disposable fixtures represent one surah, supported explanation styles and recitation metadata without claiming religious authenticity. | BLOCKED: no complete shared journey fixture proven. | Seeder refuses non-test targets; backend journey test verifies metadata, order, styles, reciters and public exposure. |
| Errors and empty states | Distinguish loading, empty/unavailable, and request failures. | INCOMPLETE: web/mobile API clients can turn errors into empty data. | Mocked 404/500/invalid JSON and empty 200 tests verify separate UI states and retry. |
| Browse/select surah | Library list from seeded surah metadata; unsupported/unseeded IDs show unavailable. | BLOCKED: no list endpoint; mobile hardcodes 12. | `GET /surahs` handler tests with seeded and unseeded cases; web/mobile navigation tests. |
| Surah overview/introduction | Show metadata and introduction when available, otherwise distinct unavailable/experimental state. | PARTIAL: public API exists; no reader UI. | API contract and web/mobile tests against fixtures; any experimental content is conspicuously labeled and segregated. |
| Choose explanation | Valid API style IDs, no silent cross-style substitution; switch in place. | PARTIAL: web hardcoded; mobile IDs mismatch; no registry endpoint. | Shared style fixture and client tests; unavailable style remains unavailable. |
| View explanation | Clearly separate Quran and explanation; internal experimental status is unmistakable; public APIs remain published-only. | PARTIAL: web renders published explanation; no internal experimental reader path. | Test fixtures include published and pending records; public endpoint never returns pending; internal test build labels experimental. |
| Listen to audio | Available URL plays, play/pause/stop and reciter switching work; unavailable URL has useful state. | PARTIAL: web native controls; mobile play code has a restart-on-“pause” defect. Rights unresolved for external release. | Browser/device playback smoke tests against licensed/test audio fixtures; no production URL dependency in unit tests. |
| Internal release | Local/test-only URLs and content; no unreviewed content public. | NOT VERIFIED; no deployment exists in evidence. | Integration environment variables point only to disposable Mongo; explicit deployment gate blocks public experimental content. |

## Prioritized Issue-by-Issue Roadmap

**Issue state is GitHub’s observed state on 2026-10-25.** Recommendations below do not change issue state. Existing closed issues are recommended for reopening/re-scoping only when their current acceptance scope blocks this MVP; do not create duplicates before maintainers reconcile existing issues.

| Priority / phase | Issue | Recommended disposition | User-visible outcome | Dependencies | Acceptance criteria / verification |
|---|---|---|---|---|---|
| P0 / CI and infrastructure | #27 CLOSED, CI baseline | **Reopen/re-scope first**: fix mobile typecheck and remove `|| true`; provide an isolated Mongo service and require integration tests to run in CI. | Failures in either app or persistence path become visible before UI work compounds them. | CI workflow; disposable Mongo service. | `go test ./... -count=1`; required Mongo tests fail rather than skip in CI; web build; direct mobile typecheck passes without suppression. CI database must be isolated and discarded. |
| P0 / fixture data and backend journey | New issue NFIX | **Create** after duplicate search (none found in the current inventory). Seed only a disposable database with clearly synthetic/non-Quranic explanation fixtures and valid seeded Surah 12 text/recitation metadata. | A deterministic backend end-to-end test exercises the real Mongo repositories without claiming religious content approval. | #27 isolated Mongo CI; no dependency on source #30 approval. | Seeder refuses non-test targets; fixtures visibly identify themselves as test/experimental; verify surah metadata, shuffled-insertion ayah ordering, exact-style explanations, recitations, and pending/rejected public exposure. Never alter source verification or create review records for real content. |
| P0 / correctness | New issue N3 | **Create** after duplicate search (none found). | Ayahs always display in canonical Quranic order. | None; Mongo integration test becomes available with #27. | Sort by `ayah_number` at the service boundary; unit test ordered output and Mongo test inserts shuffled ayahs yet returns 1..N. Inspect other client-rendered ordered lists too. |
| P0 / client failure states | New issue N4 | **Create** after duplicate search (none found). | API outage and malformed response no longer masquerade as an empty surah. | #27 for client test gates. | Web/mobile distinguish loading, empty 200, unavailable 404/202 as contracted, network/5xx and invalid JSON; retry works; mocked tests cover 404, 500, malformed JSON, empty 200. |
| P1 / surah discovery | #32 CLOSED + #34 CLOSED | **Reopen/re-scope** together: a bounded, attributed MVP metadata set is enough; defer all-114 corpus expansion. | Browse and select seeded surahs; absent entries show unavailable. | #29 contract; attributed metadata source/fixture. | Implement data-backed `GET /surahs`, sorted by number; use names/counts only from named metadata sources. Classification may be unknown/omitted unless supported. Web/mobile selectors consume the endpoint, no hardcoded-only path. |
| P1 / reader experience | #19 OPEN and #20 OPEN | **Keep open and re-scope for internal MVP.** | Browse/select, intro availability, ordered ayahs, supported explanation styles, reciter/audio controls on web and mobile. | #27, #29, #32/#34, #36, #37 contract as needed, N3/N4/NFIX. | Browser/device journey test against disposable fixture backend; real controls, no Surah 12-only labels/IDs, correct style IDs, distinct error/empty/unavailable states. Experimental content must be visibly labeled and never presented as approved. |
| P1 / styles and explanation contract | #29 CLOSED + #36 CLOSED | **Reopen/extend** only the client-needed contract now; stage full registry/ETag/readiness expansion. | Clients select supported styles with no cross-style fallback. | #25 style policy. | Canonical IDs `simplified_ar`, `egyptian_ar`, `en`; range shape documented; exact-style tests; unavailable differs from error; OpenAPI fixtures validated in CI. |
| P1 / introduction experimentation | #35 CLOSED + #43 CLOSED | **Re-scope #43 to internal experimental pipeline; #35 stays published-only.** | Internal testing can inspect drafts with a clear experimental label; public API remains published-only. | #27/#28, #33 model, NEXP access-boundary decision; suitable test fixture/evidence. | Preview does no model call/write; opt-in test path persists only pending/experimental records in disposable DB; reader internal mode is isolated and clearly labeled; public route never returns pending/rejected/experimental records. No approval/publication through this phase. |
| P1 / audio playback | #37 CLOSED and #14 CLOSED | **Keep existing work; only reopen #37 if verified API contract fields block the MVP.** | Available audio can be selected/played; failures are explicit. | #24 delivery ADR; lawful fixture or internal-only source decision. | Web/mobile playback controls and unavailable state tested. External EveryAyah use remains internal only until rights are resolved; no download or redistribution. |
| P2 / trusted review and official content | #30/#26/#33 CLOSED; #11/#43 CLOSED | **Defer specialist approval, but retain as a hard public-release gate.** | Qualified review can replace experimental test data later. | Named qualified reviewer; identity-provider/authorization decision; reproducible source verification. | Reconcile source evidence, reviewer qualifications and printed edition; API-derived reviewer identity; server-enforced checklist and provenance checks; approval route remains 503 until proven. |
| P2 / release | #15 OPEN, #16 OPEN, #22 OPEN, #23 OPEN | **Keep open and defer public release.** | Public launch only after security, rights, content, offline/QA and operations requirements. | P0/P1 internal MVP; human policy decisions. | Deployment, backup/rollback, rights clearance, public content audit, slow/offline/accessibility QA and explicit go/no-go. |
### Issue #44 and later

Repository-wide GitHub issue search returned 30 issues through #30. Exact-title searches and direct lookups resolved issues #31–43. A search for `repo:tadbor/tadbor is:issue number:>43` returned only #43 (GitHub search interpreted the query unexpectedly); the complete initial repository search showed no issue #44+. **No #44+ issue was found at audit time.** Recheck GitHub immediately before creating any issue, since issue state may change after this document.

### Existing issue acceptance highlights

- #15 asks for a public URL, hosted Mongo, all three styles, and four reciters.
- #16 requires manual citation/adversarial/slow/offline QA and a written QA note.
- #32 asks all 114 surahs with attributed metadata and disputed classifications flagged.
- #34 requires a data-derived `GET /surahs`, unavailable state for absent records, OpenAPI and cache headers.
- #35 requires published-only introduction, hidden internal fields, processing/unavailable states, attribution.
- #36 requires style availability, per-ayah readiness/ranges, ETag/Cache-Control, and no fallback.
- #37 requires audio readiness/rights/attribution metadata consistent with ADR #24.
- #43 requires structured source-cited introduction generation, unsupported-claim rejection, checklist review, and public visibility after approval. Current HTTP approval is intentionally disabled pending identity authorization.

## Existing Project Issues / Delivery Status

Keep these statements current as work proceeds:

- #15 and #16 remain the clearest deployment and QA/release gates.
- #17–23 are epics; organize work under them rather than opening duplicates.
- #32 and #34–37 are marked CLOSED but their acceptance criteria are only partially reflected in the current code inspection; recommend reopening/re-scoping where the internal MVP requires them.
- #43 is marked CLOSED, but its human approval/publication requirement is not active in the current registered HTTP route. Keep introduction approval disabled until identity/qualification authorization is real.
- #30 and #39 closure comments conflict with recorded verification/audit evidence. Do not claim they pass until a qualified person reconciles source and review records.

## Deferred Beyond Internal MVP

- Qualified specialist review and public release of generated Quranic explanations/introductions.
- Establishing printed-edition fidelity/provenance and reconciling the 19 flagged entries.
- Per-reviewer identity, qualifications, role-based authorization, and durable audit attribution.
- Full 114-surah verified Quran metadata, tafsir, introductions, Asbab al-Nuzul, and readiness.
- External audio rights clearance, rehosting, downloads, and offline caching.
- Production deployment, backups, recovery, rate limiting, observability, and final release sign-off.

## Blocking Bugs and Infrastructure

1. Backend ayah query has no explicit sort; add/test canonical ayah order before using returned slices in readers.
2. Web/mobile API clients can turn transport/status/parse failures into empty arrays; define visible error state separate from empty/unavailable.
3. Mobile typecheck failure at `App.tsx:9`; CI currently suppresses typecheck exit via `|| true`.
4. Mobile uses `egyptian`/`english`; backend IDs are `egyptian_ar`/`en`.
5. Mobile playback button label implies pause but handler restarts playback.
6. No `GET /surahs` route; both apps are currently Surah 12-centric.
7. Isolated Mongo CI service/test database is needed for integration coverage; never point CI tests at production.
8. No production `.env`/Mongo state was read. Current availability of LLM/embedding credentials is unknown; do not run live jobs just to test basic navigation.

## Verification Results

| Command | Directory | Outcome |
|---|---|---|
| `git branch --show-current`; `git rev-parse HEAD`; `git status --short --untracked-files=all`; `git log --oneline -8` | repo root | `main`; HEAD `eeb9e0fa5b5b88a88a22817504065bd1c80f41fc`; existing local work remains uncommitted. |
| `go test ./... -count=1` | `backend` | PASS for executed unit/hermetic tests. Mongo integration tests skipped without `MONGO_TEST_URI`. |
| `go build ./...` | `backend` | PASS. |
| `npm run build` | `web` | PASS. |
| `npx tsc --noEmit` | `mobile` | FAIL at `App.tsx:9` on unsupported `StatusBar.backgroundColor`; CI masks via `|| true`. |
| `git diff --check` | repo root | PASS. |
| Mongo availability check | repo root | `MONGO_URI` and `MONGO_TEST_URI` not configured. Mongo data/integration unavailable. |

No Mongo-backed user journey or live source/LLM/audio request was executed. No content was generated, approved, published, or written to a database during this plan.

## Recommended Implementation Order

1. Fix ayah ordering and make API client failures distinct from empty data; add unit tests before building screens on top.
2. Fix the mobile TypeScript failure and remove CI masking, so subsequent UI work cannot merge with an untypechecked client.
3. Add isolated Mongo service to CI and prove one fixture-driven end-to-end backend path (surah metadata → ordered ayahs → supported-style explanations → recitation metadata). Keep production/publication collections isolated.
4. Reopen/re-scope #34 for `GET /surahs` and implement an actual browse/select flow in web and mobile using fixture-backed data.
5. Align style IDs and add a selector with distinct loading/unavailable/error states; never cross-style fallback.
6. Connect introduction to the reader only as `Experimental / not reviewed` in an internal-only test mode. Do not expose these records through public APIs or label them trusted/published.
7. Implement audio controls and failure states with test audio or explicitly approved source; keep rights-dependent public release out of scope.
8. Complete internal end-to-end tests and QA; then stop at the internal MVP gate until specialist review, reviewer identity and rights decisions are resolved.

**First task to implement:** fix mobile typecheck and remove CI failure masking under #27, while provisioning the isolated Mongo CI service in the same issue. This immediately makes both clients and persistence tests trustworthy; then implement N3 canonical ayah ordering and run the NFIX fixture journey. These steps require no production content and do not depend on resolving official source verification.
