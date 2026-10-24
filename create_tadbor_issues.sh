#!/usr/bin/env bash
# Tadbor: create epics, issues, labels, milestones and dependency links on GitHub.
#
#   cd /path/to/tadbor
#   DRY_RUN=1 ./create_tadbor_issues.sh    # default: inspect + print plan, creates nothing
#   DRY_RUN=0 ./create_tadbor_issues.sh    # create missing labels/milestones/issues
#
# Requires: bash >= 4, gh (authenticated), jq. Safe to re-run: issues whose exact
# title already exists (open or closed) are reused, never duplicated or overwritten.
# Existing labels/milestones are reused; nothing is deleted or closed.
set -euo pipefail

[ "${BASH_VERSINFO[0]}" -ge 4 ] || { echo "bash >= 4 required (macOS: brew install bash)"; exit 1; }
command -v gh >/dev/null || { echo "gh is required"; exit 1; }
command -v jq >/dev/null || { echo "jq is required"; exit 1; }
DRY_RUN="${DRY_RUN:-1}"

# ---------- 1. Preflight: auth, repository, existing GitHub state ----------
gh auth status
REPO="$(gh repo view --json nameWithOwner -q .nameWithOwner)"
echo "Git remote: $(git remote get-url origin 2>/dev/null || echo 'n/a')"
echo "Target repository: $REPO   (DRY_RUN=$DRY_RUN)"
if [ -z "${SKIP_CONFIRM:-}" ]; then
  read -r -p "Is this the correct Tadbor repository? [y/N] " ok
  [ "$ok" = "y" ] || { echo "Aborted."; exit 1; }
fi

WORK="$(mktemp -d)"
gh issue list --repo "$REPO" --state all --limit 1000 --json number,title,state > "$WORK/issues.json"
gh label list --repo "$REPO" --limit 500 --json name -q '.[].name' > "$WORK/labels.txt"
gh api "repos/$REPO/milestones?state=all&per_page=100" --jq '.[].title' > "$WORK/milestones.txt"

echo; echo "== Existing issues ($(jq length "$WORK/issues.json")) - review for semantic duplicates =="
jq -r '.[] | "  #\(.number) [\(.state)] \(.title)"' "$WORK/issues.json"
echo "== Existing labels =="; sed 's/^/  /' "$WORK/labels.txt"
echo "== Existing milestones =="; sed 's/^/  /' "$WORK/milestones.txt"
echo
if [ -z "${SKIP_CONFIRM:-}" ]; then
  read -r -p "Continue? Exact-title duplicates are skipped automatically. [y/N] " ok
  [ "$ok" = "y" ] || { echo "Aborted."; exit 1; }
fi

# ---------- 2. Labels and milestones (create only if missing) ----------
# If your repo already uses other names (e.g. "priority: P0"), edit them here.
ensure_label() {
  grep -Fxq -- "$1" "$WORK/labels.txt" && return 0
  echo "label to create: $1"
  [ "$DRY_RUN" = 1 ] || gh label create "$1" --repo "$REPO" --color "$2" --description "$3" >/dev/null
}
ensure_ms() {
  grep -Fxq -- "$1" "$WORK/milestones.txt" && return 0
  echo "milestone to create: $1"
  [ "$DRY_RUN" = 1 ] || gh api -X POST "repos/$REPO/milestones" -f title="$1" -f description="$2" >/dev/null
}

ensure_label epic 5319e7 "Tracks a workstream"
ensure_label needs-decision fbca04 "Product/architecture decision required before implementation"
ensure_label content-review d93f0b "Requires human religious-content review"
ensure_label "ws:A-content" 0e8a16 "Workstream A: content foundation"
ensure_label "ws:B-backend" 1d76db "Workstream B: backend and content APIs"
ensure_label "ws:C-web" 1d76db "Workstream C: web reading experience"
ensure_label "ws:D-mobile" 1d76db "Workstream D: mobile reading experience"
ensure_label "ws:E-preferences" 006b75 "Workstream E: local preferences and privacy"
ensure_label "ws:F-audio" 0052cc "Workstream F: audio and offline"
ensure_label "ws:G-quality" bfd4f2 "Workstream G: quality and release readiness"
for p in P0 P1 P2 P3; do ensure_label "$p" b60205 "Priority $p"; done
for e in S M L; do ensure_label "effort:$e" c5def5 "Effort $e"; done

MS0="M0 - Decisions and contracts"
MS1="M1 - Content foundation and backend APIs"
MS2="M2 - Core web reading experience"
MS3="M3 - Mobile, audio at scale, quality"
MS4="M4 - Content scale-up to 114 surahs"
MS5="M5 - Release readiness"
ensure_ms "$MS0" "Decisions, shared contracts and safety tests that unblock parallel work"
ensure_ms "$MS1" "Verified Surah 12 pipeline, data model and public APIs"
ensure_ms "$MS2" "Library, introduction, ayah cards, style selector, audio player on web"
ensure_ms "$MS3" "Mobile parity, audio licensing/ingestion, e2e and accessibility checks"
ensure_ms "$MS4" "Scale-out workflow and tooling for surahs 2-114"
ensure_ms "$MS5" "Production readiness and go/no-go"

# ---------- 3. Issue helpers ----------
declare -A NUM TITLE REUSED_FLAG CHILDREN EPIC
CREATED=0; REUSED=0

existing_number() {
  jq -r --arg t "$1" '[.[] | select(.title == $t)][0].number // empty' "$WORK/issues.json"
}

link_blocked() { # issue, blocker (best-effort; needs GitHub issue-dependencies API)
  local id; id="$(gh api "repos/$REPO/issues/$2" --jq .id 2>/dev/null)" || return 0
  gh api -X POST "repos/$REPO/issues/$1/dependencies/blocked_by" -F issue_id="$id" >/dev/null 2>&1 || true
}

DOD='All acceptance criteria checked; tests added and passing in CI; docs updated; reviewed and merged; no review, verification or publication safeguard weakened.'

mk() {
  local key="$1" ws="$2" pri="$3" eff="$4" ms="$5" extra="$6" deps="$7" title="$8"
  local ctx="$9" cur="${10}" scope="${11}" oos="${12}" impl="${13}" ac="${14}" tst="${15}" dod="${16:-}"
  TITLE[$key]="$title"
  local found; found="$(existing_number "$title")"
  if [ -n "$found" ]; then
    NUM[$key]="$found"; REUSED=$((REUSED+1)); REUSED_FLAG[$key]=1
    echo "reuse   #$found  $title"; CHILDREN[$ws]+="- [ ] #$found $title"$'\n'; return 0
  fi
  local f="$WORK/$key.md"
  {
    echo "### Context"; echo "$ctx"; echo
    echo "### Current State"; echo "$cur"; echo "_Derived from the audit snapshot; re-verify against current main before starting._"; echo
    echo "### Scope"; echo "$scope"; echo
    echo "### Out of Scope"; echo "$oos"; echo
    echo "### Implementation Notes"; echo "$impl"; echo
    echo "### Dependencies"
    if [ -z "$deps" ]; then echo "None."; else
      for d in $deps; do
        if [ -n "${NUM[$d]:-}" ]; then echo "- Blocked by #${NUM[$d]} ${TITLE[$d]}"
        else echo "- Blocked by ${TITLE[$d]:-$d} (planned)"; fi
      done; fi
    echo
    echo "### Acceptance Criteria"
    IFS='|' read -ra A <<<"$ac"; for a in "${A[@]}"; do echo "- [ ] $a"; done; echo
    echo "### Testing"; echo "$tst"; echo
    echo "### Definition of Done"; echo "$DOD ${dod}"; echo
    echo "### Priority and Effort"; echo "Priority: $pri | Effort: $eff"
  } > "$f"

  local labels=(--label "$ws" --label "$pri" --label "effort:$eff")
  [ -n "$extra" ] && for x in $extra; do labels+=(--label "$x"); done
  if [ "$DRY_RUN" = 1 ]; then
    echo "create  [$pri/$eff] $title   (milestone: $ms; deps: ${deps:-none})"; NUM[$key]=""
  else
    local url; url="$(gh issue create --repo "$REPO" --title "$title" --body-file "$f" --milestone "$ms" "${labels[@]}")"
    NUM[$key]="${url##*/}"; CREATED=$((CREATED+1))
    echo "created #${NUM[$key]}  $title"
    for d in $deps; do [ -n "${NUM[$d]:-}" ] && link_blocked "${NUM[$key]}" "${NUM[$d]}"; done
  fi
  CHILDREN[$ws]+="- [ ] #${NUM[$key]:-?} $title"$'\n'
}

epic() { # ws, title, description
  local ws="$1" title="$2" desc="$3"; TITLE[$ws]="$title"
  local found; found="$(existing_number "$title")"
  if [ -n "$found" ]; then EPIC[$ws]="$found"; REUSED=$((REUSED+1)); REUSED_FLAG[$ws]=1; echo "reuse   #$found  $title"; return 0; fi
  if [ "$DRY_RUN" = 1 ]; then echo "create  [epic] $title"; EPIC[$ws]=""; return 0; fi
  local url; url="$(gh issue create --repo "$REPO" --title "$title" --body "$desc"$'\n\n_Child issues are listed below once created._' --label epic --label "$ws")"
  EPIC[$ws]="${url##*/}"; CREATED=$((CREATED+1)); echo "created #${EPIC[$ws]}  $title"
}

# ---------- 4. Epics ----------
epic ws:A-content "[Epic] A - Content foundation: verified sources, introductions, scale-out" "Verified Quran data for 114 surahs, surah metadata, introductions and Asbab al-Nuzul with source attribution, tafsir verification/ingestion, readiness reporting and the human-review workflow at scale."
epic ws:B-backend "[Epic] B - Backend and content APIs" "Library, introduction, explanation-style, audio-metadata APIs with explicit readiness states and safe fallbacks. Published, reviewed content only."
epic ws:C-web "[Epic] C - Web reading experience" "Surah library, introduction screen, ayah cards with inline explanations, style selector with in-place switching, persistent audio player, and complete loading/error/unavailable states."
epic ws:D-mobile "[Epic] D - Mobile reading experience" "Expo app parity: library, introduction, ayah cards, style selector, local preferences and audio playback."
epic ws:E-preferences "[Epic] E - Local preferences and privacy" "Language/dialect preference stored on-device only, locale-suggested defaults with manual override, no backend profile."
epic ws:F-audio "[Epic] F - Audio and offline experience" "Audio delivery decision, preparation states, rights/attribution, ingestion beyond Surah 12."
epic ws:G-quality "[Epic] G - Quality and release readiness" "CI, safeguard regression tests, e2e, RAG evaluation, accessibility, production readiness and release gate."

# ---------- 5. Issues (created in dependency order) ----------
# ===== M0: decisions, contracts, safety tests =====
mk D1 ws:F-audio P0 S "$MS0" "needs-decision" "" \
'Decide audio delivery strategy: stream, temporary download, or opt-in offline cache' \
'Readers need clear audio loading states without the app silently downloading the whole Quran. The delivery model drives backend, web and mobile audio work.' \
'Surah 12 has 444 recitation files across 4 reciters from EveryAyah-sourced data; hotlinking rights are unresolved (docs/recitation-audio-sources.md). Verify how /surahs/:id/recitations returns URLs today.' \
'Write an ADR comparing: (1) stream from source, (2) stream from our own storage/CDN, (3) temporary in-session cache, (4) opt-in per-surah offline download. Cover size estimates, hosting cost, licensing, web vs mobile differences and failure behavior. Recommend one model per platform.' \
'Implementing any option; bulk-downloading audio.' \
'docs/decisions/ (new), docs/recitation-audio-sources.md, backend/internal/recitation.' \
'ADR merged with a per-platform decision|States that all-Quran audio is never downloaded automatically|Defines behavior for unavailable audio, interrupted playback and navigation during playback|Licensing risk and required attribution recorded|If offline download is chosen, a separate scoped issue (storage limits, eviction, delete) is opened' \
'Maintainer review and sign-off on the ADR.'

mk D2 ws:B-backend P0 S "$MS0" "needs-decision" "" \
'Decide translation-versus-explanation policy and the extensible explanation-style registry' \
'English output must not silently stand in for an authenticated translation, and new dialects/languages must be addable without schema changes.' \
'Styles simplified_ar, egyptian_ar and en exist in the content API style param and generation validation (script check only); dialect fidelity is verified only by the human checklist.' \
'Define style registry fields (id, language, dialect, kind = explanation|translation, display label, direction, status). Decide whether en is a simplified explanation of tafsir or a cited translation of a named licensed translation, how each is labelled in the UI, which reviewer qualification each dialect needs, and the process to add a dialect.' \
'Building the registry endpoint (B4) or any UI.' \
'docs/decisions/, backend/internal/generation/validation.go, backend/internal/content.' \
'ADR merged|UI label wording agreed per style kind|Reviewer qualification rule defined per dialect|Adding a style needs no schema change (documented)|Quranic text is never presented as translated by an explanation style' \
'Maintainer and Arabic/English content-reviewer sign-off.'

mk D3 ws:A-content P0 S "$MS0" "needs-decision content-review" "" \
'Define source and review policy for surah introductions, classification and Asbab al-Nuzul' \
'Introductions and reasons of revelation are religious claims. Invented or unattributed content is unacceptable, and some reports and classifications are disputed.' \
'Only tafsir sources are registered today (corpus/sources/*.json, source registry). No policy exists for historical/Asbab/classification sources (verify).' \
'Define approved source list and authority tiers, how disputed reports and differing scholarly views are represented (attribution, hedged wording), the Makki/Madani classification source and handling of disputed surahs, required intro fields, citation format, reviewer checklist and who may approve.' \
'Authoring introductions; building the data model (A5).' \
'docs/ (new policy doc), corpus/sources/, docs/tafsir-source-registry.md.' \
'Policy doc merged with approved source list|Disputed-report representation rules defined with examples|Reviewer checklist for introductions defined|Rule stated: no source evidence means no published introduction|Named human owner for approvals' \
'Sign-off by the project religious-content reviewer.' \
'Policy cannot be approved by engineering alone.'

mk G1 ws:G-quality P1 S "$MS0" "" "" \
'Establish CI and record a verified baseline: go test, web build, lint, mobile typecheck' \
'The audit did not execute tests or builds, so current health is unknown.' \
'Many backend *_test.go files exist; pass/fail unknown. No CI configuration confirmed.' \
'Run go test ./..., web npm run build, lint and mobile typecheck; fix trivial breakages or file follow-ups; add a CI workflow running them on PRs.' \
'New feature tests; deployment pipelines.' \
'.github/workflows/, backend/, web/, mobile/ (note uncommitted mobile/package*.json in audit; do not discard).' \
'Baseline results recorded in the issue|CI workflow runs backend tests, web build and lint on every PR|Failures found are fixed or tracked in linked issues' \
'CI green on main.'

mk G2 ws:G-quality P0 M "$MS0" "" "" \
'Add regression tests proving pending, rejected and unreviewed content is never publicly exposed' \
'The review gate is the core safety property. New endpoints (library, introductions, audio, style availability) must not weaken it.' \
'Content service hard-filters review_status = published; review_status and source_version_snapshot hidden from public API. Test coverage of this on every public route is unverified.' \
'Table-driven tests seeded with pending/rejected/published/escalated explanations asserting public routes return only published items and never leak review_status or snapshot fields; a test enumerating router public routes so new routes cannot skip the check. Extend to introductions when B3 lands.' \
'Changing the review flow.' \
'backend/internal/content, backend/internal/api, router tests.' \
'Tests cover all current public routes|Router-enumeration test fails if a new public route has no exposure test|Leak of review_status/source_version_snapshot fails the test|Checklist item added to B3 and B4 to extend these tests' \
'go test passes in CI.'

mk B1 ws:B-backend P0 M "$MS0" "" "D2" \
'Define shared content-readiness model and API contract (OpenAPI spec + fixtures + contract tests)' \
'Web and mobile can build in parallel only against a stable contract. Content availability differs per surah, content type and style.' \
'No readiness concept exists; explanations endpoint returns published items only, so empty is ambiguous (not generated vs not reviewed vs nonexistent).' \
'Define readiness enum (available, processing, unavailable) per surah for: ayah text, introduction, audio, and each explanation style. Write OpenAPI for new/changed endpoints (library, introduction, style availability, per-ayah explanations, audio metadata) incl. error shape. Provide JSON fixtures: Surah 12 plus synthetic surahs in each state. Add contract tests validating fixtures and handlers against the spec. Review state is never exposed.' \
'Implementing endpoints (B2-B5) or UI.' \
'docs/api/openapi.yaml (new), fixtures dir shared by web and mobile, backend/internal/api tests.' \
'OpenAPI merged and reviewed by backend, web and mobile owners|Fixtures cover available/processing/unavailable for each content type|Contract tests fail on spec drift|Spec states unavailable content is never substituted by another style' \
'Spec validates in CI (e.g. openapi lint) and fixtures validate against it.'

# ===== M1: content foundation and backend =====
mk A1 ws:A-content P0 M "$MS1" "content-review" "" \
'Complete human verification of Ibn Kathir (Arabic) Surah 12 and record the printed edition' \
'Retrieval uses only source_verified chunks, so unverified Ibn Kathir blocks all generation. This is a human task, not a code change.' \
'corpus/sources/ibn-kathir-ar.json: verification_status unverified, ingestion_status not_started; docs/reviews/ibn-kathir-ar-surah-12.md lists 19 items. Digitisation is a quran.com API response, not tied to a printed edition.' \
'Reviewers resolve the 19 items, identify the printed edition (or decide the source is unsuitable and select another), and record a decision with reviewer identity and date. Only then update verification_status.' \
'Other surahs; changing retrieval verification rules.' \
'docs/reviews/ibn-kathir-ar-surah-12.md, corpus/sources/ibn-kathir-ar.json.' \
'All 19 items resolved with rationale|Printed edition recorded or source replaced|Decision logged with reviewer and date|verification_status changed only after the logged decision' \
'Review sheet diff and make seed/coverage checks.' \
'Do not flip the flag to unblock engineering.'

mk A2 ws:A-content P0 M "$MS1" "" "A1" \
'Ingest and embed verified Surah 12 tafsir into MongoDB with coverage checks' \
'Generation needs verified tafsir_chunks in the real database.' \
'Ingestion tooling exists (fetch_tafsir, build_manifest, check_coverage, register_source, ingest, embed_chunks) and was verified only against a throwaway DB.' \
'Register the verified source, ingest, embed (if semantic fallback is used) and run coverage checks against the real DB. Document the exact command sequence.' \
'Other surahs or sources.' \
'backend/cmd/*, backend/internal/ingestion, docs/tafsir-embeddings.md.' \
'Chunks present with source_verified true and correct ayah ranges|check_coverage passes for Surah 12|Commands documented and repeatable' \
'Run coverage/validation tooling; spot-check retrieval for sample ayahs.'

mk A4 ws:A-content P1 M "$MS1" "content-review" "D3" \
'Seed verified Quran text and surah metadata for all 114 surahs' \
'The library needs all surahs; text must be verified, not assumed.' \
'seed_quran and seed-check cover Surah 12 only (111 ayahs Uthmani, cross-checked against quran.com).' \
'Generalize seeding and seed-check to 114 surahs with a second-source cross-check. Add metadata per surah: number, Arabic name, transliteration, ayah count, revelation classification with source attribution (disputed cases flagged). English names carry a named source. No explanations.' \
'Tafsir, introductions, audio, images.' \
'backend/cmd/seed_quran, backend/internal/quran, docs/quran-corpus-validation.md.' \
'seed-check passes for all 114 surahs|Metadata has source attribution per field|Disputed classifications flagged, not silently chosen|Idempotent re-run' \
'make seed-check; unit tests for cross-check mismatches.'

mk A5 ws:A-content P1 M "$MS1" "content-review" "D3 B1" \
'Add surah-introduction data model, source-registry types and verification templates' \
'Introductions need the same grounding, versioning and review fields as explanations.' \
'Only tafsir chunks and explanations exist; no introduction collection or non-tafsir source types.' \
'Collection (e.g. surah_introductions) with structured sections (classification, context of revelation, themes, stories), per-claim source_refs, differing-reports field, style, review_status (hidden), prompt/model/source versions. Extend source registry for history/Asbab sources and add a verification-sheet template.' \
'Generation (A6), API (B3), UI.' \
'backend/internal/content, backend/internal/retrieval, corpus/sources, docs/.' \
'Schema and indexes documented|Source registry supports new source types|Verification sheet template added|Unit tests for validation (claims require refs)' \
'go test for model validation.'

mk B2 ws:B-backend P1 M "$MS1" "" "B1" \
'Add surah library endpoint (GET /surahs) with metadata and readiness states' \
'The library must list all 114 surahs and show what is ready.' \
'Public routes are per-surah (ayahs, explanations, recitations); no list endpoint was identified in the audit (verify).' \
'GET /surahs returning number, names, ayah count, attributed classification and readiness per content type per B1. Works with Surah 12 data before A4 completes.' \
'Cover images (A10); introductions content.' \
'backend/internal/quran, backend/internal/api, docs/api/openapi.yaml.' \
'Response matches the OpenAPI contract|Readiness derived from real data, never hard-coded|Not-yet-seeded surahs return unavailable, not errors|Cache headers set' \
'Handler and contract tests; G2 leak test extended.'

mk B3 ws:B-backend P1 M "$MS1" "" "B1 A5" \
'Add surah introduction API serving published introductions only' \
'The reading flow starts with an introduction.' \
'No introduction endpoint exists.' \
'GET /surahs/:id/introduction?style= returning published sections with attribution and differing-reports notes; explicit unavailable/processing response otherwise; never another styles text as a fallback.' \
'Generation; UI.' \
'backend/internal/content, docs/api/openapi.yaml.' \
'Only published introductions returned|review_status and snapshots hidden|Unavailable and processing states per contract|Source attribution included' \
'Contract tests; extend G2 exposure tests.'

mk B4 ws:B-backend P1 M "$MS1" "" "B1 D2" \
'Extend explanations API: style availability, per-ayah status, ayah ranges and caching headers' \
'The selector needs to know which styles are ready and must never show stale or substituted text.' \
'GET /surahs/:id/explanations?style= filters published only; web requests simplified_ar only. Explanations carry ayah_start/ayah_end.' \
'Data-driven style registry endpoint with per-style readiness for a surah; per-ayah explanation status (available/processing/unavailable); correct handling of multi-ayah explanations; ETag/Cache-Control; no cross-style fallback.' \
'UI; generating content.' \
'backend/internal/content, docs/api/openapi.yaml.' \
'Style availability endpoint per contract|Ranges represented unambiguously|ETag/Cache-Control verified|No fallback across styles|Adding a style requires no code change beyond registry data' \
'Contract tests; G2 leak tests extended.'

mk B5 ws:B-backend P1 M "$MS1" "" "B1 D1" \
'Expose audio metadata and delivery per the ADR decision' \
'Players need availability, size and attribution, and URLs that match the chosen delivery model.' \
'Recitation API lists reciters and per-surah recitations for Surah 12; rights unresolved.' \
'Add audio status, duration/size where known, reciter attribution and rights fields; implement proxy/range support only if D1 chose self-hosting.' \
'Client players; bulk ingestion (F2).' \
'backend/internal/recitation, docs/api/openapi.yaml.' \
'Metadata matches the contract|Unavailable reciters/ayahs reported explicitly|Attribution fields present|Behavior matches the D1 ADR' \
'Contract and handler tests.'

mk B6 ws:B-backend P1 S "$MS1" "" "A2" \
'Measure retrieval scaling of brute-force cosine search before multi-surah ingestion' \
'Retrieval computes cosine in Go over candidate chunks; this may not scale to 114 surahs.' \
'Brute-force cosine; thematicTopK=5; exact ayah mapping is tried first.' \
'Benchmark latency/memory at projected chunk counts; recommend keeping, adding an index or using Atlas Vector Search. Record findings and a trigger threshold.' \
'Implementing a new vector store.' \
'backend/internal/retrieval, docs/tafsir-embeddings.md.' \
'Benchmark results documented|Recommendation and threshold recorded|Follow-up issue opened only if a change is required' \
'Reproducible benchmark script.'

mk A3 ws:A-content P0 L "$MS1" "content-review" "A2 D2" \
'Generate and publish reviewed Surah 12 explanations in simplified_ar, egyptian_ar and en' \
'The UI work needs real published content in all three styles to validate states.' \
'No published explanations exist; make generate cannot write while the source is unverified. Verify which styles the batch job supports.' \
'Run generation per style, review each ayah via the dashboard checklist, publish. Egyptian reviews need a fluent reviewer; en follows the D2 labelling policy.' \
'Other surahs; weakening the checklist.' \
'backend/cmd/generate_explanations, web/app/admin, docs/batch-generation.md.' \
'Pending items generated for each style|Every published item has a review record and citations|Dialect checklist applied by a qualified reviewer|Public API serves the published items per style' \
'make generate-check; verify public API output.' \
'Partial publication is fine; unavailable states cover the rest.'

mk A7 ws:A-content P1 M "$MS1" "" "B1 A4" \
'Add per-surah content-readiness report tooling (text, introduction, styles, audio)' \
'Maintainers and the library need one truthful readiness view.' \
'Coverage checks exist for tafsir; no cross-content readiness report.' \
'A make target emitting JSON and markdown per surah using the same logic as the library readiness states.' \
'Dashboard UI.' \
'backend/cmd (new), Makefile, docs/.' \
'Report covers all four content types|Shares logic with GET /surahs readiness|CI-runnable' \
'Unit tests with fixture data.'

mk E1 ws:E-preferences P1 S "$MS1" "" "D2" \
'Specify local preferences: schema, locale-suggested defaults, manual override' \
'Users choose language/dialect without an account; country must not decide dialect.' \
'No preference storage exists (verify).' \
'Define versioned schema (style id), a suggestion-only locale mapping (e.g. ar-EG may suggest egyptian_ar), rule that explicit user choice always wins and is never overwritten by locale, fallback for unknown/removed styles, and statement that preferences are never sent as a profile (style appears only as a content query parameter).' \
'Implementation (E2, E3).' \
'docs/ (spec), shared fixtures.' \
'Spec merged|Override and fallback rules testable|Privacy statement: no backend profile or accounts' \
'Spec review by web and mobile owners.'

# ===== M2: web experience + pilot content tooling =====
mk G4 ws:G-quality P1 M "$MS2" "" "A3" \
'Build RAG grounding evaluation set and regression harness for explanations' \
'Prompt or model changes can silently degrade grounding or certainty preservation.' \
'Prompt/model versions fingerprinted; validation is structural, not semantic.' \
'Curated evaluation set from reviewed Surah 12 items (citations resolve, no claims beyond evidence, certainty/negation preserved) plus a runner reporting regressions on prompt or model version change.' \
'Automated theological judgment.' \
'backend/internal/generation, new eval dir.' \
'Eval set committed with reviewer-approved expectations|Runner reports per-style metrics|Run required before prompt-version changes (documented)' \
'Harness runs in CI against fixtures.'

mk A6 ws:A-content P1 L "$MS2" "content-review" "A5 A2" \
'Extend generation and review to produce reviewable introductions and Asbab al-Nuzul (pilot: Surah 12)' \
'Introductions must follow the same evidence-only, human-reviewed pipeline.' \
'Task briefs: simplify, translate, adapt-dialect. Review dashboard handles explanations only.' \
'New introduce task brief; retrieval from verified sources; structured output with per-claim refs and a differing-reports field; insufficient evidence yields no output; dashboard queue item type with checklist from D3. Pilot Surah 12 end to end.' \
'Surahs beyond 12; removing review.' \
'backend/internal/generation, web/app/admin, backend/internal/review.' \
'Pipeline runs for Surah 12|Unsupported claims blocked by validation|Review checklist enforced before publish|Published introduction visible via B3' \
'Unit tests; G4 eval extended.'

mk C1 ws:C-web P1 M "$MS2" "" "B1 B2" \
'Web: build surah library page with cards for all 114 surahs' \
'Users must browse and choose any surah and understand what is ready.' \
'Web has /surah/[id] only; no library page was identified (verify).' \
'Grid of cards (name, number, attributed metadata, cover image or fallback, readiness badge); unavailable surahs shown as not ready without implying verified content; keyboard navigation; RTL names; responsive.' \
'Introduction and reading screens.' \
'web/app/ (new library route), web/app/api-client.' \
'All 114 listed|Badges reflect API readiness|Unavailable surahs are not entered as if ready|Works on fixtures before backend completes|Accessible and responsive' \
'Component tests against B1 fixtures.'

mk C2 ws:C-web P1 M "$MS2" "" "B1 B3" \
'Web: surah introduction screen before reading' \
'Readers should understand context before reading ayahs.' \
'Reader starts directly at ayahs.' \
'Introduction screen with classification, context, themes, stories, visible attribution and disputed-report notes; unavailable state that lets the user start reading; Start reading action; shows non-blocking audio preparation status (see F1).' \
'Generating introduction content.' \
'web/app/surah/[id]/, api-client.' \
'Renders published sections with attribution|Unavailable/processing states handled|User can start reading while audio prepares|Never shows unreviewed content' \
'Component tests on fixtures.'

mk C3 ws:C-web P1 M "$MS2" "" "B1" \
'Web: redesign reader as ayah cards with inline explanation' \
'Ayah and explanation must stay together for long reading sessions.' \
'web/app/surah/[id]/page.tsx maps explanations by ayah_start and renders a box labelled with the simplified meaning; audio controls per ayah.' \
'Card per ayah with Uthmani typography, RTL, ayah number, explanation underneath, per-ayah play control and states (loading skeleton, unavailable). Define rendering of multi-ayah explanations. Choose and license an appropriate Quranic font.' \
'Style selector (C4); global player (C5).' \
'web/app/surah/[id]/page.tsx, components.' \
'Each ayah in its own card with explanation beneath|Multi-ayah explanations render correctly|Unavailable explanation distinct from loading|Readable on mobile widths|Font license recorded' \
'Component tests on fixtures; visual check.'

mk E2 ws:E-preferences P1 S "$MS2" "" "E1" \
'Web: persist language/dialect preference locally with a settings control' \
'Preference must survive restarts without accounts.' \
'No preference persistence.' \
'localStorage (guarded with try/catch) following the E1 schema, hydration-safe for SSR, settings control with locale-suggested default and manual override. No cookies or server storage.' \
'Mobile persistence; selector UI (C4).' \
'web/ app shell, settings component.' \
'Survives reload and restart|Explicit choice never overwritten by locale|Storage failures degrade to default|No network call stores preference' \
'Unit and component tests.'

mk C4 ws:C-web P1 L "$MS2" "" "C3 B4 E2 D2" \
'Web: explanation style selector with in-place switching' \
'Style must change without reloading, losing position, or interrupting audio.' \
'Web requests simplified_ar only.' \
'Compact sticky segmented control for simplified_ar, egyptian_ar, en driven by the style registry; per-style fetch with client cache; per-ayah loading/unavailable states; never show the previous styles text under the new label; preserve scroll position and audio playback; persist choice via E2.' \
'Changing Quranic text; regenerating content.' \
'web/app/surah/[id]/, api-client, state/cache layer.' \
'Switching does not reload the page or reset scroll|Audio keeps playing|Stale text never shown under new style|Cached styles reused|Unavailable style shows an explicit state|Selection persisted' \
'Component tests; e2e in G3.'

mk F1 ws:F-audio P1 M "$MS2" "" "D1 B5" \
'Define and implement audio preparation states and non-blocking loading UX' \
'Audio may need preparing; reading must not wait.' \
'Per-ayah audio only, no preparation state model.' \
'Shared state model (idle, preparing with progress where supported, ready, failed with retry, unavailable) per D1; web hook; intro and reading usable throughout.' \
'Offline caching; mobile consumption (see mobile player issue).' \
'web audio hook, docs/ state spec.' \
'State model documented and implemented|Progress shown only when measurable|Retry and error states work|Reading never blocked' \
'Hook tests with simulated slow/failed network.'

mk C5 ws:C-web P1 L "$MS2" "" "C3 B5 F1" \
'Web: persistent audio player' \
'Players should stay visible and survive navigation, style changes and ayah progression.' \
'Per-ayah audio controls; any existing bottom bar to be verified.' \
'Layout-level single audio instance; play/pause, progress/duration, seek, current surah/ayah, previous/next ayah, buffering/error/retry, Media Session API, no overlap with ayah cards. Define behavior for interruption and for navigating between surahs per D1.' \
'Offline/download (D1 follow-up); mobile.' \
'web layout, audio context, components.' \
'Continues through style changes|Defined behavior on surah navigation|Unavailable/interrupted audio shown clearly|Does not obstruct content|Keyboard and screen-reader accessible' \
'Component tests; e2e in G3.'

# ===== M3: mobile, audio at scale, quality =====
mk MA ws:D-mobile P1 S "$MS3" "" "" \
'Mobile: audit current app and document baseline' \
'Mobile was not fully audited and has uncommitted package changes.' \
'Expo app in mobile/; screens/components not inspected; mobile/package*.json modified in audit working tree.' \
'Inventory screens, API usage, navigation, audio library, config (EXPO_PUBLIC_API_URL); resolve or document uncommitted dependency changes; update mobile README.' \
'New features.' \
'mobile/.' \
'Baseline doc merged|Uncommitted changes resolved or explained|Gaps vs this product vision listed' \
'Run app on emulator and device.'

mk ML ws:D-mobile P1 M "$MS3" "" "MA B2" \
'Mobile: surah library and navigation' \
'Parity with the web library.' \
'To be established by the baseline audit.' \
'Library screen with cards and readiness states using the B1 contract; navigation to introduction/reading.' \
'Reading screens.' \
'mobile/.' \
'All 114 shown with readiness|Unavailable not entered as ready|Works on fixtures' \
'Component tests; device check.'

mk E3 ws:E-preferences P1 S "$MS3" "" "E1" \
'Mobile: persist language/dialect preference locally with settings screen' \
'Same preference behavior as web.' \
'No persistence.' \
'AsyncStorage (or equivalent) following E1; settings screen; locale suggestion with manual override.' \
'Server storage.' \
'mobile/.' \
'Survives app restart|Manual override honored|No backend profile' \
'Unit tests; restart test.'

mk MR ws:D-mobile P1 L "$MS3" "" "ML B3 B4 E3" \
'Mobile: introduction screen, ayah cards and style selector' \
'Mobile reading experience matching web behavior.' \
'To be established by the baseline audit.' \
'Introduction with attribution, ayah cards with inline explanation, in-place style switching with caching and unavailable states.' \
'Audio player.' \
'mobile/.' \
'Same states as web|Style switching preserves position|No stale-style text|Preference persisted' \
'Component tests; manual device QA.' \
'May be split after the baseline audit.'

mk MP ws:D-mobile P1 L "$MS3" "" "MR B5 F1" \
'Mobile: audio player with buffering/error states and background playback' \
'Long reading sessions need reliable playback.' \
'Audio library and background capability not yet verified.' \
'Persistent player, progress/seek, previous/next ayah, buffering/error/retry, background playback and lock-screen controls where supported.' \
'Offline cache unless chosen by D1.' \
'mobile/ (audio module, app config).' \
'Playback continues through style changes|Defined interruption and surah-navigation behavior|iOS and Android background behavior verified' \
'Device testing on both platforms.'

mk A10 ws:A-content P2 M "$MS3" "needs-decision" "B2" \
'Create surah card art system with licensed or original assets' \
'Each surah needs a visual card without licensing risk.' \
'No card art exists.' \
'Decide approach (e.g. generated geometric/calligraphic patterns vs commissioned art), record licensing, avoid figurative imagery as a design recommendation to be confirmed, build asset pipeline and a safe fallback.' \
'UI cards (C1).' \
'web/public or assets store, docs/.' \
'Approach and licensing recorded|Assets exist for all 114 or a deterministic generator does|Fallback when missing' \
'Visual review.'

mk F2 ws:F-audio P1 L "$MS3" "" "D1 A4" \
'Verify recitation licensing and generalize audio ingestion beyond Surah 12' \
'Audio for 114 surahs needs cleared rights and parameterized tooling.' \
'444 files for Surah 12; EveryAyah hotlinking rights unresolved; audio-check gates Surah 12.' \
'Record rights decision per reciter; add attribution fields; parameterize ingest_recitations and audio-check per surah; hold publication for any reciter without cleared rights; follow D1 on storage.' \
'Bulk automatic download into production.' \
'backend/cmd/ingest_recitations, docs/recitation-audio-sources.md.' \
'Rights decision recorded per reciter|Tooling works per surah|Uncleared audio not published|Attribution surfaced via API' \
'make audio-check per surah.'

mk G3 ws:G-quality P1 L "$MS3" "" "C4 C5" \
'Web e2e tests for the reading flow (Playwright)' \
'Cross-component behavior is where regressions occur.' \
'No e2e tests identified.' \
'Cover library to introduction to reading, style switching in place, audio continuity across style changes, unavailable states, preference persistence.' \
'Visual regression suites.' \
'web/ e2e config.' \
'Flows pass in CI|Audio continuity asserted|Unavailable states asserted' \
'CI Playwright run.'

mk G5 ws:G-quality P1 M "$MS3" "" "C4 MR" \
'Accessibility, RTL, responsive and performance audit (web and mobile)' \
'Long reading sessions require legibility and accessibility.' \
'Not audited.' \
'Audit screen-reader labels, focus order, contrast, RTL layout, large text, performance on mid-range devices; file or fix findings.' \
'New features.' \
'web/, mobile/.' \
'Audit report with findings|Blocking issues fixed|Non-blocking issues tracked' \
'Manual and automated checks (axe, Lighthouse).'

# ===== M4: scale-up =====
mk A8 ws:A-content P1 M "$MS4" "content-review" "A3 A6 A7 B6" \
'Define and tool the scale-out workflow for surahs 2-114' \
'Moving beyond Surah 12 needs a repeatable, reviewable process.' \
'Only Surah 12 tafsir is committed; one shared reviewer secret; Ibn Kathir verified status pending.' \
'Per-surah pipeline stages, ordering, batch/LLM quota and cost, reviewer capacity, per-surah definition of ready, source availability per surah, and handling surahs lacking verified sources (they remain unavailable).' \
'Generating all surahs.' \
'docs/, Makefile, backend/cmd.' \
'Workflow documented|Stage status visible via A7 readiness|Sources missing means unavailable, never filled|First batch (surahs) selected' \
'Dry-run the workflow on one additional surah.'

mk A9 ws:A-content P2 L "$MS4" "" "A8" \
'Replace shared reviewer password with per-reviewer identity, expiry and revocation' \
'Multiple reviewers need individual accountability.' \
'Shared X-Reviewer-Password, constant-time compare, fails closed; reviewer_id passed in request.' \
'Design and implement per-reviewer authentication with audit identity, rotation and revocation; preserve fail-closed behavior.' \
'Reader accounts.' \
'backend/internal/platform/auth.go, web/app/admin.' \
'Individual credentials|Revocation works|Audit records bind to authenticated identity|Fails closed' \
'Auth tests.'

# ===== M5: release =====
mk G6 ws:G-quality P2 L "$MS5" "" "" \
'Production readiness: deployment plan, CORS, rate limiting, secrets, backups' \
'No deployment manifests or hardening were evident.' \
'Only Mongo in docker-compose; Gin defaults; CORS and rate limiting not verified.' \
'Hosting and MongoDB (Atlas vs self-host) decision, env/secrets handling, CORS policy, rate limiting on public routes, backups, rollback, log hygiene.' \
'Feature work.' \
'docs/, backend/Dockerfile, API setup.' \
'Plan documented|CORS and rate limits implemented|Backups tested|No secrets in logs' \
'Staging deployment dry run.'

mk G7 ws:G-quality P1 S "$MS5" "" "G2 F2 A8 G3 G5 G6" \
'Release readiness checklist and go/no-go gate' \
'Release must prove safety, legal and content readiness.' \
'No release criteria defined.' \
'Checklist covering: only reviewed content visible, sources verified, audio rights cleared, attribution displayed, accessibility, e2e green, deployment ready.' \
'New features.' \
'docs/.' \
'Checklist merged|Each item has an owner|Go/no-go recorded' \
'Walk the checklist on staging.'

# ---------- 6. Epic child lists and sub-issue links ----------
if [ "$DRY_RUN" != 1 ]; then
  for ws in "${!EPIC[@]}"; do
    n="${EPIC[$ws]}"; [ -n "$n" ] || continue
    [ -n "${REUSED_FLAG[$ws]:-}" ] && { echo "epic #$n reused; add children manually"; continue; }
    gh issue edit "$n" --repo "$REPO" --body "$(gh issue view "$n" --repo "$REPO" --json body -q .body | sed '/^_Child issues/,$d')"$'\n\n### Child issues\n'"${CHILDREN[$ws]:-}" >/dev/null
  done
  for k in "${!NUM[@]}"; do
    case "$k" in ws:*) continue;; esac
    [ -n "${REUSED_FLAG[$k]:-}" ] && continue
    ws="$(gh issue view "${NUM[$k]}" --repo "$REPO" --json labels -q '.labels[].name' | grep '^ws:' | head -1)"
    e="${EPIC[$ws]:-}"; [ -n "$e" ] || continue
    id="$(gh api "repos/$REPO/issues/${NUM[$k]}" --jq .id)"
    gh api -X POST "repos/$REPO/issues/$e/sub_issues" -F sub_issue_id="$id" >/dev/null 2>&1 || true
  done
fi

echo
echo "Repository: $REPO"
echo "Created: $CREATED   Reused (exact title match): $REUSED   DRY_RUN=$DRY_RUN"
[ "$DRY_RUN" = 1 ] && echo "Dry run only. Re-run with DRY_RUN=0 after reviewing the plan."
