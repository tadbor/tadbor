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

### `current_version` is what ingestion keys on

`sources.current_version` names the `source_versions` document this source's
content should be ingested as, and every chunk carries that document's
`content_hash` as `source_version_hash`. Registering a version record sets the
pointer, so the snapshot being ingested is always explicit.

This replaced an earlier behaviour where chunks were keyed by `sources.edition`.
An edition is not a snapshot: two digitisations of one edition would have shared
one `source_version` and overwritten each other, with the hash that actually pins
the text never reaching the chunks. A source with two registered versions and no
pointer is now refused rather than guessed at.

`tafsir_chunks` is empty and `ingestion_status` is `not_started` in the real
`tadbor` database. Ingestion was issue #5 and has been carried out against a
throwaway probe database: 111 of 111 ayahs mapped, 454 chunks, no drift. Nothing
was written to `tadbor`, so `ingestion_status` is correctly still `not_started`.
See `docs/tafsir-chunk-mapping.md`.

Until verification is done, the only way to exercise this source is
`ingest -allow-unverified`, which is accepted for a preview only and refused when
combined with `-live`. That is what made issue #5 reviewable: the mapping had to
be inspectable before anyone approved the text, and the gate previously refused
even a preview.

## Not verified

The text has not been compared against a printed edition of Ibn Kathir. Part 1 §7
treats that as a human gate, not something to automate away, and it is why
`verification_status` is `unverified` rather than `verified`.
## Verifying a source against a printed edition

Registration can check a licence and name an edition. It cannot tell you whether
the text faithfully reproduces the edition it claims to be — that is a human
judgement against paper, and nothing in the pipeline should pretend otherwise.

`cmd/tafsir_digest` builds the evidence for that judgement:

```
cd backend
go run ./cmd/tafsir_digest \
  -source ibn-kathir-ar \
  -manifest ../corpus/manifests/ibn-kathir-ar-surah-12.json \
  -out ../docs/reviews/ibn-kathir-ar-surah-12.md
```

The sheet carries each passage in full next to the ayah it is keyed to, with the
source and verse-corpus hashes, the snapshot id, and a checklist. It reads the
committed manifest rather than the chunk collection, because a source has to be
verifiable *before* it is ingested; that also lets the sheet reproduce the review
queue without trusting a preview database.

By default it contains only the entries no automatic signal could vouch for —
19 of surah Yusuf's 111. `-all` emits everything, `-ayahs 1,34,43` a chosen few,
and `-verify` prints just the provenance header so hashes can be checked before
anyone spends time reading.

The signals come from `internal/textverify`, shared with `cmd/check_coverage`, so
both tools agree on what needs a human. They measure evidence, not correctness: a
low score means "read this one", never "this one is wrong". Paraphrase is a
legitimate way to write Tafsir, which is why a 19-entry sheet out of 111 is a
plausible result rather than a suspicious one.
