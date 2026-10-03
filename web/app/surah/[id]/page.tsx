import { getAyahs, getExplanations, getReciters, getRecitations } from "../../api-client";

export default async function SurahPage({
  params,
  searchParams,
}: {
  params: { id: string };
  searchParams: { reciter?: string };
}) {
  const surahId = Number(params.id);
  const reciters = await getReciters();
  const activeReciter = searchParams.reciter || reciters[0]?.id;

  const [ayahs, explanations, recitations] = await Promise.all([
    getAyahs(surahId),
    getExplanations(surahId, "simplified_ar"),
    activeReciter ? getRecitations(surahId, activeReciter) : Promise.resolve([]),
  ]);

  const explanationByAyah = new Map(
    explanations.map((e) => [e.ayah_start, e] as const)
  );
  const audioByAyah = new Map(
    recitations.map((r) => [r.ayah_number, r] as const)
  );

  return (
    <main style={{ maxWidth: 700, margin: "0 auto", padding: "2rem" }}>
      {/* Reciter switcher — plain links so no client JS is needed */}
      <div style={{ display: "flex", gap: "0.5rem", marginBottom: "1.5rem", flexWrap: "wrap" }}>
        {(reciters).map((r) => (
          <a
            key={r.id}
            href={`/surah/${surahId}?reciter=${r.id}`}
            style={{
              fontSize: "0.75rem",
              fontWeight: 700,
              padding: "0.4rem 0.8rem",
              borderRadius: 999,
              textDecoration: "none",
              background: r.id === activeReciter ? "#151515" : "transparent",
              color: r.id === activeReciter ? "#fff" : "#151515",
              border: "1px solid #15151533",
            }}
          >
            {r.name}
          </a>
        ))}
      </div>

      {ayahs.map((ayah) => {
        const explanation = explanationByAyah.get(ayah.ayah_number);
        const audio = audioByAyah.get(ayah.ayah_number);
        return (
          <section key={ayah.ayah_number} style={{ marginBottom: "2rem" }}>
            {/* Quran text — visually separated, never mixed with explanation */}
            <div
              style={{
                border: "1px solid #ddd",
                padding: "1rem",
                borderRadius: 8,
                fontFamily: "serif",
                fontSize: "1.4rem",
                textAlign: "right",
                direction: "rtl",
              }}
            >
              {ayah.text_uthmani}
            </div>

            {audio && (
              <audio controls style={{ width: "100%", marginTop: "0.5rem" }} preload="none">
                <source src={audio.audio_url} />
              </audio>
            )}

            {explanation && (
              <div
                style={{
                  background: "#f7f7f7",
                  padding: "1rem",
                  borderRadius: 8,
                  marginTop: "0.5rem",
                }}
              >
                <div style={{ fontWeight: 600, marginBottom: "0.5rem" }}>
                  💡 المعنى المبسط
                </div>
                <p>{explanation.text}</p>
              </div>
            )}
          </section>
        );
      })}
    </main>
  );
}
