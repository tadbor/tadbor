package ingestion

// SourceType defines the type of source
type SourceType string

const (
	SourceTypeTafsir    SourceType = "tafsir"
	SourceTypeHistory   SourceType = "history"
	SourceTypeAsbab     SourceType = "asbab"
	SourceTypeHadith    SourceType = "hadith"
	SourceTypeOther     SourceType = "other"
)

// ExtendedSource extends Source for non-tafsir sources
type ExtendedSource struct {
	Source
	SourceType      SourceType `json:"source_type"`
	Scope           string     `json:"scope"` // surah|general
	VerificationSheet string   `json:"verification_sheet,omitempty"`
}
