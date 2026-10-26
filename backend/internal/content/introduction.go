package content

import "time"

// SurahIntroduction represents a reviewed surah introduction
type SurahIntroduction struct {
	ID                    string              `bson:"_id,omitempty" json:"id"`
	SurahID               int                 `bson:"surah_id" json:"surah_id"`
	Style                 string              `bson:"style" json:"style"`
	Level                 string              `bson:"level" json:"level"`
	Sections              []Section           `bson:"sections" json:"sections"`
	SourceRefs            []string            `bson:"source_refs" json:"source_refs"`
	Attributions          []SourceAttribution `bson:"-" json:"attributions"`
	DifferingReports      []Claim             `bson:"differing_reports,omitempty" json:"differing_reports,omitempty"`
	ReviewStatus          string              `bson:"review_status" json:"-"`
	SourceVersionSnapshot string              `bson:"source_version_snapshot" json:"-"`
	PromptVersion         string              `bson:"prompt_version,omitempty" json:"-"`
	ModelVersion          string              `bson:"model_version,omitempty" json:"-"`
	CreatedAt             time.Time           `bson:"created_at" json:"-"`
	UpdatedAt             time.Time           `bson:"updated_at" json:"-"`
}

type Section struct {
	Type       string   `bson:"type" json:"type"` // classification|context|themes|stories|asbab
	Title      string   `bson:"title" json:"title"`
	Content    string   `bson:"content" json:"content"`
	SourceRefs []string `bson:"source_refs" json:"source_refs"`
	Claims     []Claim  `bson:"claims" json:"claims"`
}

type Claim struct {
	Text       string   `bson:"text" json:"text"`
	SourceRefs []string `bson:"source_refs" json:"source_refs"`
}

type SourceAttribution struct {
	SourceID      string   `json:"source_id"`
	Title         string   `json:"title"`
	Author        string   `json:"author"`
	Edition       string   `json:"edition,omitempty"`
	SourceVersion string   `json:"source_version"`
	SourceRefs    []string `json:"source_refs"`
}
