package ingestion

import (
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"strings"
	"time"

	"tadbor/backend/internal/embedding"
)

// Source ingestion statuses (Part 1 §6, "ingestion_status"). These describe how
// far a source has travelled through the pipeline in Part 1 §7 — they are
// bookkeeping about the ingest job, never about whether content may be used.
// That second question is answered exclusively by SourceRegistry.IsUsable.
const (
	StatusNotStarted = "not_started"
	StatusInProgress = "in_progress"
	StatusIngested   = "ingested"
	StatusFailed     = "failed"
)

// Mapping types from Part 1 §8. A chunk's mapping type is the single most
// important correctness property in the system: it is how a chunk becomes
// reachable from an exact verse lookup instead of from a similarity search.
const (
	MappingSingleAyah = "single_ayah"
	MappingMultiAyah  = "multi_ayah"
	MappingSurahLevel = "surah_level"
	MappingThematic   = "thematic"
)

// Content types from Part 1 §10.
const (
	ContentTypeTafsir            = "tafsir"
	ContentTypeHistoricalContext = "historical_context"
	ContentTypeLexical           = "lexical"
	ContentTypeThematic          = "thematic"
)

// Document is one ingestible source document: an ordered list of structurally
// detected sections, each already anchored to a verse range.
//
// Parsing (PDF/OCR/layout detection) is deliberately NOT modelled here. Day 2
// ships the machinery that consumes structure, not the structure detection that
// produces it — that depends on the real Tier-1 source and edition once they are
// supplied. Until then the manifest can legitimately be empty.
type Document struct {
	ID            string    `json:"id"`
	SourceVersion string    `json:"source_version"`
	Language      string    `json:"language"`
	ContentType   string    `json:"content_type"`
	SurahID       int       `json:"surah_id"`
	Sections      []Section `json:"sections"`
}

// Section is a contiguous passage of commentary that discusses one verse, one
// verse range, the whole surah, or a theme (Part 1 §8–§9).
type Section struct {
	Ordinal     int    `json:"ordinal"`
	SurahID     int    `json:"surah_id"`
	AyahStart   int    `json:"ayah_start"`
	AyahEnd     int    `json:"ayah_end"`
	MappingType string `json:"mapping_type"`
	Text        string `json:"text"`
}

// Chunk is the write model for the "tafsir_chunks" collection. It is a strict
// superset of what internal/retrieval reads (retrieval.TafsirChunk): retrieval
// projects the fields it filters and ranks on, while the rest exist to satisfy
// the mandatory metadata set in Part 1 §10 and to keep the ingestion pipeline
// resumable and auditable.
type Chunk struct {
	ID            string `bson:"_id,omitempty" json:"id"`
	DocumentID    string `bson:"document_id" json:"document_id"`
	ParentChunkID string `bson:"parent_chunk_id,omitempty" json:"parent_chunk_id,omitempty"`

	SourceID      string `bson:"source_id" json:"source_id"`
	SourceVersion string `bson:"source_version" json:"source_version"`

	SurahID     int    `bson:"surah_id" json:"surah_id"`
	AyahStart   int    `bson:"ayah_start" json:"ayah_start"`
	AyahEnd     int    `bson:"ayah_end" json:"ayah_end"`
	MappingType string `bson:"mapping_type" json:"mapping_type"`

	// Text is the original cleaned text with diacritics intact — this is what
	// gets embedded. Part 1 §7: normalization must be reversible/auditable, and
	// diacritics are meaning-bearing in Quranic quotations inside commentary.
	Text string `bson:"text" json:"text"`
	// TextNormalized is a diacritic-stripped form for future lexical matching
	// only. It is never displayed to a reader and never embedded.
	TextNormalized string `bson:"text_normalized" json:"text_normalized"`
	ContentHash    string `bson:"content_hash" json:"content_hash"`

	ContentType string `bson:"content_type" json:"content_type"`
	Language    string `bson:"language" json:"language"`

	// Denormalized source state (Part 1 §10 requires authority_tier and the
	// source's review status on every chunk). Retrieved directly as
	// "source_verified" by internal/retrieval, which cannot join back to
	// "sources" — so it is a snapshot taken at ingest time.
	AuthorityTier  int    `bson:"authority_tier" json:"authority_tier"`
	SourceVerified bool   `bson:"source_verified" json:"-"`
	ReviewStatus   string `bson:"review_status" json:"review_status"`

	Embedding           []float64 `bson:"embedding" json:"-"`
	EmbeddingModel      string    `bson:"embedding_model" json:"embedding_model"`
	EmbeddingDimensions int       `bson:"embedding_dimensions" json:"embedding_dimensions"`
	EmbeddingNormalized bool      `bson:"embedding_normalized" json:"embedding_normalized"`
	EmbeddingHash       string    `bson:"embedding_hash" json:"embedding_hash"`

	CreatedAt string `bson:"created_at,omitempty" json:"created_at"`
	UpdatedAt string `bson:"updated_at,omitempty" json:"updated_at"`
}

// ChunkKey is a chunk's content identity: what it says, and which verses it is
// anchored to. It deliberately excludes position in the document, so editing one
// passage does not invalidate the stored vectors of every passage after it.
type ChunkKey struct {
	AyahStart   int
	AyahEnd     int
	MappingType string
	ContentHash string
}

// ChunkID is the deterministic identity of a chunk — the idempotency key that
// makes a re-run of the pipeline safe.
//
// It binds source, source version, content, verse mapping, and a duplicate
// index. Consequences:
//   - re-running with unchanged input produces identical ids, so upserts
//     overwrite instead of duplicating;
//   - editing a passage changes its content hash and therefore its id, so the
//     superseded chunk is removed by the manifest-diff sweep rather than
//     silently mutated;
//   - two editions of the same work never collide, per Part 1 §6;
//   - removing or inserting an earlier passage does not disturb the ids of later
//     passages, so a structural edit re-embeds only what actually changed.
//
// dupIndex separates chunks that are genuinely identical — the same text
// anchored to the same verses, which real tafsir do contain (a repeated formula,
// a cross-reference quoted twice). Those chunks are interchangeable, so their
// relative order is assigned from document order among themselves and stays
// stable. Without it, identical text would silently overwrite itself.
func ChunkID(sourceID, sourceVersion string, key ChunkKey, dupIndex int) string {
	h := sha256.New()
	for _, part := range []string{
		sourceID,
		sourceVersion,
		strconv.Itoa(key.AyahStart),
		strconv.Itoa(key.AyahEnd),
		key.MappingType,
		key.ContentHash,
		strconv.Itoa(dupIndex),
	} {
		h.Write([]byte(part))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}

// parentChunkID identifies the whole verse-commentary section a paragraph-level
// chunk was split out of, so context assembly (Part 1 §13) or a reviewer can
// expand back to the full section (Part 1 §9). It is derived from the section's
// own content, so it stays stable across edits elsewhere in the document.
func parentChunkID(sourceID, sourceVersion string, section ChunkKey) string {
	return ChunkID(sourceID, sourceVersion, section, 0)
}

// ContentHash is the sha256 of a chunk's text, matching the ayah corpus
// convention in cmd/seed_quran.
func ContentHash(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])
}

// EmbeddingHash fingerprints a stored vector together with the model that
// produced it, so a corpus can be audited for mixed embedding spaces.
func EmbeddingHash(model string, vectors [][]float64) string {
	h := sha256.New()
	h.Write([]byte(model))
	h.Write([]byte{0})
	for _, v := range vectors {
		for _, f := range v {
			h.Write([]byte(strconv.FormatFloat(f, 'g', -1, 64)))
			h.Write([]byte{0})
		}
		h.Write([]byte{1})
	}
	return hex.EncodeToString(h.Sum(nil))
}

// embed fills the vector provenance fields on a chunk. The caller has already
// validated the vector shape via ValidateBatch.
func (c *Chunk) embed(vector []float64, model string) {
	c.Embedding = vector
	c.EmbeddingModel = model
	c.EmbeddingDimensions = len(vector)
	c.EmbeddingNormalized = true
	c.EmbeddingHash = EmbeddingHash(model, [][]float64{vector})
}

// timestamp returns an RFC3339 string, matching the convention already used by
// internal/review.
func timestamp() string { return time.Now().UTC().Format(time.RFC3339) }

// trimmed is a small helper used by the validators.
func trimmed(s string) bool { return strings.TrimSpace(s) != "" }

// modelOrDefault falls back to the verified production model.
func modelOrDefault(model string) string {
	if trimmed(model) {
		return model
	}
	return embedding.ModelID
}
