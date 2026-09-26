# Tadbor — Master Documentation Outline

This is the skeleton for Tadbor's single source of truth. Each section below defines its purpose, scope, open decisions, and dependencies — not the final content. Sections are grouped into five tracks that mirror how the project actually needs to be built: **Product & Religious Foundation → Content & AI Design → Data & Technical Architecture → Experience & Operations → Quality & Governance**.

Throughout every section, these principles apply and should never be contradicted by a technical or product decision:
1. The Quran text is immutable and never altered.
2. The app explains/translates meaning — it does not produce an alternate Quran.
3. AI never independently generates or invents Tafsir.
4. AI's role is limited to simplification, translation, dialect adaptation, organization, and UX assistance.
5. All religious content is grounded in trusted Tafsir sources.
6. Human review is a mandatory gate before anything religious is published.
7. Every explanation is traceable to a source.
8. Quran text and explanation/translation are separated in both data model and UI.
9. MVP = Surah Yusuf only.
10. First explanation styles: Simplified Arabic, Egyptian Arabic, Modern English.
11. Architecture supports future languages/dialects without redesign.
12. Accuracy and religious integrity outrank features, speed, and UX convenience.

---

## Track A — Product & Religious Foundation

### A1. Product Vision
- **Purpose:** State what Tadbor is and is not, in one page, so every later decision can be checked against it.
- **Defines:** The one-sentence mission; the "not an AI tafsir generator" boundary; success definition beyond downloads (e.g., correct understanding, trust from religious reviewers).
- **Key decisions:** How to phrase the "meaning stays fixed, expression adapts" principle publicly without implying a new translation of the Quran; who the vision statement must be approved by (a religious authority, an advisor, or Mahmoud alone for MVP).
- **Dependencies:** None — this is the root document everything else traces back to.
- **MVP vs Future:** MVP needs the full vision written now, since it constrains scope decisions later. No future-only content here.

### A2. Problem Statement
- **Purpose:** Justify why this app should exist rather than existing apps/translations.
- **Defines:** The specific comprehension gap (classical Arabic vs. spoken dialects/other languages); why existing translations/tafsir apps don't solve it; the risk of *not* having human review (i.e., why generic AI explanation is dangerous here).
- **Key decisions:** Whether the problem statement should explicitly name gaps in competing apps, or stay generic.
- **Dependencies:** A1 (Vision).
- **MVP vs Future:** Fully written for MVP; doesn't change with scope expansion.

### A3. Target Users
- **Purpose:** Define who Tadbor is for, to scope tone, dialect choices, and explanation depth.
- **Defines:** Primary segment (e.g., non-fluent-in-fusha Arabic speakers), secondary segment (non-Arabic-speaking Muslims), explicitly excluded users (e.g., Tafsir scholars who need primary-source depth — out of scope for MVP).
- **Key decisions:** Whether children/new Muslims are a target segment now or later (affects tone and Beginner-level design in A4/C-track content model).
- **Dependencies:** A2.
- **MVP vs Future:** MVP defines the Surah-Yusuf-reader persona; broader personas (reciters, teachers, scholars) are future.

### A4. User Personas
- **Purpose:** Make target users concrete enough to design against.
- **Defines:** 2–3 personas with language background, religious knowledge level, device/context of use (e.g., reading during commute vs. structured study).
- **Key decisions:** Whether personas are based on real user interviews or founder assumptions for MVP (affects how much weight to put on them later).
- **Dependencies:** A3.
- **MVP vs Future:** MVP needs 1–2 personas matching the Surah Yusuf reader; expand persona set alongside future language/surah rollout.

### A5. Functional Requirements
- **Purpose:** Enumerate what the system must do, independent of how.
- **Defines:** Reading Quran text; viewing surah intro; viewing per-verse simplified meaning; switching explanation style (language/dialect); viewing source citation; (future) search, bookmarking, audio.
- **Key decisions:** Whether style-switching is per-verse or a global toggle; whether users can compare two explanation styles side by side.
- **Dependencies:** A1–A4, and feeds directly into B1 (Content Architecture) and C-track data model.
- **MVP vs Future:** MVP = read Surah Yusuf, switch between the 3 initial styles, see source. Search, audio, bookmarking, notes are future.

### A6. Non-Functional Requirements
- **Purpose:** Define quality bars that aren't features — reliability, performance, accessibility, content-integrity guarantees.
- **Defines:** Uptime expectations, load-time targets, offline reading support, accessibility (font size/RTL support), and — critically — a non-functional requirement that no unreviewed content can ever reach production.
- **Key decisions:** Whether offline access is required for MVP (relevant to a reading app used during commutes/prayer times).
- **Dependencies:** A5.
- **MVP vs Future:** The "no unreviewed content in production" rule is MVP-mandatory. Performance/scale targets can be lightweight for MVP and formalized later.

---

## Track B — Content & AI Design

### B1. Content Architecture
- **Purpose:** Define the overall shape of content in the system and how it flows from source to user, mirroring the pipeline in the project brief.
- **Defines:** The layered structure — Quran text layer, Tafsir source layer, verified interpretation layer, simplified explanation layer, dialect/language layer, review-status layer.
- **Key decisions:** Whether layers are strictly sequential (each depends on the previous being reviewed) or can branch (e.g., translating into English doesn't require re-deriving from Arabic dialect text).
- **Dependencies:** A5, A6.
- **MVP vs Future:** MVP implements the full layered pipeline for one surah; future work is scaling the same architecture, not redesigning it.

### B2. Quran Content Model
- **Purpose:** Define how the immutable Quran text itself is represented and sourced.
- **Defines:** Which authoritative Quran text source/API to use (Uthmani script standard); surah/ayah numbering scheme; how text is protected from any edit path in the system.
- **Key decisions:** Which specific trusted Quran text source to adopt; whether the app stores the text locally or fetches from a verified API each time.
- **Dependencies:** B1.
- **MVP vs Future:** Must be finalized for MVP — this is the one part of the model that can never change later, so getting the source right now matters most.

### B3. Tafsir Source Model
- **Purpose:** Define which tafsir works are trusted inputs and how they're represented in the system.
- **Defines:** List of approved tafsir sources (e.g., specific classical or contemporary tafsirs) and their metadata (author, era, language, licensing); how a tafsir source is looked up per-verse.
- **Key decisions:** Which specific tafsir(s) to license/use for Surah Yusuf; whether the app can mix multiple tafsirs per verse or must pick one primary source per verse for MVP; licensing/usage rights for chosen tafsir texts.
- **Dependencies:** B2.
- **MVP vs Future:** MVP needs at least one authoritative tafsir source fully cleared (rights + reviewer familiarity) for Surah Yusuf. Multi-tafsir comparison is future.

### B4. Translation Model
- **Purpose:** Define how a verified interpretation becomes a translated explanation.
- **Defines:** Source-of-truth text that gets translated (the reviewed interpretation, not the raw tafsir or the Quran directly); translation review step; storage of translated variants per verse.
- **Key decisions:** Whether translation is AI-drafted + human-approved, or human-translated from the start for MVP; who is qualified to approve an English rendering as faithful.
- **Dependencies:** B3.
- **MVP vs Future:** MVP: one reviewed English explanation per verse. Future: additional languages reuse this same model.

### B5. Dialect Adaptation Model
- **Purpose:** Define how the same fixed meaning is re-expressed in different Arabic dialects/styles without altering meaning.
- **Defines:** The "meaning stays fixed, expression adapts" transformation step; how a dialect variant is linked back to its parent verified meaning (never independently derived); reviewer sign-off scoped to *this* dialect rendering.
- **Key decisions:** Whether Simplified Arabic and Egyptian Arabic are produced independently from the verified interpretation or derived sequentially (Simplified Arabic → Egyptian); how to prevent dialect adaptation from drifting doctrinally (style-only checklist for reviewers).
- **Dependencies:** B3, B4 (shares the same "adapt without altering meaning" governance).
- **MVP vs Future:** MVP covers Simplified Arabic + Egyptian Arabic. Model must be generic enough that adding Levantine, Gulf, etc. later needs no redesign.

### B6. AI Architecture
- **Purpose:** Define exactly where AI is allowed to act in the pipeline and where it is not.
- **Defines:** AI touchpoints (drafting simplification, drafting translation, drafting dialect rendering, organizing content, powering search/personalization later); explicit non-touchpoints (AI never proposes new tafsir, never fills gaps in sources, never publishes without review).
- **Key decisions:** Which model(s)/provider to use; whether AI drafts are shown to reviewers as suggestions only, with mandatory edits allowed; how AI output is flagged as "AI-drafted, unreviewed" internally until sign-off.
- **Dependencies:** B1–B5 (AI operates on outputs of these models, not on raw sources directly).
- **MVP vs Future:** MVP needs the guardrails (B6) fully defined even if the AI drafting itself is simple/manual-assisted. Personalization, semantic search, and Q&A features are future and need their own AI-boundary review before being added.

---

## Track C — Data & Technical Architecture

### C1. Database Design
- **Purpose:** Model all the entities above (Quran text, tafsir sources, interpretations, explanations, dialects, review status) as concrete schemas.
- **Defines:** Tables/collections for Surah, Ayah, TafsirSource, Interpretation, Explanation (per language/dialect/level), ReviewRecord, Citation; relationships enforcing that explanations always reference a source and a review status.
- **Key decisions:** Relational vs. document store (content is hierarchical and citation-heavy, which usually favors relational integrity); how review status blocks content from being queried by the public API until approved.
- **Dependencies:** B1–B6.
- **MVP vs Future:** MVP schema only needs Surah Yusuf's entities but should be designed as if every surah will be added — no Yusuf-specific hardcoding.

### C2. API Design
- **Purpose:** Define how the frontend, admin dashboard, and AI drafting pipeline talk to the data layer.
- **Defines:** Public read endpoints (get surah, get ayah with explanations by style); internal endpoints (submit AI draft, submit reviewer decision, publish); versioning strategy.
- **Key decisions:** Whether public API can ever return unreviewed content (should be a hard "no" enforced at the query layer, not just the UI layer).
- **Dependencies:** C1.
- **MVP vs Future:** MVP needs public read + internal review endpoints. Rate limiting, external API access for third parties is future.

### C3. Admin / Reviewer Dashboard
- **Purpose:** Give the human reviewer a working tool to approve/reject/edit content before publish.
- **Defines:** Reviewer queue (AI drafts awaiting review); side-by-side view of source tafsir vs. drafted explanation; approve/reject/edit-and-approve actions; audit trail of who approved what.
- **Key decisions:** Who the reviewer(s) are for MVP (a single qualified person vs. a small panel); whether edits by the reviewer go back through any secondary check.
- **Dependencies:** B6, C1, C2.
- **MVP vs Future:** MVP needs a functional (even minimal/internal-only) dashboard — this is not optional, since human review is a hard gate. Multi-reviewer workflows, comments/discussion threads are future.

### C4. Technical Architecture
- **Purpose:** Define the overall system architecture — services, hosting, how content pipeline stages connect.
- **Defines:** Frontend app architecture; backend services (content API, review service, AI drafting service); how the AI drafting step is decoupled from publishing.
- **Key decisions:** Mobile app vs. web-first for MVP; hosting/infra choices; how much of this reuses Mahmoud's existing stack/tools vs. new choices specific to Tadbor.
- **Dependencies:** C1–C3, B6.
- **MVP vs Future:** MVP architecture should be intentionally small (one surah, one review flow) but structured so scaling to more surahs/languages is additive, not a rewrite.

---

## Track D — Experience & Operations

### D1. User Experience Flow
- **Purpose:** Define the end-to-end reading journey.
- **Defines:** Entry → surah intro screen → verse-by-verse reading with explanation toggle → style/dialect switch → source view; how "before the surah" content (section 6 of the brief) is presented.
- **Key decisions:** Default explanation style shown on first open; how prominently the source citation is displayed (must be visible, not buried).
- **Dependencies:** A5, B1, D2.
- **MVP vs Future:** MVP flow is linear and single-surah. Cross-surah navigation, bookmarking, and personalized flows are future.

### D2. Localization Strategy
- **Purpose:** Define how new languages/dialects get added without re-architecting.
- **Defines:** The process for onboarding a new dialect/language (source review → draft → human review → publish), reusing B5's model; UI localization (RTL/LTR handling) separate from content localization.
- **Key decisions:** Whether UI chrome (buttons, menus) is localized independently of content language.
- **Dependencies:** B4, B5.
- **MVP vs Future:** MVP only needs Arabic + English UI. The *process* for adding more must be documented now even though it's not executed until future phases.

### D3. AI Prompting Strategy
- **Purpose:** Define the actual prompt/instruction design that keeps AI within its allowed role (B6).
- **Defines:** Prompt templates for simplification, translation, and dialect adaptation, each explicitly constrained to "rephrase this verified interpretation" rather than "explain this verse"; required inputs (the interpretation text, not the raw ayah) so the AI cannot bypass the source.
- **Key decisions:** Whether prompts include explicit refusal instructions if asked to generate new tafsir; how prompt versions are tracked/audited over time.
- **Dependencies:** B6.
- **MVP vs Future:** MVP needs finalized prompts for the 3 initial styles. Additional styles/languages reuse the same template pattern.

### D4. Hallucination Prevention
- **Purpose:** Define concrete technical/process controls that stop AI from inventing content.
- **Defines:** Constraining AI input strictly to the approved interpretation text (no open-ended "explain verse X" prompts); automated checks (e.g., flagging explanations that introduce named entities/claims not present in the source); mandatory human review as the final backstop.
- **Key decisions:** How much automated pre-review checking is feasible for MVP vs. relying entirely on the human reviewer.
- **Dependencies:** D3, B6.
- **MVP vs Future:** MVP relies primarily on strict prompting + human review. Automated drift-detection tooling is future.

---

## Track E — Quality & Governance

### E1. Safety & Religious Accuracy Rules
- **Purpose:** Consolidate section 10's content rules into an enforceable rule set for both AI and reviewers.
- **Defines:** The explicit prohibited list (inventing asbab al-nuzul, inventing stories/rulings, presenting AI opinion as tafsir, blending Quran and explanation visually, exaggerated emotional tone that shifts meaning).
- **Key decisions:** Whether these rules are enforced only via reviewer checklist or also via automated content flags.
- **Dependencies:** All of Track B.
- **MVP vs Future:** Must be fully defined before any content ships — this is not deferrable.

### E2. Human Review Workflow
- **Purpose:** Define the step-by-step process content goes through before publishing.
- **Defines:** Draft → reviewer checklist (accuracy, no added/removed meaning, translation/dialect correctness, clear Quran/explanation separation) → approve or send back → publish; escalation path if reviewer is unsure.
- **Key decisions:** Turnaround expectations; whether a second reviewer is required for MVP or one is sufficient.
- **Dependencies:** C3, E1.
- **MVP vs Future:** MVP needs the full checklist and single-reviewer flow working end-to-end. Multi-stage/multi-reviewer workflows are future.

### E3. Content Validation Pipeline
- **Purpose:** Define the automated/manual checks content passes through between AI drafting and human review.
- **Defines:** Structural checks (citation present, style tag correct, no missing fields) run before content even reaches a human reviewer.
- **Key decisions:** What's automatable now vs. manual-only for MVP.
- **Dependencies:** E2, D4.
- **MVP vs Future:** MVP can be mostly manual with a short automated pre-check list. Fuller automated QA is future.

### E4. Content QA Strategy
- **Purpose:** Define ongoing quality assurance after content is published (not just before).
- **Defines:** Periodic re-review cadence; user-reported issue handling (e.g., a user flags a possibly inaccurate explanation).
- **Key decisions:** Whether users can flag content in MVP, and who triages flags.
- **Dependencies:** E2, E3.
- **MVP vs Future:** Basic flagging + reviewer follow-up is worth having even at MVP given the sensitivity of the content. Formal QA cadences/metrics are future.

### E5. Versioning of Religious Content
- **Purpose:** Define how changes to published explanations are tracked over time.
- **Defines:** Version history per explanation; who can revise published content and under what review requirement (a revision should go through the same gate as new content).
- **Key decisions:** Whether old versions remain visible/auditable to users or only to admins.
- **Dependencies:** C1, E2.
- **MVP vs Future:** MVP needs basic version history (even simple timestamped records) since religious content edits must be fully traceable. Public change-logs are future.

### E6. Audit Logs
- **Purpose:** Record who did what, when, across the content pipeline.
- **Defines:** Logging of AI draft generation, reviewer decisions, edits, publishes, and any content flags/reports.
- **Key decisions:** Retention period; who can access logs.
- **Dependencies:** C2, C3, E2.
- **MVP vs Future:** MVP needs logging of review decisions at minimum. Full system-wide audit tooling can mature later.

### E7. Security
- **Purpose:** Define standard application security requirements.
- **Defines:** Auth for admin/reviewer accounts; protection of the publish pipeline so only reviewed content is exposed; standard API/data security practices.
- **Key decisions:** Auth provider/approach for the admin dashboard.
- **Dependencies:** C2, C3.
- **MVP vs Future:** MVP needs reviewer-account security and publish-pipeline protection at minimum.

### E8. Privacy
- **Purpose:** Define what user data is collected and how it's handled.
- **Defines:** What's collected (reading progress, preferred style/dialect) vs. what isn't; data retention and deletion.
- **Key decisions:** Whether MVP needs user accounts at all, or can be anonymous/local-only.
- **Dependencies:** A5, C1.
- **MVP vs Future:** MVP can likely minimize data collection (no accounts needed to read). Personalization features later will need a fuller privacy policy.

### E9. Testing Strategy
- **Purpose:** Define how correctness is verified across both software and content layers.
- **Defines:** Standard software testing (unit/integration/e2e) plus a content-specific test category (does published content match its approved interpretation exactly, is source citation always present).
- **Key decisions:** How much content-integrity testing can be automated vs. relies on the review gate itself.
- **Dependencies:** E1–E3.
- **MVP vs Future:** MVP needs at least the content-integrity checks. Full automated test suite maturity is ongoing/future.

### E10. MVP Scope
- **Purpose:** Serve as the single, unambiguous list of what ships first.
- **Defines:** Surah Yusuf, 3 explanation styles (Simplified Arabic, Egyptian Arabic, Modern English), one tafsir source, single-reviewer workflow, basic reading UI, no accounts/search/audio.
- **Key decisions:** None — this section aggregates decisions made in A–E rather than making new ones.
- **Dependencies:** Every other section feeds into this one.
- **MVP vs Future:** This section *is* the MVP/Future boundary; keep it updated as decisions in other sections are finalized.

### E11. Future Roadmap
- **Purpose:** Capture what's intentionally deferred, so scope stays disciplined.
- **Defines:** Additional surahs, additional dialects/languages, search, bookmarking/notes, audio recitation, multi-tafsir comparison, personalization, community/scholar contribution workflow.
- **Key decisions:** Rough sequencing (e.g., more surahs before more languages, or vice versa).
- **Dependencies:** E10.
- **MVP vs Future:** Entirely future by definition — kept here so it doesn't silently creep into MVP scope.

---

## Recommended Implementation / Documentation Order

Religious-content and product decisions must be locked before technical design, since technical choices (schema, API, AI prompts) all depend on them and are expensive to unwind later.

1. **A1–A4** (Vision, Problem, Users, Personas) — grounds everything; fast to finalize.
2. **B2–B3** (Quran Content Model, Tafsir Source Model) — the two decisions that can never be revisited later (text source, approved tafsir); must be locked early.
3. **E1** (Safety & Religious Accuracy Rules) — write this right after B2–B3, before any AI or reviewer tooling is designed, since it governs both.
4. **B4–B5** (Translation Model, Dialect Adaptation Model) — defines what "adapting without altering meaning" actually means operationally.
5. **B1, B6** (Content Architecture, AI Architecture) — now that source and adaptation rules exist, define the pipeline and AI's exact boundaries within it.
6. **A5–A6** (Functional / Non-Functional Requirements) — can now be written precisely against a defined pipeline.
7. **C1–C2** (Database Design, API Design) — technical modeling of everything decided above.
8. **E2–E3** (Human Review Workflow, Content Validation Pipeline) — process design, needed before dashboard build.
9. **C3–C4** (Admin/Reviewer Dashboard, Technical Architecture) — build the tools the process in step 8 requires.
10. **D1–D4** (UX Flow, Localization Strategy, AI Prompting Strategy, Hallucination Prevention) — user-facing and AI-execution details, informed by the finalized architecture.
11. **E4–E9** (QA Strategy, Versioning, Audit Logs, Security, Privacy, Testing Strategy) — operational hardening, needed before real users touch the system.
12. **E10–E11** (MVP Scope, Future Roadmap) — finalize last, as the living summary of every decision above; revisit whenever any earlier section changes.

Sections 2–3 above (Quran source + tafsir source + accuracy rules) are the ones with no safe way to "start building and fix later" — everything else can iterate.
