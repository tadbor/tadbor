# Content Review Policy: Introductions, Classifications, and Asbab al-Nuzul

- Status: Accepted
- Date: 2026-10-24
- Owners: Content Review Lead (to be assigned)

## Scope
Covers surah introductions, Makki/Madani classification, themes, stories, and Asbab al-Nuzul (occasions of revelation). These are religious/historical claims and require attribution.

## Approved Sources and Authority Tiers
- Prefer well-known, verifiable tafasir and standard sources. Each source must be registered in `corpus/sources/` with `licensing_status`, `verification_status`, authority tier, edition, and license basis.
- Tier 1: Classical tafasir with clear attribution (e.g. Ibn Kathir) - only use if `verification_status: verified`.
- Other sources require review and clearance before registration.

## Representation Rules
- **Attribution required:** Every claim must cite approved sources via `source_refs` to resolved chunks/entries.
- **Disputed reports:** If scholars differ, present both views with attribution and hedged language (e.g. "Some say...", "Others hold..."). Do not pick one as fact.
- **Hedging:** Use cautious phrasing for narrations of uncertain strength.
- **No source = no publication:** If no approved evidence supports a claim, it must not be published.
- **Separation:** Distinguish Qur'anic text from commentary/historical reports.

## Makki/Madani Classification
- Use a documented classification source (to be registered). Disputed surahs must be noted.
- Record classification rationale with citations.

## Required Fields for Introductions
- Surah number/name, classification (with source), overview/themes, relevant context, Asbab al-Nuzul (if known with sources), disputed notes, references.
- All published entries must have review records and citations.

## Reviewer Checklist
- [ ] All claims supported by cited approved sources
- [ ] Disputed views represented fairly with attribution
- [ ] No Qur'anic text misattributed or altered
- [ ] Classification matches cited source or marked disputed
- [ ] Hedging used where appropriate
- [ ] Citations resolve to existing evidence
- [ ] Clear separation between text and commentary

## Approval
Only qualified content reviewers may approve. Reviews recorded via the review system with audit trail.
