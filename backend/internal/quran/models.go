package quran

// Ayah mirrors the "ayahs" collection. This corpus is treated as immutable
// once loaded and verified — see docs/rag-architecture-part1.md §5.
// Loaded once via scripts/seed_quran.go, never written to by the API.
type Ayah struct {
	ID            string `bson:"_id,omitempty" json:"id"`
	SurahID       int    `bson:"surah_id" json:"surah_id"`
	AyahNumber    int    `bson:"ayah_number" json:"ayah_number"`
	TextUthmani   string `bson:"text_uthmani" json:"text_uthmani"`
	TextSimple    string `bson:"text_simple" json:"text_simple"` // normalized, search-only — never displayed as "the Quran"
	CorpusVersion string `bson:"corpus_version" json:"corpus_version"`
	ContentHash   string `bson:"content_hash" json:"content_hash"`
}

type Surah struct {
	ID        int    `bson:"_id" json:"id"`
	Name      string `bson:"name" json:"name"`
	AyahCount int    `bson:"ayah_count" json:"ayah_count"`
}
