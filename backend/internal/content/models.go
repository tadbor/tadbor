package content

// Explanation mirrors the "explanations" collection. The public API only
// ever returns documents with ReviewStatus == "published" — see
// docs/rag-architecture-part1.md §4 and §20.
type Explanation struct {
	ID            string   `bson:"_id,omitempty" json:"id"`
	SurahID       int      `bson:"surah_id" json:"surah_id"`
	AyahStart     int      `bson:"ayah_start" json:"ayah_start"`
	AyahEnd       int      `bson:"ayah_end" json:"ayah_end"`
	Style         string   `bson:"style" json:"style"` // "simplified_ar" | "egyptian_ar" | "en"
	Level         string   `bson:"level" json:"level"` // "beginner" | "tadabbur" | "advanced"
	Text          string   `bson:"text" json:"text"`
	SourceRefs    []string `bson:"source_refs" json:"source_refs"`
	ReviewStatus  string   `bson:"review_status" json:"-"` // never exposed to the reader
	SourceVersion string   `bson:"source_version_snapshot" json:"-"`
}

type Citation struct {
	SourceTitle string `bson:"source_title" json:"source_title"`
	Author      string `bson:"author" json:"author"`
	Edition     string `bson:"edition,omitempty" json:"edition,omitempty"`
}
