# Quran corpus validation — Surah 12 (Yusuf)

Issue #2. Regenerate with:

```
cd backend
MONGO_URI="mongodb://tadbor:tadbor_dev_password@localhost:27017" \
  go run ./cmd/seed_quran -surah 12 -check
```

## Source of record

`https://api.quran.com/api/v4/quran/verses/uthmani?chapter_number=12`

Stored with `corpus_version = "quran.com-v4-uthmani"`. quran.com was chosen
over the Tanzil-derived feeds because its API returns verses already keyed and
individually addressable, and it does not fold the Basmala into ayah 1.

## Independent cross-checks

Two, per the issue:

- `https://api.alquran.cloud/v1/surah/12/quran-uthmani` — Tanzil-derived text.
- Tanzil's own Uthmani export,
  `https://tanzil.net/pub/download/index.php?quranType=uthmani&outType=txt&agree=true`,
  the authority the issue names. Used as tiebreaker.

Comparison is by letter key: harakat, tatweel, superscript alef, and
alef/ya/ta-marbuta spelling variants are folded so that only genuine textual
differences register. Both secondary sources fold the Basmala into ayah 1, so it
is stripped before comparing — otherwise the only ayah 1 difference would be
an artefact of the feed, not of the text.

## Result

```
  stored ayahs            111
  byte-identical to source 111
  content hashes verified  111
  cross-source agreements  109
  mismatches               0
  bad hashes               0
  cross-source differences 2

  PASS: every stored ayah is byte-identical to quran.com-v4-uthmani and re-hashes correctly.
```

- **Count:** 111 (`countDocuments({surah_id: 12})`).
- **Integrity:** all 111 stored `text_uthmani` values are byte-for-byte the
  source text, and each `content_hash` is SHA-256 of the text it sits beside.
- **Spot check:** this exceeds the 10-ayah minimum, because the loader
  cross-checks all 111 on every run rather than sampling.

### The two differences

Both are defects in alquran.cloud, not in this corpus:

| Ayah | Resolution |
|---|---|
| 39 | alquran.cloud disagrees; Tanzil agrees with quran.com |
| 41 | alquran.cloud disagrees; Tanzil agrees with quran.com |

Since Tanzil — the source the issue names — sides with quran.com on both, the
stored text stands and no change was made.

## The verifier can fail

A check that cannot fail proves nothing, so corruption was injected into a
throwaway database and the verifier was confirmed to catch each case and exit
non-zero:

| Injected fault | Reported as |
|---|---|
| Altered `text_uthmani` | `STORED TEXT MISMATCH` |
| Stale `content_hash` | `CONTENT HASH does not match stored text` |
| Deleted ayah row | `STORED TEXT MISMATCH not stored` |
| Leftover row beyond the surah | named explicitly, e.g. `not verses of this surah ... [112]` |

A duplicated ayah row is rejected outright by the unique index on
`{surah_id, ayah_number}`, and the verifier aborts if a duplicate is found.

## Fields stored

`surah_id`, `ayah_number`, `text_uthmani` (verbatim), `text_simple` (search
key: marks stripped, spelling variants folded), `corpus_version`,
`content_hash` (SHA-256 of `text_uthmani`).

`text_simple` is a derived search key only. Nothing displays it as scripture;
all rendering uses `text_uthmani`.

## Effect on thematic retrieval

Before issue #2, `text_uthmani` was empty, so a thematic query had no text to
embed and retrieval could only return `GAP_INSUFFICIENT_EVIDENCE`. With the
corpus in place, a throwaway database holding these 111 real ayahs plus one
synthetic thematic chunk moved all 111 ayahs to `NEEDS_REVIEW_THEMATIC` —
proving the ayah text is read, embedded, and matched end to end. The synthetic
chunk existed only in that throwaway database, which was dropped; no synthetic
Tafsir was ever written to `tadbor`.

Issue #7 remains blocked for the separate reason that no Tafsir source has been
approved and ingested (issue #3). See `docs/retrieval-validation.md`.
