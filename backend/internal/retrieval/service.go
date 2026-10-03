package retrieval

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"

	"tadbor/backend/internal/embedding"
)

// ErrInsufficientEvidence is returned instead of any generated content when no
// approved source has evidence for the requested verse — see
// docs/rag-architecture-part1.md §12. Callers must treat this as a hard stop,
// never a prompt to "let the LLM guess."
var ErrInsufficientEvidence = errors.New("insufficient evidence for requested verse")

// thematicTopK caps how much thematic evidence one request may pull back.
const thematicTopK = 5

// TafsirChunk mirrors the "tafsir_chunks" collection. Embedding is stored as a
// plain float64 array. NOTE: self-hosted MongoDB (via this docker-compose) has
// no built-in ANN vector index — Atlas Vector Search requires MongoDB Atlas.
// At this corpus size (one surah, low hundreds of chunks) brute-force cosine
// similarity over all chunks that already passed metadata filtering is fast
// enough and avoids adding Atlas as a dependency. Revisit only if the corpus
// grows to the point this becomes measurably slow — see Part 2 §31.
type TafsirChunk struct {
	ID             string    `bson:"_id" json:"id"`
	SourceID       string    `bson:"source_id" json:"source_id"`
	SourceVerified bool      `bson:"source_verified" json:"-"`
	Text           string    `bson:"text" json:"text"`
	Embedding      []float64 `bson:"embedding" json:"-"`
	SurahID        int       `bson:"surah_id" json:"surah_id"`
	AyahStart      int       `bson:"ayah_start" json:"ayah_start"`
	AyahEnd        int       `bson:"ayah_end" json:"ayah_end"`
	MappingType    string    `bson:"mapping_type" json:"mapping_type"` // single_ayah | multi_ayah | surah_level | thematic
}

type Service struct {
	chunks *mongo.Collection
	ayahs  *mongo.Collection
	// embedder supplies the thematic query vector. It is an interface so the
	// retrieval logic stays testable without a network, and nil is a supported
	// state meaning exact mapping only.
	embedder embedding.Embedder
}

// NewService builds retrieval over the tafsir_chunks collection. Pass a nil
// embedder to run exact verse mapping only; thematic fallback will then report
// ErrInsufficientEvidence for any gap instead of guessing.
func NewService(db *mongo.Database, embedder embedding.Embedder) *Service {
	return &Service{
		chunks:   db.Collection("tafsir_chunks"),
		ayahs:    db.Collection("ayahs"),
		embedder: embedder,
	}
}

// GetEvidence implements the retrieval sequence from Part 1 §11:
// exact ayah mapping first (from approved sources only), vector similarity
// only as a secondary/thematic fallback.
//
// A nil or empty queryEmbedding means "no semantic query was supplied". In that
// case there is no basis on which to rank thematic candidates, so this returns
// ErrInsufficientEvidence rather than the thematic chunks. Ranking them anyway
// would hand the caller arbitrary rows in similarity 0.0 and let the generation
// pipeline treat them as supporting evidence, which is exactly the failure §12
// exists to prevent. Callers that want thematic search must pass a real
// embedding — GetEvidenceForAyah computes one on demand.
func (s *Service) GetEvidence(ctx context.Context, surahID, ayah int, queryEmbedding []float64) ([]TafsirChunk, error) {
	exact, err := s.exactMapping(ctx, surahID, ayah)
	if err != nil {
		return nil, err
	}
	if len(exact) > 0 {
		return exact, nil
	}

	// No exact-mapped evidence from an approved source — fall back to thematic
	// semantic search, but only among chunks that are still source-verified.
	if len(queryEmbedding) == 0 {
		return nil, ErrInsufficientEvidence
	}
	thematic, err := s.semanticFallback(ctx, surahID, queryEmbedding, thematicTopK)
	if err != nil {
		return nil, err
	}
	if len(thematic) == 0 {
		return nil, ErrInsufficientEvidence
	}
	return thematic, nil
}

// GetEvidenceForAyah runs the full sequence for a bare verse reference, deriving
// the thematic query embedding from the verse's own text when one is needed.
//
// The embedding is computed lazily: exact verse mapping is the overwhelmingly
// common path and needs no query vector at all, so a request whose ayah has
// mapped evidence costs no call to the embedding provider. Only a gap — the case
// that actually needs semantic search — reaches out. (That means a gap costs one
// extra indexed lookup of exact evidence, which is cheap next to a network call.)
//
// A provider failure is returned as an error, deliberately not as
// ErrInsufficientEvidence: the two mean different things to the generation
// pipeline — "we looked and there is nothing" versus "we could not look" — and
// collapsing them would let an outage masquerade as a content gap.
func (s *Service) GetEvidenceForAyah(ctx context.Context, surahID, ayah int) ([]TafsirChunk, error) {
	if s.embedder == nil {
		// No embedder configured: exact mapping only, and a gap is honestly
		// reported rather than filled with unranked thematic chunks.
		return s.GetEvidence(ctx, surahID, ayah, nil)
	}

	exact, err := s.exactMapping(ctx, surahID, ayah)
	if err != nil {
		return nil, err
	}
	if len(exact) > 0 {
		return exact, nil
	}

	vec, err := s.embedAyahText(ctx, surahID, ayah)
	if err != nil {
		return nil, err
	}
	return s.GetEvidence(ctx, surahID, ayah, vec)
}

// embedAyahText embeds the verse's own text to stand in as the thematic query.
// The verse is what the reader is asking about, so it is what the search should
// be phrased as; the commentary is what we are searching for, not the query.
func (s *Service) embedAyahText(ctx context.Context, surahID, ayah int) ([]float64, error) {
	var row struct {
		TextUthmani string `bson:"text_uthmani"`
	}
	if err := s.ayahs.FindOne(ctx, bson.M{"surah_id": surahID, "ayah_number": ayah}).Decode(&row); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, ErrInsufficientEvidence
		}
		return nil, fmt.Errorf("read ayah %d:%d text: %w", surahID, ayah, err)
	}
	if strings.TrimSpace(row.TextUthmani) == "" {
		return nil, ErrInsufficientEvidence
	}

	vecs, err := s.embedder.Embed(ctx, []string{row.TextUthmani})
	if err != nil {
		return nil, fmt.Errorf("embed ayah %d:%d query: %w", surahID, ayah, err)
	}
	if len(vecs) != 1 || len(vecs[0]) == 0 {
		return nil, fmt.Errorf("embed ayah %d:%d query: embedder returned no usable vector", surahID, ayah)
	}
	return vecs[0], nil
}

func (s *Service) exactMapping(ctx context.Context, surahID, ayah int) ([]TafsirChunk, error) {
	filter := bson.M{
		"surah_id":        surahID,
		"ayah_start":      bson.M{"$lte": ayah},
		"ayah_end":        bson.M{"$gte": ayah},
		"source_verified": true,
	}
	cur, err := s.chunks.Find(ctx, filter)
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)

	var results []TafsirChunk
	if err := cur.All(ctx, &results); err != nil {
		return nil, err
	}
	return results, nil
}

func (s *Service) semanticFallback(ctx context.Context, surahID int, queryEmbedding []float64, topK int) ([]TafsirChunk, error) {
	filter := bson.M{"surah_id": surahID, "source_verified": true, "mapping_type": "thematic"}
	cur, err := s.chunks.Find(ctx, filter)
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)

	var candidates []TafsirChunk
	if err := cur.All(ctx, &candidates); err != nil {
		return nil, err
	}

	type scored struct {
		chunk TafsirChunk
		score float64
	}
	var ranked []scored
	for _, c := range candidates {
		ranked = append(ranked, scored{chunk: c, score: cosineSimilarity(queryEmbedding, c.Embedding)})
	}
	// simple selection sort for top-K — fine at this scale, replace if corpus grows a lot
	for i := 0; i < len(ranked) && i < topK; i++ {
		max := i
		for j := i + 1; j < len(ranked); j++ {
			if ranked[j].score > ranked[max].score {
				max = j
			}
		}
		ranked[i], ranked[max] = ranked[max], ranked[i]
	}
	if len(ranked) > topK {
		ranked = ranked[:topK]
	}

	var out []TafsirChunk
	for _, r := range ranked {
		out = append(out, r.chunk)
	}
	return out, nil
}

func cosineSimilarity(a, b []float64) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	var dot, normA, normB float64
	for i := range a {
		dot += a[i] * b[i]
		normA += a[i] * a[i]
		normB += b[i] * b[i]
	}
	if normA == 0 || normB == 0 {
		return 0
	}
	return dot / (math.Sqrt(normA) * math.Sqrt(normB))
}
