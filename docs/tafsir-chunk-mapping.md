# Tafsir chunk mapping — Surah 12, ayahs 1–111

Issue #5. Written 2026-10-04.

Issue #5 asks for verse-level chunking of a Tafsir corpus with correct
`chunk_ayah_mappings`, so that querying the mapping table for any ayah of Surah
Yusuf returns at least one chunk, and no chunk exists without a mapping.

This document records what was built, what the mapping actually is, and — just as
important — what could not be proven about it.

## Result

| Check | Result |
|---|---|
| Ayahs with at least one mapped chunk | **111 / 111** |
| Gaps | **0** |
| Chunks | 454 |
| Passages (whole commentary sections) | 111 |
| Mappings written | 454 — one per chunk |
| Mapping table vs chunk collection | **no drift** |
| Entries no automatic signal could corroborate | 19 — all read, all correct |

Source: `ibn-kathir-ar`, snapshot `ibn-kathir-ar-qurancom-2026-10-04`
(edition `quran.com/ar-tafsir-ibn-kathir`, quran.com resource 14). Raw text is
committed at `corpus/tafsir/ar-tafsir-ibn-kathir-surah-12.md` (issue #3, commit
`a474646`).

## Where the mapping comes from

The mapping is **not** inferred from the Arabic. `fetch_tafsir` writes one
`## surah:ayah` heading per entry, and those headings are keys the upstream API
supplied — so which verse a passage belongs to is a fact carried by the corpus,
not a guess made by a heuristic. `build_manifest` reads those headings and writes
them into the manifest; the chunker consumes them and never infers a mapping
(Part 1 §9 is explicit that it must not).

The tool refuses to build a manifest that is not a complete cover of the surah:
a missing heading fails the build rather than producing a manifest with a silent
gap. `build_manifest -check` re-derives the manifest from the corpus file and
fails if the committed manifest has diverged, so a hand edit that redirected a
passage to a different verse cannot pass unnoticed.

```
cd backend
go run ./cmd/build_manifest -source ibn-kathir-ar -surah 12 -check
```

## What was built

### `chunk_ayah_mappings` collection

Part 1 §8 names a mapping table separate from the chunk documents. It did not
exist; chunks only denormalized `surah_id`, `ayah_start`, `ayah_end`, and
`mapping_type`. That denormalized copy stays, because `internal/retrieval`
projects those fields directly and cannot join on the hot path — but the mapping
table now exists as the thing to query when auditing a surah's coverage.

`_id` is the chunk id. Part 1 §8 gives a chunk exactly one mapping, so this makes
"a chunk is mapped twice" impossible by construction rather than by convention.

Both sides are written from the same `[]Chunk` slice in the same step
(`internal/ingestion/mappings.go`, `service.go`), and pruning takes a chunk's
mapping with it, so they cannot drift through normal operation.
`Store.VerifyMappingsConsistent` re-checks after the fact and reports the three
shapes drift can take:

- a chunk with no mapping row
- a mapping row whose chunk is gone
- a mapping row whose verse range disagrees with its chunk's denormalized range

### `backend/cmd/build_manifest`

Builds `corpus/manifests/ibn-kathir-ar-surah-12.json` from the corpus file and the
registry record. The corpus path comes from the registry's `raw_text_ref` rather
than a filename convention, so the manifest is built from the text the reviewed
registry record names. `-check` verifies the committed manifest still matches.

### `backend/cmd/check_coverage`

Answers issue #5's two acceptance questions — is every ayah mapped, and have the
two sides drifted — and adds the part a mapping table cannot answer by itself:
whether each passage reads like commentary on the verse it is keyed to.

### `ingest -allow-unverified` (preview only)

Every source starts unverified, which made issue #5 unworkable: the gate refused
even a preview, so a manifest's mapping could not be reviewed before anyone
approved the text. The flag allows a **preview** of an unapproved source and is
refused when combined with `-live`, so it cannot become a way to write unverified
content. The live gate in `Service.IngestSource` enforces the same rule
independently of the CLI, and both are tested.

## How the mapping was checked

The corpus was ingested into a throwaway database and checked there. The real
`tadbor` database was never written to, and the probe database was dropped.

Three independent signals were used, because one signal cannot carry the claim:

| Signal | What it catches | Why it is not enough alone |
|---|---|---|
| `quoted_run` — longest run of the verse's own words appearing consecutively in the passage | verbatim quotation | fails on near-verbatim quotes with different spelling |
| `introduced` — a stock mufassir opening (`يخبر تعالى`, `يقول تعالى`) | commentary that paraphrases | only present in 36 of 111 entries |
| `quoted` — share of the verse's 8-rune shingles present in the passage | near-verbatim quotation | shingle overlap can come from a similarly-worded verse |

A mapping is flagged only when **all three** fail. Each signal has a known failure
mode, so none is allowed to decide on its own.

Building these signals surfaced two things worth recording:

- **Ibn Kathir quotes without hamza.** `LexicalForm` deliberately preserves
  letter shapes, so `أَبَاهُ` never matched `اباه` and 72 of 111 entries looked
  uncorroborated. Match-only orthography folding (`cmd/check_coverage`) fixed it.
  The folding never touches stored text, the embedding, or anything a reader
  sees — hamza is meaning-bearing in Qur'anic text, the same reasoning Part 1 §7
  gives for diacritics.
- **Short verses are quoted with altered letters.** The ayahs collection reads
  `انا انزلنه قرءنا عربيا لعلكم تعقلون`; the 12:2 entry writes
  `( إنا أنزلناه قرآنا عربيا لعلكم تعقلون )`. An inserted alif and a rewritten
  `ء` break every exact word match, which is what the shingle signal exists for.

### The 19 entries no signal could corroborate

None of the three signals can prove a mapping right: a passage that paraphrases
its verse shares no wording with it, and Ibn Kathir does that constantly. Those 19
were therefore read.

**All 19 are correctly keyed.** Examples:

- **12:61** — verse `قالوا سنرود عنه اباه وانا لفعلون`; the entry glosses it as
  `أي : سنحرص على مجيئه إليك` ("that is, we will strive to bring him to you"),
  with no shared wording at all. Zero lexical overlap, correct mapping.
- **12:91** — entry explains `لخطين` as `وأقروا له بأنهم أساءوا إليه وأخطئوا في حقه`.
- **12:43** — entry opens `هذه الرؤيا من ملك مصر`, glossing the dream verse.
- **12:101** — the verse is a prayer and the entry opens `هذا دعاء من يوسف الصديق`.
- **12:70** — quotes `( أيتها العير إنكم لسارقون )`, spelled differently from the
  ayah.

This is the honest limit of the check: it establishes that no mapping is
*contradicted*, and that the 19 weakest are correct on reading. It does not
automatically prove the other 92, only that they show a signal of quotation.

## Open items

Embeddings for these 454 chunks are issue #6; see `docs/tafsir-embeddings.md`.
`tafsir_chunks` is still empty in the real database either way, for the reason below.

### The source is still unverified, so this is not yet live

`verification_status` on `ibn-kathir-ar` remains `unverified`, by design from
issue #3. `SourceRegistry.IsUsable` therefore refuses it, retrieval filters on
`source_verified`, and **issue #7 stays blocked**: with the real source unapproved,
a live ingest writes nothing and the retrieval endpoint keeps returning
`GAP_INSUFFICIENT_EVIDENCE`.

The coverage numbers above were produced against a probe database with the source
temporarily marked verified. That was a probe, not an approval — no human has yet
compared this text against a printed edition, and that decision is still open.

### Chunks are keyed by the pinned snapshot, not the edition

This was found during issue #5 and fixed in the same series. `IngestSource`
defaulted a document's `source_version` to `Source.Edition`, so chunks carried
`quran.com/ar-tafsir-ibn-kathir` — an edition slug, not a snapshot. Two
snapshots of one edition therefore shared one `source_version`, so re-ingesting
corrected text would overwrite the original with nothing recording which bytes
either came from, and the registry's SHA-256 pin never reached the chunks.

Chunks are now keyed by the `source_versions` document id
(`ibn-kathir-ar-qurancom-2026-10-04`) and carry its `content_hash` as
`source_version_hash`. `sources.current_version` is the pointer that makes the
choice explicit; `SourceRegistry.Version` accepts a single registered version with
no pointer (the state issue #3 left the registry in, and it is unambiguous) but
refuses to choose between several. A source with no registered version is an
error rather than a fallback to the edition.

`TestTwoSnapshotsOfOneEditionProduceDistinctChunks` is the regression test: the
same edition digitised twice now yields two sets of chunks instead of one
overwritten.

### 12:1 is surah-level content keyed to a verse

The entry for 12:1 is `تفسير سورة يوسف` — the surah preamble plus Ibn Kathir's
hadith on teaching Surat Yusuf. The API keys it to 12:1, so it is labelled
`single_ayah`; it is not word-by-word exegesis of the verse. Relabelling it
`surah_level` would mean overruling the source's own keying, which is a decision
for a human, not for the manifest builder. A reader asking about 12:1 will get the
preamble commentary as their primary evidence.

## Reproducing

```bash
# 1. manifest matches the committed corpus
cd backend
go run ./cmd/build_manifest -source ibn-kathir-ar -surah 12 -check

# 2. chunk plan, without a provider or a write (works while unverified)
set -a && . ../.env && set +a
go run ./cmd/ingest -source ibn-kathir-ar \
  -manifest ../corpus/manifests/ibn-kathir-ar-surah-12.json -allow-unverified

# 3. coverage, gaps, and drift — needs an ingested database
go run ./cmd/check_coverage -source ibn-kathir-ar -surah 12
go run ./cmd/check_coverage -source ibn-kathir-ar -surah 12 -only-flagged
```

`check_coverage` exits 1 only for a gap or for drift. An unapproved source and an
uncorroborated mapping both exit 0: those are states for a human to resolve, not
failures this tool has standing to declare.