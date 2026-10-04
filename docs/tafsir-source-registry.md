# Tafsir source registry — Surah 12

Issue #3. The Tier-1 source is **Tafsir Ibn Kathir**, `ibn-kathir-ar`.

Regenerate or verify with:

```
cd backend
go run ./cmd/fetch_tafsir -surah 12 -tafsir-id 14          # re-download the raw text
go run ./cmd/fetch_tafsir -surah 12 -tafsir-id 14 -check   # confirm the file matches upstream
go run ./cmd/register_source -file ../corpus/sources/ibn-kathir-ar.json
go run ./cmd/register_source -file ../corpus/sources/ibn-kathir-ar.json -check
go run ./cmd/register_source -list
```

## Why Ibn Kathir, and why this endpoint

The licensing choice was the easy part: Ibn Kathir died in 774 AH / 1373 CE, so
the work is out of copyright. It is also the example the issue itself names.

The endpoint mattered more than the choice of tafsir. Classical tafsir are usually
published as continuous prose that has to be split into per-ayah passages by
parsing ayah markers out of the text. A marker that is missed or misread silently
attributes commentary to the wrong verse, and Part 1 §8 calls verse-level mapping
the single most important correctness property in the system.

quran.com serves this tafsir already keyed to a verse:

```
GET /api/v4/tafsirs/14/by_chapter/12?per_page=111
```

Each passage carries its own `verse_key`, so the mapping is a fact rather than an
inference. Issue #5 still has to do the chunking, but it is not re-deriving
where each passage belongs.

## The raw text

`corpus/tafsir/ar-tafsir-ibn-kathir-surah-12.md` — 111 ayahs, 181,298 bytes,
committed so a human can review the corpus without running anything.

The file is a snapshot of a third-party API response and is regenerated with
`-check` in CI-style checks rather than at build or ingest time.

### Cleaning: tags dropped, text kept

The API returns HTML. Only `<p>` and `<span>` appear, and the spans are not
decoration — they carry meaning:

| Class | Count | Content |
|---|---:|---|
| `blue` | 559 | inline asides, mostly narrative introductions (`عن ابن عباس قال :`) |
| `arabic qpc-hafs` | 394 | the Quranic quotations being explained |
| `red` | 124 | quoted hadith and other books |
| `reference brown` | 91 | bracketed citations (`[ الزمر : 23 ]`) |

Part 1 §7 is explicit that footnotes and citations are structure rather than
noise. So only the markup is removed and the inner text is kept: dropping the
text along with the tag would delete Ibn Kathir's references, and keeping the
tags would push markup into the embedded text. `&amp;`-style entities are
unescaped *after* tag removal so an entity that expands to something tag-shaped
is not re-read as markup.

### Things a reviewer should know about this text

- **12:1 is not word-by-word exegesis.** It opens with the surah preamble
  (`تفسير سورة يوسف [ وهي مكية ]`) and the hadith on teaching Surat Yusuf. That is
  the canonical entry keyed to 12:1, but it is a different kind of commentary
  from the other 110.
- **Entry lengths range from 88 to 9,830 characters.** 12:3 and 12:106 carry long
  chains of narration. Issue #5 will need paragraph-level chunking for these; one
  chunk per ayah would produce two chunks that blow past any sensible context
  budget.
- **Coverage is Surah Yusuf only.** Other surahs would need a separate fetch.

## Registry records

Three documents, per Part 2 §24 and `docs/build-deploy-guide.md`:

| Collection | `_id` | Key fields |
|---|---|---|
| `sources` | `ibn-kathir-ar` | tier 1, `licensing_status: cleared`, `verification_status: unverified` |
| `source_versions` | `ibn-kathir-ar-qurancom-2026-10-04` | `edition`, `version_label`, `content_hash` |
| `tafsir_documents` | `ibn-kathir-ar-surah-12` | `source_version_id`, `raw_text_ref`, `surah_id: 12` |

The registry record itself lives in `corpus/sources/ibn-kathir-ar.json` rather
than in command flags, because `authority_tier`, `licensing_status`, and
`verification_status` are human judgements about a specific published work. In a
file they are version-controlled and a change to a licensing claim shows up as a
diff.

### `content_hash` is computed, not typed

`source_versions.content_hash` is the SHA-256 of the raw text file, derived by
`register_source` at write time. A hand-typed hash would be a claim rather than a
check. `-check` recomputes it, so an edit to the corpus text or a change
upstream is reported as a mismatch instead of going unnoticed.

## Two schema problems worth recording

**1. `licensing_status` has two incompatible vocabularies.** Part 1 §6 documents
the field as `public domain / licensed / pending rights clearance`, but
`SourceRegistry.IsUsable` compares against the literal string `"cleared"`. A
source with the documented value `public domain` would fail the gate forever with
no way to satisfy both. This record stores `cleared`, because that is what the
code compares, and keeps the descriptive basis in
`metadata.licensing_basis: "public domain"`. One of the two should be reconciled; the spec or the constant
is wrong.

**2. `source_versions` was not implemented.** The issue and Part 2 §24 both call
for a `source_versions` collection, but the code modelled the edition as a
`Source.Edition` *string*, which `ingestion.Service` then copies into every
chunk's `source_version`. There was no collection to write to. This adds the
collection as the spec describes it and sets `Source.Edition` as well, since that
is what the pipeline actually reads.

## The gate still blocks, on purpose

`verification_status` is `unverified` because that is what issue #3 asks for, and
it is the reason nothing can be retrieved yet. `IsUsable` requires
`verification_status == "verified"` **and** `licensing_status == "cleared"`, so
this source is currently unusable. That was verified directly against the real
record: blocked as registered, usable once verification alone is flipped —
which also proves the record carries every other field the gate reads — and
blocked again if licensing is withdrawn.

`tafsir_chunks` is empty and `ingestion_status` is `not_started`. Ingestion is
issue #5.

## Not verified

The text has not been compared against a printed edition of Ibn Kathir. Part 1 §7
treats that as a human gate, not something to automate away, and it is why
`verification_status` is `unverified` rather than `verified`.