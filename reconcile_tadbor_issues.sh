#!/usr/bin/env bash
# Tadbor: reconcile existing GitHub issues with the 2026-10-25 audit and the internal-MVP
# roadmap, and create only the genuinely new issues.
#
#   cd /path/to/tadbor
#   DRY_RUN=1 ./reconcile_tadbor_issues.sh   # DEFAULT. Read-only: validates and prints the full plan.
#   DRY_RUN=0 ./reconcile_tadbor_issues.sh   # Applies the plan (asks you to type the repo name).
#
# Structure: (1) preflight and read-only validation of EVERYTHING, (2) plan display,
# (3) apply, only when DRY_RUN=0 and validation found no errors. Nothing is written
# while validating, so a validation failure never leaves a half-applied plan.
#
# Dependency model (see the DEPENDENCY DECISIONS block near the data section):
#   - "GitHub links" = real blocked-by links created through the GitHub issue-dependencies API.
#   - "Referenced"   = plain-text references in comments/bodies. Never created as links.
#
# Optional env: REPO=owner/name  CONFIRM_REPO=owner/name (non-interactive apply)
#               ALLOW_NO_DEP_LINKS=1 (continue if the dependencies API is unavailable)
#               BACKUP_DIR=./reconcile-backups (where #32's pre-edit copy is saved)
# Requires: bash >= 4, gh (authenticated), jq, diff.
set -euo pipefail

die() { echo "ERROR: $*" >&2; exit 1; }
[ "${BASH_VERSINFO[0]}" -ge 4 ] || die "bash >= 4 required (macOS: brew install bash)"
for c in gh jq diff; do command -v "$c" >/dev/null || die "$c is required"; done

DRY_RUN="${DRY_RUN:-1}"
case "$DRY_RUN" in 0|1) ;; *) die "DRY_RUN must be 0 or 1 (got '$DRY_RUN')";; esac
ALLOW_NO_DEP_LINKS="${ALLOW_NO_DEP_LINKS:-0}"
BACKUP_DIR="${BACKUP_DIR:-./reconcile-backups}"
MARK='<!-- tadbor-reconcile-2026-10-25 -->'
RESCOPE_MARK='<!-- tadbor-rescope-2026-10-25 -->'

WORK="$(mktemp -d)"; trap 'rm -rf "$WORK"' EXIT
ERRORS=(); WARNINGS=()
err()  { ERRORS+=("$*"); }
warn() { WARNINGS+=("$*"); }
indent() { sed 's/^/    | /'; }

# ============================ 1. PREFLIGHT (read-only) ============================
echo "== Preflight (read-only) =="
gh auth status >/dev/null 2>&1 || die "gh is not authenticated (run: gh auth login)"
if [ -z "${REPO:-}" ]; then
  REPO="$(gh repo view --json nameWithOwner -q .nameWithOwner)" || die "cannot determine the repository from the git remote"
fi
ORIGIN="$(git remote get-url origin 2>/dev/null || true)"
if [ -n "$ORIGIN" ] && ! grep -qiF -- "$REPO" <<<"$ORIGIN"; then
  warn "REPO '$REPO' does not match git remote origin '$ORIGIN'"
fi
echo "Repository: $REPO   DRY_RUN=$DRY_RUN"

gh issue list --repo "$REPO" --state all --limit 1000 --json number,title,state > "$WORK/issues.json" \
  || die "cannot list issues"
jq empty "$WORK/issues.json" || die "issue list is not valid JSON"
ISSUE_COUNT="$(jq length "$WORK/issues.json")"
[ "$ISSUE_COUNT" -lt 1000 ] || die "1000+ issues: listing may be truncated, cannot dedupe safely"
gh label list --repo "$REPO" --limit 500 --json name -q '.[].name' > "$WORK/labels.txt" || die "cannot list labels"
gh api "repos/$REPO/milestones?state=all&per_page=100" --jq '.[].title' > "$WORK/milestones.txt" || die "cannot list milestones"
echo "Existing issues: $ISSUE_COUNT; highest number: $(jq '[.[].number] | max // 0' "$WORK/issues.json")"

existing_number() { jq -r --arg t "$1" '[.[] | select(.title == $t)][0].number // empty' "$WORK/issues.json"; }
existing_state()  { jq -r --arg n "$1" '[.[] | select(.number == ($n|tonumber))][0].state // empty' "$WORK/issues.json"; }
has_line()        { grep -Fxq -- "$2" "$1"; }

# view_issue N -> 0 ok (JSON in $WORK/view_N.json) | 1 not found | exits on any other gh error
view_issue() {
  local n="$1"
  if gh issue view "$n" --repo "$REPO" --json number,state,title,body,updatedAt,comments \
        > "$WORK/view_$n.json" 2> "$WORK/gh.err"; then return 0; fi
  if grep -qiE 'could not resolve|not found|HTTP 404' "$WORK/gh.err"; then return 1; fi
  die "unexpected gh error while reading #$n: $(cat "$WORK/gh.err")"
}

title_matches() { # title, alternatives separated by '|' (fixed strings, case-insensitive)
  local title="${1,,}" a; local -a alts
  IFS='|' read -ra alts <<<"$2"
  for a in "${alts[@]}"; do
    case "$title" in *"${a,,}"*) return 0;; esac
  done
  return 1
}

# ============================ 2. PLAN REGISTRY ============================
declare -A NUM N_SEEN N_TITLE N_PRI N_EFF N_LABELS N_MS N_PLAN N_LBL_OK N_LBL_MISSING N_MS_OK
declare -A R_FRAG R_ACT R_PLAN R_STATE R_TITLE
declare -A RS_FRAG RS_TITLE RS_PLAN RS_UPDATED RS_TITLE_OLD
NEW_KEYS=(); REC_N=(); RS_N=(); LINKS=()

# new KEY PRI EFF "title" "labels" "milestone"   (body on stdin)
new() {
  local key="$1"
  [ -z "${N_SEEN[$key]:-}" ] || die "duplicate new-issue key $key"
  N_SEEN[$key]=1; NEW_KEYS+=("$key")
  N_PRI[$key]="$2"; N_EFF[$key]="$3"; N_TITLE[$key]="$4"; N_LABELS[$key]="$5"; N_MS[$key]="$6"
  cat > "$WORK/raw_$key.md"
}
# rec NUMBER "title alternatives a|b" ACTION(reopen|comment)   (comment body on stdin)
rec() {
  local n="$1"
  [ -z "${R_ACT[$n]:-}" ] || die "duplicate rec for #$n"
  REC_N+=("$n"); R_FRAG[$n]="$2"; R_ACT[$n]="$3"
  case "$3" in reopen|comment) ;; *) die "bad action '$3' for #$n";; esac
  cat > "$WORK/rec_$n.md"
}
# rescope NUMBER "title alternatives (old|new)" "new title"   (new scope section on stdin)
rescope() {
  local n="$1"
  RS_N+=("$n"); RS_FRAG[$n]="$2"; RS_TITLE[$n]="$3"
  cat > "$WORK/rescope_$n.md"
}
# link BLOCKED BLOCKER   (each is an issue number or a new-issue KEY): a REAL GitHub blocked-by link
link() { LINKS+=("$1 $2"); }

is_num()    { [[ "$1" =~ ^[0-9]+$ ]]; }
tok_known() { is_num "$1" || [ -n "${N_SEEN[$1]:-}" ]; }
tok_num()   { if is_num "$1"; then echo "$1"; else echo "${NUM[$1]:-}"; fi; }
tok_ref()   { local n; n="$(tok_num "$1")"; if [ -n "$n" ]; then echo "#$n"; else echo "{{$1}}"; fi; }
blockers_of() { local l; for l in "${LINKS[@]}"; do if [ "${l%% *}" = "$1" ]; then echo "${l#* }"; fi; done; }
blocks_of()   { local l; for l in "${LINKS[@]}"; do if [ "${l#* }" = "$1" ]; then echo "${l%% *}"; fi; done; }

subst() { # replace {{KEY}} with #number where the number is already known
  local t k; t="$(cat)"
  for k in "${NEW_KEYS[@]}"; do
    if [ -n "${NUM[$k]:-}" ]; then t="${t//\{\{$k\}\}/#${NUM[$k]}}"; fi
  done
  printf '%s' "$t"
}

render_new_body() { # key
  local key="$1" b first=1
  subst < "$WORK/raw_$key.md"; echo; echo
  echo "### Dependencies (GitHub blocked-by links)"
  for b in $(blockers_of "$key"); do echo "- Blocked by $(tok_ref "$b")"; first=0; done
  if [ "$first" = 1 ]; then echo "None. (Any related issues are listed under Related, as plain references.)"; fi
  echo; echo "### Priority and Effort"; echo "Priority: ${N_PRI[$key]} | Effort: ${N_EFF[$key]}"
}

render_comment() { # n
  local n="$1" b any=0
  echo "$MARK"
  echo "**Reconciliation 2026-10-25** (source: PROJECT_STATUS.md audit and internal-MVP roadmap; GitHub issue state is not evidence that acceptance criteria are met)."
  echo; subst < "$WORK/rec_$n.md"; echo; echo
  echo "**GitHub blocked-by links set on this issue by this reconciliation:**"
  for b in $(blockers_of "$n"); do echo "- blocked by $(tok_ref "$b")"; any=1; done
  if [ "$any" = 0 ]; then echo "- none"; fi
  any=0
  echo "**Issues that this issue blocks (GitHub links):**"
  for b in $(blocks_of "$n"); do echo "- $(tok_ref "$b")"; any=1; done
  if [ "$any" = 0 ]; then echo "- none"; fi
}

# ============================ 3. DATA ============================
MS0="M0 - Decisions and contracts"
MS1="M1 - Content foundation and backend APIs"
MS2="M2 - Core web reading experience"
MS3="M3 - Mobile, audio at scale, quality"

# ---- DEPENDENCY DECISIONS (why each direction was chosen) ----
# Contract-first: #29 (spec + fixtures + CI validation) blocks #32, #34, #36, because those
#   issues must conform to a shared contract and the original plan said the contract is agreed
#   first. #29 itself is blocked by nothing open. Handler conformance to the spec is checked by
#   each implementing issue's own tests, so #29 does NOT wait for #34/#36 (removes the old cycle
#   #29 -> #34/#36 -> #29).
# #34 and #32 are NOT linked. #34 works on whatever metadata exists and reports the rest as
#   unavailable; #32 widens the data. Linking either way would invent a prerequisite.
# #28 <- #27 (needs the Mongo CI lane). #35 <- #27, #28: the #28 harness lands first and the
#   introduction route is added to it by #35's own PR (removes the old cycle #28 <-> #35).
# #43 <- #27, #35, NEXP (drafts need the merged introduction code and the labelled boundary).
# NEXP <- #27, #28, #35. NFIX <- #27.
# Plain references only (no GitHub link): #25, #26 (a closed policy issue whose named approver is
#   a project decision), #33, #24, #30, NFIX/#28 shared fixtures, N4/#36 style IDs.
# GitHub links to build (blocked <- blocker):
link 28 27
link 35 27
link 35 28
link 43 27
link 43 35
link 43 NEXP
link 32 29
link 34 29
link 36 29
link NFIX 27
link NEXP 27
link NEXP 28
link NEXP 35

echo "(registering plan data...)"

# ---------------- New issues ----------------
new N3 P0 S "Return ayahs in canonical order and prove it with shuffled-insertion tests" "ws:B-backend" "$MS1" <<'EOF'
### Context
Web and mobile render ayahs in the order the API returns them. The backend query has no explicit sort, so ayah order currently depends on MongoDB natural order. Wrong Quranic order is a correctness defect, not a cosmetic one.

### Current State
Per the 2026-10-25 audit, `GET /surahs/:surahId/ayahs` queries by surah id without an explicit sort. Verify in `backend/internal/quran/service.go` before starting.

### Scope
- Sort by ayah number in the query (and add a supporting index if needed), so the guarantee lives at the service boundary.
- Check the recitations and explanations list queries for the same assumption and record the result in the PR.

### Out of Scope
Pagination, new endpoints, client-side sorting as a workaround.

### Implementation Notes
`backend/internal/quran/service.go`, `backend/internal/quran/handlers.go`, Mongo index creation code. Clients keep trusting API order, so the fix belongs on the server.

### Related (reference only, not a GitHub link)
The Mongo integration test runs in CI once #27 lands; the fix and the unit test do not wait for it.

### Acceptance Criteria
- [ ] Ayah query sorts by ayah number ascending
- [ ] Unit test asserts order 1..N
- [ ] Mongo integration test inserts ayahs in shuffled order and still receives 1..N
- [ ] Any other list query that clients render in order is either sorted or documented as unordered

### Testing
`go test ./... -count=1`, plus the Mongo integration lane (must run, not skip).

### Definition of Done
All criteria checked; tests pass in CI; no change to Quranic text content.
EOF

new N4 P0 M "Distinguish API failures from empty and unavailable states in web and mobile clients" "ws:C-web ws:D-mobile" "$MS2" <<'EOF'
### Context
An outage or malformed response must never look like a surah with no content.

### Current State
Per the audit: the web API client turns non-list or failed responses into empty arrays and does not reject non-2xx responses; the mobile client parses JSON without checking HTTP status or response shape. Verify in `web/app/api-client/index.ts` and `mobile/src/api/client.ts`.

### Scope
- A typed client result that separates: loading, available, empty, unavailable (for example 404 for an unseeded surah, 202 processing for introductions), and error (network, 5xx, invalid JSON).
- Visible error UI with a retry control on both clients.

### Out of Scope
New screens, the style selector, caching strategy.

### Implementation Notes
Keep the existing runtime type guards. Map status codes to states in one place per client. Do not log response bodies that could contain internal details.

### Related (reference only, not a GitHub link)
#27 (so mobile type errors in this change fail CI) and #36 (canonical style IDs). Neither blocks starting this work.

### Acceptance Criteria
- [ ] Non-2xx responses and invalid JSON produce an error state, never an empty list
- [ ] 404 for an unseeded surah renders as unavailable; 200 with no items renders as empty; the two are visibly different
- [ ] Retry works after an error
- [ ] Tests with mocked 404, 500, malformed JSON and empty 200 on web and mobile

### Testing
Client unit tests with mocked fetch; manual check by stopping the API.

### Definition of Done
All criteria checked; tests in CI; no screen shows an outage as successful empty content.
EOF

new NFIX P0 M "Create isolated synthetic MVP fixture dataset and backend journey test" "ws:G-quality" "$MS1" <<'EOF'
### Context
The internal MVP journey needs a repeatable dataset that does not depend on unreviewed or unverified religious content, and must run only against a disposable database.

### Current State
No Mongo URI was configured during the audit; Mongo integration tests skip. No fixture dataset covers surah metadata, ordered ayahs, explanations in each supported style, and recitations together.

### Scope
- A fixture seeder that refuses to run unless it targets an explicitly disposable database (for example only via `MONGO_TEST_URI`, with a database-name guard).
- Synthetic placeholder text clearly labelled as fixture (for example "FIXTURE ayah N"), not presented as Quranic text or as reviewed explanation content.
- Records covering: one seeded surah and one unseeded id, ayahs inserted in shuffled order, published explanations for `simplified_ar`, `egyptian_ar`, `en`, a pending and a rejected explanation that must never appear publicly, recitation metadata for at least two reciters.
- One backend journey test: surah metadata, ordered ayahs, supported-style explanations, recitations.

### Out of Scope
Changing source verification state, creating review records for real content, running LLM or audio jobs, production data.

### Implementation Notes
`backend/cmd` (new seeder) or test helpers. Fixture records carry an unmistakable marker so no public path can mistake them for approved content outside a test database.

### Related (reference only, not a GitHub link)
#29 (fixture shapes) and #28 (pending/rejected exposure fixtures): reuse one shared helper; whichever issue lands first owns it.

### Acceptance Criteria
- [ ] Seeder aborts against a non-disposable target
- [ ] Fixture data is visibly labelled and non-Quranic
- [ ] Journey test passes against the fixture on a real Mongo
- [ ] Pending and rejected fixtures never appear in public responses
- [ ] No registry flag or review record for real content is touched

### Testing
Journey test in the Mongo integration lane; a negative test for the target guard.

### Definition of Done
All criteria checked; runs in CI with Mongo; fixture usage documented in the README.
EOF

new NEXP P1 L "Design and implement an internal-only experimental content mode with mandatory labelling" "ws:B-backend needs-decision" "$MS2" <<'EOF'
### Context
Internal testers need to see generated, unreviewed drafts in the reader. Public endpoints must keep serving published content only, so this needs a separate, explicitly bounded path.

### Current State
Public routes filter to published content. Introduction and explanation drafts are stored as pending. No internal-only reader path or labelling exists. The introduction approval HTTP route intentionally returns 503.

### Scope
- First, a short ADR: how the mode is enabled (separate route or environment), the access boundary, and why it cannot leak into public serving.
- Implementation per the ADR: drafts are served only through that boundary, every response carries an explicit experimental status, and web and mobile show a non-dismissable "Experimental / not reviewed" label.
- Disabled by default; configuration that enables it is rejected in production-like environments.
- No promotion path from experimental to published.

### Out of Scope
Approval or publication of any content, reviewer identity (separate issue), public exposure of drafts.

### Implementation Notes
`backend/internal/content`, `backend/internal/introduction`, `backend/internal/api`, web and mobile reader components. Reuse the exposure-test harness to prove the boundary.

### Related (reference only, not a GitHub link)
The fixture dataset issue (synthetic drafts for tests) and the introduction pipeline issue #43 (source of real drafts).

### Acceptance Criteria
- [ ] ADR merged and approved by a maintainer
- [ ] Public endpoints never return pending, rejected or experimental records (regression test)
- [ ] Experimental responses are explicitly marked, and clients show the label on every experimental item
- [ ] Mode is off by default and cannot be enabled in production configuration
- [ ] No code path sets a draft to published

### Testing
Exposure tests on real Mongo; client tests asserting the label; a configuration test for the production guard.

### Definition of Done
All criteria checked; tests in CI; labelling visible in manual review on web and mobile.
EOF

new NREV P2 L "Implement trusted reviewer identity and API-enforced review checklist (approval stays disabled until done)" "ws:A-content content-review needs-decision" "$MS3" <<'EOF'
### Context
Approval of religious content needs an attributable, qualified reviewer. The current login is a shared password plus a caller-typed name, which is neither identity nor qualification. Introduction approval therefore fails closed with 503, which is correct and must stay until this is done.

### Current State
Per the audit: shared secret in `X-Reviewer-Password`; the admin cookie stores the raw shared password; the checklist is enforced mainly in the dashboard UI, so direct API calls can bypass it; reviewer_id comes from the request.

### Scope
- Decision: identity provider and the mapping from identities to reviewer qualifications (per content type and per dialect).
- Reviewer identity derived server-side for audit records; no free-text identity.
- Checklist, citation-resolution and grounding-version checks enforced at the API boundary for explanations and introductions.
- Session handling that no longer stores the raw shared secret; expiry and revocation.
- Re-enable introduction decisions only after all of the above.

### Out of Scope
Reader accounts, assigning real reviewers (a project decision), automated theological judgment.

### Implementation Notes
`backend/internal/platform/auth.go`, `backend/internal/review`, `web/app/admin`. The direct service approval method must not become reachable until the HTTP boundary enforces identity and checklist.

### Related (reference only, not a GitHub link)
#26: its policy names the approver as "to be assigned". Naming a qualified approver is a project decision, not a GitHub dependency. #35 and #43 keep approval disabled until this is done.

### Acceptance Criteria
- [ ] Identity provider and qualification mapping documented and approved
- [ ] Audit identity comes from the authenticated session only
- [ ] API rejects approval without the full checklist, with unresolved citations, or with a grounding-version mismatch (tests call the API directly)
- [ ] Spoofed names and unauthorized users are rejected (tests)
- [ ] Revocation and expiry work
- [ ] Introduction decision route stays 503 until these criteria pass

### Testing
API-level tests with no UI; Mongo integration tests for transitions; spoofing and authorization negative tests.

### Definition of Done
All criteria checked; tests in CI; policy owner sign-off recorded.
EOF

# ---------------- #32: explicit title/body re-scope (original text preserved verbatim) ----------------
rescope 32 "Seed verified Quran|Seed Quran|MVP: bounded dataset" "Seed verified Quran text and surah metadata (MVP: bounded dataset first; all 114 surahs staged)" <<'EOF'
## MVP re-scope (2026-10-25)
**Why:** the audit found only Surah 12 seeded and no surah-metadata data contract, while the internal MVP needs library data rather than all 114 surahs. The original text below is kept in full; nothing in it is dropped.

### Stage 1 - MVP gate
- [ ] Surah metadata shape follows the contract defined in #29 (number, Arabic name, transliteration, ayah count)
- [ ] A deliberately bounded dataset is seeded to that shape
- [ ] Every metadata field has a named source; revelation classification is omitted or flagged unknown unless a named source supports it, and disputed values are flagged. Nothing is invented.
- [ ] Seeding is idempotent, and `seed-check` covers the bounded dataset with a second-source cross-check
- [ ] Unseeded surahs are reported as unavailable, not as errors

### Stage 2 - not an MVP gate
All original acceptance criteria below (all 114 surahs, verified text, attributed metadata, disputed classifications flagged) are retained unchanged. If maintainers prefer, split Stage 2 into its own issue when Stage 1 closes.

### Dependency direction
GitHub blocked-by link: #29 (the metadata contract is defined there first). Referenced only, not linked: #26 (policy for attributed classification; Stage 1 omits classification unless sourced, so it does not block) and #34 (the library endpoint works with whatever data exists and does not wait for this issue).
EOF

# ---------------- Reconciliation comments ----------------
rec 27 "Establish CI" reopen <<'EOF'
Reopening: the audit found mobile typecheck failing at `mobile/App.tsx:9` (`StatusBar` has no `backgroundColor`) while CI masks it with `|| true`, and Mongo integration tests skip because CI has no Mongo.

Proposed acceptance criteria:
- [ ] Mobile type error fixed and `|| true` removed from the mobile typecheck step
- [ ] CI provides an isolated Mongo service and sets `MONGO_TEST_URI`
- [ ] Integration tests fail rather than skip when running in CI
- [ ] `go test ./... -count=1`, `go build ./...`, `cd web && npm run build`, `cd mobile && npx tsc --noEmit` all run in CI
- [ ] Verification: a deliberately introduced mobile type error fails the build

CI must never point at production data.
EOF

rec 28 "regression tests" reopen <<'EOF'
Reopening: the audit found no router-enumeration test, and Mongo-backed exposure tests were skipped.

Proposed acceptance criteria:
- [ ] Every registered public route has an exposure test, enforced by a router-enumeration test that fails when a route has none
- [ ] Pending, rejected, escalated and published fixtures run against each public route on real Mongo (not skipped)
- [ ] No `review_status`, source snapshot, prompt/model version or internal timestamp leaks
- [ ] Verification: adding a public route without a test fails CI

Direction note: this harness lands first. The introduction route is added to it by #35's own pull request, so this issue does not wait for #35.

Referenced (no link): the fixture dataset issue {{NFIX}} shares the pending/rejected fixtures; reuse one helper.
EOF

rec 29 "shared content-readiness" reopen <<'EOF'
Reopening and re-scoping to the internal-MVP journey (the full readiness matrix can be staged later). The audit found fixtures and OpenAPI covering introductions only, and OpenAPI stale for several real routes.

Direction: this is the contract-first issue. It blocks #32, #34 and #36 and is blocked by nothing open. It does not wait for them: handler conformance is verified by each implementing issue's own tests.

Proposed acceptance criteria:
- [ ] OpenAPI documents the existing public routes and defines the planned `GET /surahs` response, including the surah-metadata fields that #32 seeds to
- [ ] Style availability and status semantics are defined; 200 empty vs 202 processing vs 404 unavailable vs 5xx error are documented
- [ ] Fixtures exist for each state the MVP journey uses
- [ ] Spec and fixtures are validated in CI
- [ ] Spec states that a missing style never falls back to another style
- [ ] Spec states that #34, #36 and #35 must add handler-conformance tests against it

Referenced (no link): {{NFIX}} (shared fixture shapes) and #25 (style policy, closed).
EOF

rec 32 "Seed verified Quran|Seed Quran|MVP: bounded dataset" reopen <<'EOF'
Reopening. The title and body of this issue are re-scoped in a separate edit made by this reconciliation, with the original text preserved verbatim under "Original issue text". Summary of the new Stage 1 (MVP gate):
- [ ] Metadata follows the contract from #29
- [ ] Bounded dataset seeded; every field has a named source; classification omitted or flagged unknown unless sourced; nothing invented
- [ ] Idempotent seeding and `seed-check` with a second-source cross-check
- [ ] Unseeded surahs reported as unavailable, not errors

Stage 2 (all 114 surahs, original criteria) is retained and is not an MVP gate.

Referenced (no link): #26 (policy for attributed classification; not blocking because Stage 1 omits unsourced classification) and #34 (works without waiting for this issue).
EOF

rec 34 "surah library endpoint" reopen <<'EOF'
Reopening: the audit found no `GET /surahs` route registered, and both clients are hardcoded to Surah 12.

Proposed acceptance criteria:
- [ ] `GET /surahs` registered, data-derived, sorted by surah number, and documented in OpenAPI
- [ ] Unseeded or unavailable content is reported as unavailable, not as an error
- [ ] Cache headers set
- [ ] Handler tests with one seeded and one unseeded surah
- [ ] Handler responses are validated against the #29 contract in CI
- [ ] Web and mobile selectors consume it (tracked in #19 and #20)

Referenced (no link): #32 widens the dataset; this endpoint works with whatever metadata exists and does not wait for it.
EOF

rec 35 "surah introduction API" reopen <<'EOF'
Reopening: the introduction implementation exists only in uncommitted local changes, and its Mongo integration tests were skipped.

Proposed acceptance criteria:
- [ ] Code merged to `main` through a reviewed pull request
- [ ] Mongo integration tests run in CI (not skipped)
- [ ] The introduction route is added to the #28 exposure harness in this pull request; no internal fields leak
- [ ] Error responses do not echo internal error text
- [ ] Handler responses validated against the #29 contract

Out of scope: a public introduction reader UI (tracked in #19) and approval/publication.
EOF

rec 36 "Extend explanations API" reopen <<'EOF'
Reopening and re-scoping to what the MVP needs now; ETag and availability endpoints can be staged. The audit found web requesting only `simplified_ar` and mobile sending `egyptian`/`english`, which do not match backend IDs `egyptian_ar` and `en`. Where this comment conflicts with the original issue body, this comment is the current scope; the body itself was not edited.

Proposed acceptance criteria:
- [ ] One canonical style ID list (`simplified_ar`, `egyptian_ar`, `en`) used by backend, web and mobile
- [ ] Explanation ranges (`ayah_start`/`ayah_end`) represented unambiguously in the contract
- [ ] Test proving a missing style never falls back to another style
- [ ] Handler responses validated against the #29 contract in CI
- [ ] Staged follow-up (not an MVP gate): availability endpoint and ETag/Cache-Control

Referenced (no link): #25 (style policy, closed) and {{N4}} (client error vs unavailable states).
EOF

rec 37 "audio metadata" comment <<'EOF'
Not reopening yet. Reopen only if the MVP needs fields the API lacks: audio status and attribution per reciter. The rights decision remains separate and unresolved (EveryAyah usage rights are not established); external-URL playback is internal-testing only until it is.

If reopened, acceptance criteria: status and attribution fields in the contract, unavailable audio reported explicitly, no downloads or redistribution.

Referenced (no link): #24 (ADR) and #29 (contract).
EOF

rec 43 "Extend generation and review" reopen <<'EOF'
Reopening and re-scoping to an internal-only experimental pipeline. Approval and publication are deliberately out of scope because the registered approval route returns 503 until trusted reviewer identity exists ({{NREV}}). The pipeline is also only in uncommitted local changes, and its Mongo tests were skipped.

Proposed acceptance criteria:
- [ ] Pipeline merged through a reviewed pull request
- [ ] Preview (default) never calls the model or writes; `-live` writes only pending/experimental drafts
- [ ] Tested with a deterministic generator against a disposable Mongo (CI lane from #27)
- [ ] No code path transitions a draft to published; clients label drafts "Experimental / not reviewed" ({{NEXP}})
- [ ] Context size for Surah 12 measured and a limit documented
- [ ] Docs state that citation resolution is not entailment and every claim needs human review
- [ ] Deferred explicitly: specialist approval, publication, "published via #35"

Referenced (no link): #33 (data model, closed; the model currently exists in local changes), {{NREV}} (identity, needed for approval) and #30 (source reconciliation, needed before any approved content).
EOF

rec 30 "Complete human verification" comment <<'EOF'
Not reopening, but flagging an unresolved conflict: the registry JSON says `verified`, while the review sheet describes an automated-pilot review, states the printed edition cannot be reproduced, and lists 19 entries still needing human review. The README also says unverified.

Until a qualified content owner reconciles these, do not treat this source as approved evidence, do not change the registry flag, and do not create reviewer records on anyone's behalf. Internal testing should use the synthetic fixtures ({{NFIX}}). Reopen if a qualified reviewer disputes the closure.
EOF

rec 39 "Generate and publish" comment <<'EOF'
Not reopening, but flagging: the closure note says all three styles were published while also reporting that review records were missing, and current database contents were not inspected in the audit.

Before any published explanation is relied on, an authorized, non-production procedure should audit each published record against its review record, cited chunks and current source version. Records that cannot be reconciled should be handled by an explicitly authorized decision, not by silently editing data. Reopen if that audit finds gaps.
EOF

rec 31 "Ingest and embed" comment <<'EOF'
Not reopening. Its claimed database state was not verifiable in the audit. Verification belongs in an isolated test database, and the result depends on the source reconciliation in #30. UI and navigation work does not depend on this issue.
EOF

rec 26 "Define source" comment <<'EOF'
Noting for the record: the policy names its approver as "to be assigned" and does not establish classification or Asbab evidence. This does not block MVP engineering, but it blocks any approved introduction content. Assigning a named qualified owner is a project decision outside engineering, so it is referenced from {{NREV}} rather than expressed as a GitHub dependency.
EOF

rec 19 "C - Web reading|Epic C" comment <<'EOF'
Internal-MVP scope for the web reader (acceptance is browser behavior, not a passing build):
- [ ] Surah library from `GET /surahs` (#34) with an unavailable state for unseeded surahs
- [ ] Ordered ayahs ({{N3}}); explanation style selector using canonical IDs (#36); no cross-style substitution
- [ ] Introduction shown only via the experimental-labelled mode ({{NEXP}}) or the published path
- [ ] Distinct loading, empty, unavailable and error states ({{N4}})
- [ ] Audio playback with working controls and a failure state
- [ ] Browser tests (for example Playwright) against the fixture API ({{NFIX}})

New work tracked under this epic: {{N3}}, {{N4}}, {{NFIX}}, {{NEXP}}.
EOF

rec 20 "D - Mobile reading|Epic D" comment <<'EOF'
Internal-MVP scope for mobile. Fix first, in this order:
- [ ] Typecheck failure at `mobile/App.tsx:9` (tracked in #27)
- [ ] Style IDs aligned with the backend (`simplified_ar`, `egyptian_ar`, `en`; currently `egyptian`/`english`)
- [ ] Library and navigation from `GET /surahs`; remove hardcoded Surah 12
- [ ] Remove or make functional the presentation-only controls (tabs, back, bookmark)
- [ ] Playback toggle: the control labelled as pause currently restarts playback; pause/resume must work
- [ ] Response status and shape validation ({{N4}})
- [ ] Verified on an iOS or Android simulator/device (not yet device-tested)

Related new issues: {{N3}}, {{N4}}, {{NFIX}}, {{NEXP}}.
EOF

rec 23 "G - Quality and release|Epic G" comment <<'EOF'
Closure policy proposed for all issues: close only with a link to the merged PR and command output (or reviewer name and date for content tasks) for each acceptance criterion. A passing build or unit-test run is not enough, and Mongo integration tests that skip are not evidence. Public release stays gated on #15, #16, audio rights, qualified review, reviewer identity ({{NREV}}) and production readiness.
EOF

# ============================ 4. VALIDATION (read-only) ============================
echo; echo "== Validating (read-only) =="

validate_new() {
  local key found l m ok missing
  for key in "${NEW_KEYS[@]}"; do
    found="$(existing_number "${N_TITLE[$key]}")"
    if [ -n "$found" ]; then
      NUM[$key]="$found"; N_PLAN[$key]="SKIP: an issue with this exact title already exists (#$found, $(existing_state "$found"))"
    else
      N_PLAN[$key]="CREATE"
    fi
    ok=""; missing=""
    for l in ${N_LABELS[$key]} "${N_PRI[$key]}" "effort:${N_EFF[$key]}"; do
      if has_line "$WORK/labels.txt" "$l"; then ok+="$l "; else missing+="$l "; fi
    done
    N_LBL_OK[$key]="$ok"; N_LBL_MISSING[$key]="$missing"
    if [ -n "$missing" ]; then warn "new issue $key: labels not found and will be skipped: $missing"; fi
    if has_line "$WORK/milestones.txt" "${N_MS[$key]}"; then N_MS_OK[$key]=1; else N_MS_OK[$key]=0; warn "new issue $key: milestone '${N_MS[$key]}' not found; it will be created without one"; fi
  done
}

validate_rec() {
  local n act state title marked
  for n in "${REC_N[@]}"; do
    act="${R_ACT[$n]}"
    if ! view_issue "$n"; then err "#$n does not exist"; R_PLAN[$n]="ERROR: issue not found"; continue; fi
    title="$(jq -r .title "$WORK/view_$n.json")"; state="$(jq -r .state "$WORK/view_$n.json")"
    R_TITLE[$n]="$title"; R_STATE[$n]="$state"
    if ! title_matches "$title" "${R_FRAG[$n]}"; then
      err "#$n title does not match expected '${R_FRAG[$n]}' (actual: '$title'); refusing to touch it"
      R_PLAN[$n]="ERROR: title mismatch"; continue
    fi
    marked="$(jq -r --arg m "$MARK" '[.comments[].body | contains($m)] | any' "$WORK/view_$n.json")"
    if [ "$marked" = true ]; then
      if [ "$act" = reopen ] && [ "$state" = CLOSED ]; then
        warn "#$n already carries a reconciliation comment but is CLOSED. It may have been closed again on purpose, so it will NOT be reopened automatically."
      fi
      R_PLAN[$n]="SKIP: reconciliation comment already present"
    elif [ "$act" = reopen ] && [ "$state" = CLOSED ]; then R_PLAN[$n]="REOPEN + COMMENT"
    elif [ "$act" = reopen ]; then R_PLAN[$n]="COMMENT only (already OPEN, no state change)"
    else R_PLAN[$n]="COMMENT only (state stays $state)"; fi
  done
}

validate_rescope() {
  local n title state body a missing
  for n in "${RS_N[@]}"; do
    if ! view_issue "$n"; then err "#$n (re-scope) does not exist"; RS_PLAN[$n]="ERROR: issue not found"; continue; fi
    title="$(jq -r .title "$WORK/view_$n.json")"; state="$(jq -r .state "$WORK/view_$n.json")"
    RS_UPDATED[$n]="$(jq -r .updatedAt "$WORK/view_$n.json")"
    jq -j .body "$WORK/view_$n.json" > "$WORK/rs_old_$n.md"
    if ! title_matches "$title" "${RS_FRAG[$n]}"; then
      err "#$n re-scope: title '$title' does not match expected '${RS_FRAG[$n]}'. MANUAL REVIEW REQUIRED; not editing."
      RS_PLAN[$n]="ERROR: title mismatch"; continue
    fi
    if grep -qF -- "$RESCOPE_MARK" "$WORK/rs_old_$n.md"; then RS_PLAN[$n]="SKIP: re-scope marker already in body"; continue; fi
    if [ ! -s "$WORK/rs_old_$n.md" ]; then
      err "#$n re-scope: existing body is empty. MANUAL REVIEW REQUIRED; not editing."; RS_PLAN[$n]="ERROR: empty body"; continue
    fi
    missing=""
    for a in '### Scope' '### Acceptance Criteria'; do
      if ! grep -qF -- "$a" "$WORK/rs_old_$n.md"; then missing+="'$a' "; fi
    done
    if [ -n "$missing" ]; then
      err "#$n re-scope: body lacks expected sections ($missing). It has probably been edited by hand. MANUAL REVIEW REQUIRED; not editing."
      RS_PLAN[$n]="ERROR: body does not match expected template"; continue
    fi
    { echo "$RESCOPE_MARK"; echo; cat "$WORK/rescope_$n.md"; echo; echo "---"
      echo "## Original issue text (preserved verbatim)"; echo "_Previous title: ${title}_"; echo
      cat "$WORK/rs_old_$n.md"; } > "$WORK/rs_new_$n.md"
    RS_PLAN[$n]="EDIT title and body (state $state unchanged); copy saved to $BACKUP_DIR first"
    RS_TITLE_OLD[$n]="$title"
  done
}

check_cycles() {
  local -A indeg=() removed=(); local -a nodes=()
  local l a b n progress remaining
  for l in "${LINKS[@]}"; do
    a="${l%% *}"; b="${l#* }"
    for n in "$a" "$b"; do
      if [ -z "${indeg[$n]+x}" ]; then indeg[$n]=0; nodes+=("$n"); fi
    done
  done
  for l in "${LINKS[@]}"; do a="${l%% *}"; indeg[$a]=$(( indeg[$a] + 1 )); done
  remaining=${#nodes[@]}
  while :; do
    progress=0
    for n in "${nodes[@]}"; do
      if [ -z "${removed[$n]:-}" ] && [ "${indeg[$n]}" -eq 0 ]; then
        removed[$n]=1; remaining=$((remaining-1)); progress=1
        for l in "${LINKS[@]}"; do
          if [ "${l#* }" = "$n" ]; then a="${l%% *}"; indeg[$a]=$(( indeg[$a] - 1 )); fi
        done
      fi
    done
    [ "$progress" = 1 ] || break
  done
  if [ "$remaining" -gt 0 ]; then
    local left=""; for n in "${nodes[@]}"; do if [ -z "${removed[$n]:-}" ]; then left+="$n "; fi; done
    err "dependency links contain a cycle among: $left"
  fi
}

DEP_API=1
declare -A L_STATUS
validate_links() {
  local l a b na probe_n="" nb present
  for l in "${LINKS[@]}"; do
    a="${l%% *}"; b="${l#* }"
    if [ "$a" = "$b" ]; then err "self-referencing link: $l"; fi
    for t in "$a" "$b"; do
      if ! tok_known "$t"; then err "link '$l' references unknown token '$t'"; continue; fi
      if is_num "$t" && [ -z "$(existing_state "$t")" ]; then err "link '$l': issue #$t does not exist"; fi
    done
    if is_num "$a" && [ -z "$probe_n" ]; then probe_n="$a"; fi
  done
  check_cycles
  if [ -n "$probe_n" ]; then
    if gh api "repos/$REPO/issues/$probe_n/dependencies/blocked_by" > "$WORK/dep_probe.json" 2> "$WORK/gh.err"; then
      DEP_API=1
    elif grep -qiE 'HTTP 404|HTTP 410|not found' "$WORK/gh.err"; then
      DEP_API=0
      if [ "$ALLOW_NO_DEP_LINKS" = 1 ]; then warn "issue-dependencies API unavailable; GitHub links will be SKIPPED (ALLOW_NO_DEP_LINKS=1). Add them manually."
      else err "issue-dependencies API is unavailable on this repository, so real GitHub links cannot be created (set ALLOW_NO_DEP_LINKS=1 to proceed without them)"; fi
    else
      die "unexpected gh error probing the dependencies API: $(cat "$WORK/gh.err")"
    fi
  fi
  for l in "${LINKS[@]}"; do
    a="${l%% *}"; b="${l#* }"; na="$(tok_num "$a")"; nb="$(tok_num "$b")"
    if [ "$DEP_API" = 0 ]; then L_STATUS[$l]="SKIP (dependencies API unavailable)"; continue; fi
    if [ -z "$na" ] || [ -z "$nb" ]; then L_STATUS[$l]="CREATE (after the new issue exists)"; continue; fi
    if [ ! -f "$WORK/dep_$na.json" ]; then
      gh api "repos/$REPO/issues/$na/dependencies/blocked_by" > "$WORK/dep_$na.json" 2> "$WORK/gh.err" \
        || die "unexpected gh error reading dependencies of #$na: $(cat "$WORK/gh.err")"
    fi
    present="$(jq -r --argjson b "$nb" 'any(.[]; .number == $b)' "$WORK/dep_$na.json")"
    if [ "$present" = true ]; then L_STATUS[$l]="EXISTS (skip)"; else L_STATUS[$l]="CREATE"; fi
  done
}

UNRESOLVED=()
scan_placeholders() {
  local f k seen=""
  for f in "$WORK"/raw_*.md "$WORK"/rec_*.md "$WORK"/rescope_*.md; do
    [ -f "$f" ] || continue
    for k in $(grep -oE '\{\{[A-Za-z0-9_]+\}\}' "$f" | tr -d '{}' | sort -u); do
      if [ -z "${N_SEEN[$k]:-}" ]; then err "unknown placeholder {{$k}} in $(basename "$f")"
      elif [ -z "${NUM[$k]:-}" ] && [[ " $seen " != *" $k "* ]]; then seen+="$k "; UNRESOLVED+=("$k"); fi
    done
  done
}

validate_new; validate_rec; validate_rescope; validate_links; scan_placeholders
echo "Validation finished: ${#ERRORS[@]} error(s), ${#WARNINGS[@]} warning(s)."

# ============================ 5. PLAN DISPLAY ============================
show_plan() {
  local key n l b
  echo; echo "################ ACTION PLAN (DRY_RUN=$DRY_RUN; nothing is written by this section) ################"
  echo; echo "---- 1. New issues (${#NEW_KEYS[@]}) ----"
  for key in "${NEW_KEYS[@]}"; do
    echo; echo "[$key] ${N_PLAN[$key]}"
    echo "  Title:     ${N_TITLE[$key]}"
    echo "  Priority:  ${N_PRI[$key]}   Effort: ${N_EFF[$key]}"
    echo "  Labels to apply: ${N_LBL_OK[$key]:-none}   missing (skipped): ${N_LBL_MISSING[$key]:-none}"
    echo "  Milestone: ${N_MS[$key]} (exists: $([ "${N_MS_OK[$key]}" = 1 ] && echo yes || echo no))"
    if [ "${N_PLAN[$key]}" = CREATE ]; then echo "  Full proposed body:"; render_new_body "$key" | indent; fi
  done
  echo; echo "---- 2. Title/body re-scope of existing issues ----"
  for n in "${RS_N[@]}"; do
    echo; echo "#$n: ${RS_PLAN[$n]}"
    if [ -f "$WORK/rs_new_$n.md" ]; then
      echo "  BEFORE title: ${RS_TITLE_OLD[$n]:-$(jq -r .title "$WORK/view_$n.json")}"
      echo "  AFTER  title: ${RS_TITLE[$n]}"
      echo "  Body diff (- removed, + added; original text is kept verbatim, so only additions are expected):"
      diff -u "$WORK/rs_old_$n.md" "$WORK/rs_new_$n.md" | indent || true
      echo "  Removed lines from the original body: $(diff "$WORK/rs_old_$n.md" "$WORK/rs_new_$n.md" | grep -c '^<' || true)"
    fi
  done
  echo; echo "---- 3. Comments and reopens (${#REC_N[@]}) ----"
  for n in "${REC_N[@]}"; do
    echo; echo "#$n [${R_STATE[$n]:-?}] ${R_TITLE[$n]:-}"; echo "  Plan: ${R_PLAN[$n]}"
    case "${R_PLAN[$n]}" in
      REOPEN*|COMMENT*) echo "  Full comment text:"; render_comment "$n" | indent;;
    esac
  done
  echo; echo "---- 4. GitHub blocked-by links (real links via the dependencies API) ----"
  echo "Format: BLOCKED <- BLOCKER   [status]"
  for l in "${LINKS[@]}"; do echo "  $(tok_ref "${l%% *}") <- $(tok_ref "${l#* }")   [${L_STATUS[$l]}]"; done
  echo "Everything else written in comments or bodies as 'Referenced' or 'Related' stays plain text and is NOT linked."
  echo; echo "---- 5. Unresolved issue-number placeholders ----"
  if [ "${#UNRESOLVED[@]}" -eq 0 ]; then echo "  none"; else
    for key in "${UNRESOLVED[@]}"; do echo "  {{$key}} -> will become the number of the issue created for '${N_TITLE[$key]}' (not known until that issue is created)"; done
  fi
  echo; echo "---- 6. Warnings and errors ----"
  if [ "${#WARNINGS[@]}" -eq 0 ]; then echo "  warnings: none"; else printf '  WARNING: %s\n' "${WARNINGS[@]}"; fi
  if [ "${#ERRORS[@]}" -eq 0 ]; then echo "  errors: none"; else printf '  ERROR: %s\n' "${ERRORS[@]}"; fi
}
show_plan

if [ "${#ERRORS[@]}" -gt 0 ]; then
  echo; echo "Validation FAILED. Nothing was changed. Fix the errors above (manual review where indicated) and re-run."
  exit 1
fi
if [ "$DRY_RUN" = 1 ]; then
  echo; echo "Dry run complete. No GitHub writes were made. Review the plan, then run with DRY_RUN=0."
  exit 0
fi

# ============================ 6. APPLY (DRY_RUN=0 only) ============================
echo
if [ -n "${CONFIRM_REPO:-}" ]; then ans="$CONFIRM_REPO"; else read -r -p "Type the repository name ($REPO) to apply the plan above: " ans; fi
[ "$ans" = "$REPO" ] || die "confirmation did not match; aborting before any write"

CREATED=0; EDITED=0; REOPENED=0; COMMENTED=0; LINKED=0; SKIPPED=0

apply_new() {
  local key url larg marg
  for key in "${NEW_KEYS[@]}"; do
    if [ "${N_PLAN[$key]}" != CREATE ]; then echo "skip    $key: ${N_PLAN[$key]}"; SKIPPED=$((SKIPPED+1)); continue; fi
    render_new_body "$key" > "$WORK/final_$key.md"
    larg=(); local l
    for l in ${N_LBL_OK[$key]}; do larg+=(--label "$l"); done
    marg=(); if [ "${N_MS_OK[$key]}" = 1 ]; then marg=(--milestone "${N_MS[$key]}"); fi
    url="$(gh issue create --repo "$REPO" --title "${N_TITLE[$key]}" --body-file "$WORK/final_$key.md" \
          ${marg[@]+"${marg[@]}"} ${larg[@]+"${larg[@]}"})" \
      || die "creating '${N_TITLE[$key]}' failed. Issues created so far are listed above; re-running is safe (exact-title dedupe)."
    NUM[$key]="${url##*/}"
    is_num "${NUM[$key]}" || die "could not parse the new issue number from '$url'"
    CREATED=$((CREATED+1)); echo "created #${NUM[$key]}  ${N_TITLE[$key]}"
  done
}

apply_rescope() {
  local n fresh
  for n in "${RS_N[@]}"; do
    case "${RS_PLAN[$n]}" in EDIT*) ;; *) echo "skip    #$n re-scope: ${RS_PLAN[$n]}"; SKIPPED=$((SKIPPED+1)); continue;; esac
    fresh="$WORK/fresh_$n.json"
    gh issue view "$n" --repo "$REPO" --json title,body,updatedAt > "$fresh" || die "cannot re-read #$n before editing"
    [ "$(jq -r .updatedAt "$fresh")" = "${RS_UPDATED[$n]}" ] || die "#$n changed since validation; re-run to re-validate (nothing was edited)"
    mkdir -p "$BACKUP_DIR"
    cp "$WORK/view_$n.json" "$BACKUP_DIR/issue-$n-before-$(date +%Y%m%dT%H%M%S).json" || die "backup failed; not editing #$n"
    gh issue edit "$n" --repo "$REPO" --title "${RS_TITLE[$n]}" --body-file "$WORK/rs_new_$n.md" >/dev/null \
      || die "editing #$n failed"
    gh issue view "$n" --repo "$REPO" --json body -q .body | grep -qF -- "$RESCOPE_MARK" \
      || die "#$n edit could not be verified (marker missing afterwards); inspect it manually and restore from $BACKUP_DIR if needed"
    EDITED=$((EDITED+1)); echo "edited  #$n (backup in $BACKUP_DIR)"
  done
}

apply_rec() {
  local n act state marked
  for n in "${REC_N[@]}"; do
    case "${R_PLAN[$n]}" in REOPEN*|COMMENT*) ;; *) echo "skip    #$n: ${R_PLAN[$n]}"; SKIPPED=$((SKIPPED+1)); continue;; esac
    view_issue "$n" || die "#$n disappeared before commenting"
    state="$(jq -r .state "$WORK/view_$n.json")"
    marked="$(jq -r --arg m "$MARK" '[.comments[].body | contains($m)] | any' "$WORK/view_$n.json")"
    if [ "$marked" = true ]; then echo "skip    #$n: already reconciled"; SKIPPED=$((SKIPPED+1)); continue; fi
    render_comment "$n" > "$WORK/final_comment_$n.md"
    act="${R_ACT[$n]}"
    if [ "$act" = reopen ] && [ "$state" = CLOSED ]; then
      gh issue reopen "$n" --repo "$REPO" --comment "$(cat "$WORK/final_comment_$n.md")" >/dev/null || die "reopening #$n failed"
      REOPENED=$((REOPENED+1)); echo "reopened #$n (+comment)"
    else
      gh issue comment "$n" --repo "$REPO" --body-file "$WORK/final_comment_$n.md" >/dev/null || die "commenting on #$n failed"
      COMMENTED=$((COMMENTED+1)); echo "commented #$n (state $state unchanged)"
    fi
  done
}

apply_links() {
  local l a b na nb id out
  for l in "${LINKS[@]}"; do
    a="${l%% *}"; b="${l#* }"; na="$(tok_num "$a")"; nb="$(tok_num "$b")"
    if [ "$DEP_API" = 0 ]; then echo "skip    link #$na <- #$nb (dependencies API unavailable; add manually)"; SKIPPED=$((SKIPPED+1)); continue; fi
    [ -n "$na" ] && [ -n "$nb" ] || die "link '$l' has an unresolved issue number at apply time"
    gh api "repos/$REPO/issues/$na/dependencies/blocked_by" > "$WORK/dep_now_$na.json" 2> "$WORK/gh.err" \
      || die "cannot read dependencies of #$na: $(cat "$WORK/gh.err")"
    if [ "$(jq -r --argjson b "$nb" 'any(.[]; .number == $b)' "$WORK/dep_now_$na.json")" = true ]; then
      echo "skip    link #$na <- #$nb already exists"; SKIPPED=$((SKIPPED+1)); continue
    fi
    id="$(gh api "repos/$REPO/issues/$nb" --jq .id)" || die "cannot read the id of #$nb"
    if ! out="$(gh api -X POST "repos/$REPO/issues/$na/dependencies/blocked_by" -F issue_id="$id" 2>&1)"; then
      die "linking #$na <- #$nb failed: $out"
    fi
    LINKED=$((LINKED+1)); echo "linked  #$na is blocked by #$nb"
  done
}

apply_new; apply_rescope; apply_rec; apply_links
echo
echo "Done on $REPO. created=$CREATED edited=$EDITED reopened=$REOPENED commented=$COMMENTED linked=$LINKED skipped=$SKIPPED"