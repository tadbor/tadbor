# ADR 0001: Translation vs Explanation Policy and Style Registry

- Status: Accepted
- Date: 2026-10-24
- Deciders: Maintainers, Content Reviewers

## Context

Tadbor produces RAG-grounded explanations from tafsir sources with mandatory human review. The system currently supports styles `simplified_ar`, `egyptian_ar`, and `en` (see API/content/generation). We need a clear policy on when output is an "explanation" vs a "translation", how each is labeled in UI, reviewer qualifications per dialect/language, and a path to add styles without schema changes.

## Decision

### 1) Explanation vs Translation

- **Explanation (default):** Output derived from approved tafsir/commentary via RAG, transformed per task briefs (e.g. `simplify`), with citations to source chunks, `requires_review: true`, and published only after human approval. Explanations interpret/clarify meaning; they are not presented as the Qur'an text.
- **Translation:** A cited translation of the Qur'anic text (or an approved translation source) with clear attribution to that translation and its license. In this project, `en` is **not** an authenticated Qur'an translation by default. 
- **Policy:** `en` is treated as a **simplified explanation in English** grounded in tafsir unless there is a separately licensed, cited translation source registered and explicitly marked `kind: translation` with attribution/rights. The UI must never label explanation output as "translation of the Qur'an". Quranic text itself remains the Uthmani text from the verified corpus.

### 2) Style Registry (extensible without schema changes)

Define a logical style registry (data/config) with fields:
- `id` (string, stable, e.g. `simplified_ar`, `egyptian_ar`, `en`)
- `language` (BCP-47 or simple code: `ar`, `en`)
- `dialect` (e.g. `msaar`, `egyptian`, `en` or null)
- `kind` (enum: `explanation` | `translation`) 
- `display_label` (UI label, localized-friendly)
- `direction` (rtl/ltr)
- `status` (enum: `active` | `beta` | `disabled`)
- `requires_reviewer_qualification` (array of roles/qualifications)
- `notes` (licensing/attribution notes)

Adding a new style requires adding a registry entry (and possibly task brief/validation) but no DB schema change to core models.

### 3) UI Labeling
- Explanations labeled as "explanation" / meaning (e.g. Arabic "المعنى المبسط", English "Explanation" or "Meaning") — never "Qur'an Translation".
- Translations (if ever added) must show translator name, edition, license/attribution clearly and be marked distinctly.
- Styles list shows labels from registry.

### 4) Reviewer Qualifications
- `simplified_ar`: Arabic-literate reviewer familiar with tafsir basics.
- `egyptian_ar`: Reviewer fluent in Egyptian Arabic with tafsir familiarity (dialect fidelity per checklist).
- `en`: Reviewer with Arabic+English familiarity and tafsir understanding to verify grounding/citations (not just fluency). 
- Qualifications enforced by process/checklist; system may track but not replace human judgment.

### 5) Adding a Dialect/Style
Process: propose style in registry with rationale, add validation/task brief if needed, define reviewer qualifications, update UI labels, test with pilot content, document in ADR/changelog.

## Consequences
- Prevents conflating explanation with translation.
- Clear path to extend styles without schema migrations.
- Reviewers know what to check per style.
- Safeguards against mislabeling remain intact.

## References
- `backend/internal/generation/validation.go` (script checks)
- `backend/internal/content/models.go`, `backend/internal/generation/service.go`
- `docs/reviewer-dashboard.md`
