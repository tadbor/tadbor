# Addendum — Recitation Audio

Adds audio recitation as a new content type, separate from Tafsir. Unlike
explanations, recitation audio needs no human review or grounding pipeline —
it's verified source files per `(reciter, surah, ayah)`, not generated content.

## Reciters (first pass)

Four well-known reciters, matching the app's existing "start small, expand
later" pattern:

- Mishary Rashid Alafasy
- Abdul Basit Abdus Samad
- Saad Al-Ghamdi
- Maher Al-Muaiqly

## Source

Per-ayah and per-surah MP3s pulled from a public Quran-audio source (e.g.
EveryAyah.com or quran.com's audio CDN) at ingestion time — not recorded or
generated. Verify each reciter's usage terms before shipping; this is the
standard source pool most Quran apps already use for exactly this purpose.

## Data model

```
reciters(id, name, style_note)
recitations(id, reciter_id, surah_id, ayah_number, audio_url, duration_seconds)
```

`audio_url` points at object storage (Supabase Storage free tier or similar)
— **audio files themselves never live in MongoDB**, only the reference. This
mirrors how `explanations` references `source_refs` rather than embedding
source text.

## API

`GET /surahs/:surahId/recitations?reciter=<id>` — returns per-ayah audio URLs
for the requested reciter, alongside the existing `/ayahs` and `/explanations`
endpoints. Public, read-only — same trust boundary as `ContentService`.

## Offline behavior

Mirrors the reference pattern of downloading audio surah-by-surah for offline
listening: the mobile/web client caches downloaded files locally once fetched
— the backend only ever serves the reference URL, it doesn't manage per-user
download state.

## What this does NOT touch

No change to the review gate, generation contract, or retrieval pipeline —
recitation is a fully separate, simpler content type that sits next to
Tafsir-grounded explanations, not inside that pipeline.
