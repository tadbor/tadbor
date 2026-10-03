const API_URL = process.env.NEXT_PUBLIC_API_URL;

// Response shapes mirror the Go `json:"..."` tags one-for-one:
// Ayah -> backend/internal/quran/models.go, Reciter/Recitation ->
// backend/internal/recitation/service.go, Explanation ->
// backend/internal/content/models.go. Fields tagged `json:"-"` in Go
// (review_status, source_version_snapshot) are never exposed and are absent here.
export type Ayah = {
  id: string;
  surah_id: number;
  ayah_number: number;
  text_uthmani: string;
  text_simple: string;
  corpus_version: string;
  content_hash: string;
};

export type Reciter = {
  id: string;
  name: string;
  style_note?: string;
};

export type Recitation = {
  id: string;
  reciter_id: string;
  surah_id: number;
  ayah_number: number;
  audio_url: string;
  duration_seconds?: number;
};

export type Explanation = {
  id: string;
  surah_id: number;
  ayah_start: number;
  ayah_end: number;
  style: string;
  level: string;
  text: string;
  source_refs: string[];
};

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null;
}

function isAyah(value: unknown): value is Ayah {
  return (
    isRecord(value) &&
    typeof value.surah_id === "number" &&
    typeof value.ayah_number === "number" &&
    typeof value.text_uthmani === "string"
  );
}

function isReciter(value: unknown): value is Reciter {
  return isRecord(value) && typeof value.id === "string" && typeof value.name === "string";
}

function isRecitation(value: unknown): value is Recitation {
  return (
    isRecord(value) &&
    typeof value.reciter_id === "string" &&
    typeof value.surah_id === "number" &&
    typeof value.ayah_number === "number" &&
    typeof value.audio_url === "string"
  );
}

function isExplanation(value: unknown): value is Explanation {
  return (
    isRecord(value) &&
    typeof value.ayah_start === "number" &&
    typeof value.ayah_end === "number" &&
    typeof value.style === "string" &&
    typeof value.text === "string"
  );
}

// Every response is parsed as `unknown` and narrowed by a guard, so a backend
// field rename surfaces as a dropped row instead of an `any` that silently
// type-checks. Unparseable bodies degrade to an empty list rather than a 500.
async function fetchList<T>(path: string, isItem: (value: unknown) => value is T): Promise<T[]> {
  const res = await fetch(`${API_URL}${path}`, { cache: "no-store" });
  const data: unknown = await res.json().catch(() => null);
  if (!Array.isArray(data)) return [];
  return data.filter(isItem);
}

export async function getAyahs(surahId: number): Promise<Ayah[]> {
  return fetchList(`/surahs/${surahId}/ayahs`, isAyah);
}

export async function getExplanations(surahId: number, style: string): Promise<Explanation[]> {
  return fetchList(`/surahs/${surahId}/explanations?style=${style}`, isExplanation);
}

export async function getReciters(): Promise<Reciter[]> {
  return fetchList("/reciters", isReciter);
}

export async function getRecitations(surahId: number, reciterId: string): Promise<Recitation[]> {
  return fetchList(`/surahs/${surahId}/recitations?reciter=${reciterId}`, isRecitation);
}