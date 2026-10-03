package ingestion

import (
	"errors"
	"fmt"

	"tadbor/backend/internal/embedding"
)

// Validation errors. A chunk that cannot be safely retrieved must never enter
// the index — Part 1 §10 is explicit that a chunk missing its verse mapping
// "should fail ingestion validation rather than enter the index with nulls".
var (
	ErrSourceNotUsable = errors.New("source is not verified and licensing-cleared")
	ErrEmptyDocument   = errors.New("document contains no sections with text")
)

// ValidateDocument checks everything needed to produce chunks, before any
// provider call is made. Fail here are free; fail after embedding are not.
func ValidateDocument(doc Document, src Source) error {
	if src.ID == "" {
		return errors.New("source id is required")
	}
	if doc.ID == "" {
		return errors.New("document id is required")
	}
	if doc.SourceVersion == "" {
		return errors.New("document source_version is required (edition or publication version)")
	}
	if doc.SurahID < 1 {
		return fmt.Errorf("document surah_id must be >= 1, got %d", doc.SurahID)
	}
	if !validContentType(doc.ContentType) {
		return fmt.Errorf("unknown content_type %q", doc.ContentType)
	}
	if doc.Language == "" {
		return errors.New("document language is required")
	}

	var withText int
	for i, sec := range doc.Sections {
		if trimmed(sec.Text) {
			withText++
			continue
		}
		if len(sec.Text) != 0 {
			return fmt.Errorf("section %d: text is whitespace only", i)
		}
	}
	if withText == 0 {
		return ErrEmptyDocument
	}
	return nil
}

// ValidateChunk enforces the mandatory metadata set from Part 1 §10 plus the
// provenance fields the pipeline depends on for idempotency and resume. It does
// not require an embedding — vector checks live in ValidateBatch, so a chunk can
// be structurally validated before it is ever sent to the provider.
func ValidateChunk(c Chunk) error {
	required := []struct {
		name  string
		value string
	}{
		{"id", c.ID},
		{"document_id", c.DocumentID},
		{"source_id", c.SourceID},
		{"source_version", c.SourceVersion},
		{"language", c.Language},
		{"content_type", c.ContentType},
		{"mapping_type", c.MappingType},
		{"text", c.Text},
		{"text_normalized", c.TextNormalized},
		{"content_hash", c.ContentHash},
		{"review_status", c.ReviewStatus},
	}
	for _, f := range required {
		if !trimmed(f.value) {
			return fmt.Errorf("chunk %s: %s is required", c.ID, f.name)
		}
	}
	if c.ContentHash != ContentHash(c.Text) {
		return fmt.Errorf("chunk %s: content_hash does not match text", c.ID)
	}
	if c.SurahID < 1 {
		return fmt.Errorf("chunk %s: surah_id must be >= 1, got %d", c.ID, c.SurahID)
	}
	if !validContentType(c.ContentType) {
		return fmt.Errorf("chunk %s: unknown content_type %q", c.ID, c.ContentType)
	}
	if c.AuthorityTier < 1 || c.AuthorityTier > 3 {
		return fmt.Errorf("chunk %s: authority_tier must be 1-3, got %d", c.ID, c.AuthorityTier)
	}
	return validateMappingRange(c.MappingType, c.AyahStart, c.AyahEnd)
}

// validateMappingRange enforces the verse-range rules that make retrieval's
// precedence order (exact → range → thematic → none, Part 1 §8) hold.
//
// Note the deliberate consequence for surah_level and thematic chunks: they keep
// ayah_start/ayah_end at 0. retrieval.exactMapping filters on
// ayah_start <= ayah && ayah_end >= ayah, and no real ayah number is <= 0, so
// those chunks can never satisfy an exact-verse lookup. They are reachable only
// through the explicit mapping_type filter. That is the intended ordering,
// achieved without changing the retrieval package.
func validateMappingRange(mappingType string, ayahStart, ayahEnd int) error {
	switch mappingType {
	case MappingSingleAyah:
		if ayahStart < 1 || ayahEnd != ayahStart {
			return fmt.Errorf("single_ayah requires ayah_start == ayah_end >= 1, got %d-%d", ayahStart, ayahEnd)
		}
	case MappingMultiAyah:
		if ayahStart < 1 || ayahEnd < ayahStart {
			return fmt.Errorf("multi_ayah requires 1 <= ayah_start <= ayah_end, got %d-%d", ayahStart, ayahEnd)
		}
	case MappingSurahLevel, MappingThematic:
		if ayahStart != 0 || ayahEnd != 0 {
			return fmt.Errorf("%s must not claim a verse range (got %d-%d)", mappingType, ayahStart, ayahEnd)
		}
	default:
		return fmt.Errorf("unknown mapping_type %q", mappingType)
	}
	return nil
}

// ValidateBatch pairs chunks with the vectors returned for them. Alignment is the
// whole risk of a batched call: a short, reordered, or malformed response would
// otherwise attach the wrong verse's meaning to the wrong chunk.
func ValidateBatch(chunks []Chunk, vectors [][]float64) error {
	if len(chunks) != len(vectors) {
		return fmt.Errorf("%w: got %d vectors for %d chunks", embedding.ErrUnexpectedShape, len(vectors), len(chunks))
	}
	for i := range chunks {
		if err := ValidateChunk(chunks[i]); err != nil {
			return err
		}
		if err := embedding.ValidateVector(vectors[i], embedding.ExpectedDimensions); err != nil {
			return fmt.Errorf("chunk %s: %w", chunks[i].ID, err)
		}
	}
	return nil
}

func validContentType(ct string) bool {
	switch ct {
	case ContentTypeTafsir, ContentTypeHistoricalContext, ContentTypeLexical, ContentTypeThematic:
		return true
	default:
		return false
	}
}
