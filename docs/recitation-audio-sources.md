# Recitation audio: where the URLs come from, and what is still unresolved

## What is loaded

`make audio` runs `backend/cmd/ingest_recitations -live`, which writes **444
`recitations` documents** for Surah Yusuf — 111 ayahs × 4 reciters — and stores
only the reference, never the audio itself:

```
https://everyayah.com/data/<reciter folder>/<surah:03d><ayah:03d>.mp3
```

| `reciter_id` | Reciter | EveryAyah folder |
| --- | --- | --- |
| `alafasy` | Mishary Rashid Alafasy | `Alafasy_128kbps` |
| `abdulbasit` | Abdul Basit Abdus Samad | `Abdul_Basit_Murattal_192kbps` |
| `ghamdi` | Saad Al-Ghamdi | `Ghamadi_40kbps` |
| `muaiqly` | Maher Al-Muaiqly | `MaherAlMuaiqly128kbps` |

Those folder names are not derivable from the reciter ids in the database, and
two of the four obvious guesses are wrong — `Ghamadi_40kbps` has no `Al_`
prefix and `MaherAlMuaiqly128kbps` has neither separators nor a bitrate suffix.
They were read off everyayah.com's own `/data` index. A wrong guess produces a
404 indistinguishable from a recording that does not exist, which is the
difference between a missing verse and a broken reciter.

## Every URL is checked before it is stored

A recitation document that exists but does not play is the exact failure this
issue is closing, so inserting 444 unverified URLs would reproduce it at 444
times the size — and unlike the old `example.com` placeholders it would look
like real data, so nothing downstream would flag it.

- HEAD first, ranged GET as fallback: some static hosts answer HEAD with 405,
  and recording those as missing would drop a reciter that is fine.
- The response must be `audio/*`, not merely 200. An HTML error page served
  with 200 is not a recording.
- Anything unreachable is **skipped and named** (`muaiqly:37`), and the command
  exits non-zero. A partial load is a failure, not a warning.
- 250 ms between requests by default. This is a volunteer-run site serving a lot
  of people; a parallel burst is the fastest way to get the whole directory
  rate-limited for everyone.

## Two failure modes that had to be closed explicitly

**The placeholders came back.** `scripts/seed_recitation.go` used to write 8
`example.com` recitations for ayahs 1–2, and it still ran as part of `make seed`
— so re-seeding overwrote real audio with URLs that do not play. It now writes
the reciters only, and points at `cmd/ingest_recitations` for audio. `make
audio-check` fails on any URL containing `example.com`, `placeholder` or
`localhost`.

**Nothing stopped a double load.** There was no index on the collection beyond
`_id`, so two runs would both insert and the surah page would play the same ayah
twice. `cmd/ingest_recitations` creates a unique index on
`(surah_id, reciter_id, ayah_number)`, which is also what makes its upserts
upserts.

## Verification

```
$ make audio-check
{"surah_id":12,"reciters":4,"ayahs":111,"planned_urls":444,
 "stored_recitations":444,"missing_verses":0,"placeholder_urls":0,"duplicate_documents":0}
```

The gate was checked by breaking it: deleting `muaiqly:37` makes `make
audio-check` exit non-zero with `1 missing verses`, and re-running `make audio`
restores it.

Playback spot-check — 6 ayahs (1, 9, 37, 55, 83, 111) × 4 reciters, 24 URLs,
each fetched through the API's URL and inspected:

```
206 Partial Content | audio/mpeg | 2048 bytes | frame sync fffa/fffb/fff2
```

All 24 return real MPEG frames, and the frame headers differ per reciter as the
bitrate folders predict. `http://localhost:3000/surah/12` renders 111
`<audio controls>` elements pointing at EveryAyah URLs.

## Unresolved: licensing

Issue #14 asks to *"confirm each reciter's audio terms allow this kind of use in
an app."* **This is not resolved, and nothing here decides it.**

everyayah.com publishes no licence, no terms page, and no per-reciter rights
statement. Checked and found empty or absent: the `/data` index, the homepage,
`/terms`, `/about`, `/faq`. The files are served without charge, which is not a
grant of redistribution rights — and every reciter here is living or recently
deceased, with an estate that holds those rights.

What has been decided is only the shape that keeps the answer cheap:

- **Direct linking, not re-hosting.** Nothing is copied into MongoDB or object
  storage. Re-hosting would be redistributing files this project has not
  established a right to redistribute.
- The reference is stored, never the bytes — the same rule explanations follow
  with `source_refs` (see `docs/ADDENDUM-recitation-audio.md`).

So the question narrows to whether a public app may hotlink these URLs, and it
is a question for a human who can weigh the distribution model against the
rights. It is not a code question and it should not be answered by whoever
happens to be running the ingestion.

**To answer it:** the reciters' own institutions and estates are the
authoritative source — Mishary Alafasy and Saad Al-Ghamdi via their
institutional channels, Abdul Basit Abdus Samad and Maher Al-Muaiqly likewise.
An alternative that removes the question entirely is a source with an explicit
redistribution licence.

## Note on mobile

`mobile/src/screens/SurahScreen.tsx` passes `audio.audio_url` straight to
`expo-av`, and `mobile/src/api/client.ts` fetches the same
`/surahs/:id/recitations?reciter=` endpoint the web player uses. Playback there
is therefore unverified by execution — no device was available — but it depends
on nothing beyond the URLs verified above. The remaining check is listening to
five ayahs per reciter in the running app, which is task 4 of issue #14.