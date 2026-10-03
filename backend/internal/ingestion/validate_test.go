package ingestion

import (
	"errors"
	"math"
	"strings"
	"testing"

	"tadbor/backend/internal/embedding"
)

// unitVector is a deterministic unit-length vector, standing in for a provider
// response. Real vectors come from the live smoke test only.
func unitVector() []float64 {
	v := make([]float64, embedding.ExpectedDimensions)
	scale := 1 / math.Sqrt(embedding.ExpectedDimensions)
	for i := range v {
		v[i] = scale
	}
	return v
}

func validChunk() Chunk {
	c := Chunk{
		ID:             ChunkID("src", "v1", ChunkKey{AyahStart: 1, AyahEnd: 1, MappingType: MappingSingleAyah, ContentHash: ContentHash("نص")}, 0),
		DocumentID:     "doc-1",
		SourceID:       "src",
		SourceVersion:  "v1",
		SurahID:        12,
		AyahStart:      1,
		AyahEnd:        1,
		MappingType:    MappingSingleAyah,
		Text:           "نص",
		TextNormalized: "نص",
		ContentHash:    ContentHash("نص"),
		ContentType:    ContentTypeTafsir,
		Language:       "ar",
		AuthorityTier:  1,
		SourceVerified: true,
		ReviewStatus:   VerificationVerified,
		EmbeddingModel: embedding.ModelID,
		Embedding:      unitVector(),
		EmbeddingHash:  "hash",
	}
	c.EmbeddingDimensions = len(c.Embedding)
	c.EmbeddingNormalized = true
	return c
}

func TestValidateChunkAcceptsAWellFormedChunk(t *testing.T) {
	if err := ValidateChunk(validChunk()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// Part 1 §10: a chunk that cannot be safely retrieved must not enter the index.
func TestValidateChunkRequiresMandatoryMetadata(t *testing.T) {
	for _, tc := range []struct {
		name    string
		mutate  func(*Chunk)
		message string
	}{
		{"id", func(c *Chunk) { c.ID = "" }, "id is required"},
		{"document_id", func(c *Chunk) { c.DocumentID = "" }, "document_id is required"},
		{"source_id", func(c *Chunk) { c.SourceID = "" }, "source_id is required"},
		{"source_version", func(c *Chunk) { c.SourceVersion = "" }, "source_version is required"},
		{"language", func(c *Chunk) { c.Language = "" }, "language is required"},
		{"content_type", func(c *Chunk) { c.ContentType = "" }, "content_type is required"},
		{"mapping_type", func(c *Chunk) { c.MappingType = "" }, "mapping_type is required"},
		{"text", func(c *Chunk) { c.Text = "   " }, "text is required"},
		{"text_normalized", func(c *Chunk) { c.TextNormalized = "" }, "text_normalized is required"},
		{"content_hash", func(c *Chunk) { c.ContentHash = "" }, "content_hash is required"},
		{"review_status", func(c *Chunk) { c.ReviewStatus = "" }, "review_status is required"},
		{"surah_id", func(c *Chunk) { c.SurahID = 0 }, "surah_id must be >= 1"},
		{"authority_tier too high", func(c *Chunk) { c.AuthorityTier = 4 }, "authority_tier must be 1-3"},
		{"authority_tier unset", func(c *Chunk) { c.AuthorityTier = 0 }, "authority_tier must be 1-3"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := validChunk()
			tc.mutate(&c)
			err := ValidateChunk(c)
			if err == nil {
				t.Fatal("expected an error")
			}
			if !strings.Contains(err.Error(), tc.message) {
				t.Fatalf("error %q does not mention %q", err, tc.message)
			}
		})
	}
}

func TestValidateChunkRejectsTamperedText(t *testing.T) {
	c := validChunk()
	c.Text = "نص مختلف"

	if err := ValidateChunk(c); err == nil || !strings.Contains(err.Error(), "content_hash does not match") {
		t.Fatalf("expected a content hash mismatch, got %v", err)
	}
}

func TestValidateChunkRejectsIncoherentVerseRanges(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*Chunk)
	}{
		{"single ayah spanning a range", func(c *Chunk) { c.AyahEnd = 3 }},
		{"single ayah at zero", func(c *Chunk) { c.AyahStart, c.AyahEnd = 0, 0 }},
		{"multi ayah inverted", func(c *Chunk) {
			c.MappingType = MappingMultiAyah
			c.AyahStart, c.AyahEnd = 6, 4
		}},
		{"thematic claiming a verse", func(c *Chunk) { c.MappingType = MappingThematic }},
		{"surah_level claiming a verse", func(c *Chunk) { c.MappingType = MappingSurahLevel }},
		{"unknown mapping type", func(c *Chunk) { c.MappingType = "guess" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := validChunk()
			tc.mutate(&c)
			if err := ValidateChunk(c); err == nil {
				t.Fatal("expected an error")
			}
		})
	}
}

func TestValidateChunkAcceptsEveryMappingType(t *testing.T) {
	for _, tc := range []struct {
		mapping            string
		ayahStart, ayahEnd int
	}{
		{MappingSingleAyah, 4, 4},
		{MappingMultiAyah, 4, 6},
		{MappingSurahLevel, 0, 0},
		{MappingThematic, 0, 0},
	} {
		t.Run(tc.mapping, func(t *testing.T) {
			c := validChunk()
			c.MappingType, c.AyahStart, c.AyahEnd = tc.mapping, tc.ayahStart, tc.ayahEnd
			if err := ValidateChunk(c); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

// A batched call's main risk is misalignment: attaching one verse's meaning to
// another verse's chunk.
func TestValidateBatchDetectsMisalignment(t *testing.T) {
	t.Run("too few vectors", func(t *testing.T) {
		err := ValidateBatch([]Chunk{validChunk(), validChunk()}, [][]float64{unitVector()})
		if !errors.Is(err, embedding.ErrUnexpectedShape) {
			t.Fatalf("expected ErrUnexpectedShape, got %v", err)
		}
	})
	t.Run("too many vectors", func(t *testing.T) {
		err := ValidateBatch([]Chunk{validChunk()}, [][]float64{unitVector(), unitVector()})
		if !errors.Is(err, embedding.ErrUnexpectedShape) {
			t.Fatalf("expected ErrUnexpectedShape, got %v", err)
		}
	})
	t.Run("wrong vector width", func(t *testing.T) {
		err := ValidateBatch([]Chunk{validChunk()}, [][]float64{make([]float64, 16)})
		if err == nil {
			t.Fatal("expected an error")
		}
	})
	t.Run("unnormalized vector", func(t *testing.T) {
		bad := make([]float64, embedding.ExpectedDimensions)
		for i := range bad {
			bad[i] = 3
		}
		if err := ValidateBatch([]Chunk{validChunk()}, [][]float64{bad}); err == nil {
			t.Fatal("expected an error")
		}
	})
	t.Run("invalid chunk fails the batch", func(t *testing.T) {
		c := validChunk()
		c.ContentHash = "tampered"
		if err := ValidateBatch([]Chunk{c}, [][]float64{unitVector()}); err == nil {
			t.Fatal("expected an error")
		}
	})
	t.Run("aligned batch passes", func(t *testing.T) {
		chunks := []Chunk{validChunk(), validChunk()}
		if err := ValidateBatch(chunks, [][]float64{unitVector(), unitVector()}); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

func TestValidateDocumentRejectsIncompleteMetadata(t *testing.T) {
	src := usableSource()
	if err := ValidateDocument(doc(singleAyah(0, 1, "نص")), src); err != nil {
		t.Fatalf("a well-formed document should pass: %v", err)
	}

	noSource := usableSource()
	noSource.ID = ""
	if err := ValidateDocument(doc(singleAyah(0, 1, "نص")), noSource); err == nil {
		t.Fatal("expected a source id requirement")
	}

	if err := ValidateDocument(Document{ID: "d", SourceVersion: "v", Language: "ar", ContentType: ContentTypeTafsir, SurahID: 12}, src); !errors.Is(err, ErrEmptyDocument) {
		t.Fatalf("expected ErrEmptyDocument, got %v", err)
	}
}
