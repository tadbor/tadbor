const API_URL = process.env.NEXT_PUBLIC_API_URL;

export async function getAyahs(surahId: number) {
  const res = await fetch(`${API_URL}/surahs/${surahId}/ayahs`, { cache: "no-store" });
  return res.json();
}

export async function getExplanations(surahId: number, style: string) {
  const res = await fetch(`${API_URL}/surahs/${surahId}/explanations?style=${style}`, {
    cache: "no-store",
  });
  return res.json();
}

export async function getReciters() {
  const res = await fetch(`${API_URL}/reciters`, { cache: "no-store" });
  return res.json();
}

export async function getRecitations(surahId: number, reciterId: string) {
  const res = await fetch(`${API_URL}/surahs/${surahId}/recitations?reciter=${reciterId}`, {
    cache: "no-store",
  });
  return res.json();
}
