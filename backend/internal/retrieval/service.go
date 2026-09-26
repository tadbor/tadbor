package retrieval

import (
	"context"
	"errors"
	"math"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
)

// ErrInsufficientEvidence is returned instead of any generated content when no
// approved source has evidence for the requested verse — see
// docs/rag-architecture-part1.md §12. Callers must treat this as a hard stop,
// never a prompt to "let the LLM guess."
var ErrInsufficientEvidence = errors.New("insufficient evidence for requested verse")

// TafsirChunk mirrors the "tafsir_chunks" collection. Embedding is stored as a
// plain float64 array. NOTE: self-hosted MongoDB (via this docker-compose) has
// no built-in ANN vector index — Atlas Vector Search requires MongoDB Atlas.
// At this corpus size (one surah, low hundreds of chunks) brute-force cosine
// similarity over all chunks that already passed metadata filtering is fast
// enough and avoids adding Atlas as a dependency. Revisit only if the corpus
// grows to the point this becomes measurably slow — see Part 2 §31.
type TafsirChunk struct {
	ID              string    `bson:"_id" json:"id"`
	SourceID        string    `bson:"source_id" json:"source_id"`
	SourceVerified  bool      `bson:"source_verified" json:"-"`
	Text            string    `bson:"text" json:"text"`
	Embedding       []float64 `bson:"embedding" json:"-"`
	SurahID         int       `bson:"surah_id" json:"surah_id"`
	AyahStart       int       `bson:"ayah_start" json:"ayah_start"`
	AyahEnd         int       `bson:"ayah_end" json:"ayah_end"`
	MappingType     string    `bson:"mapping_type" json:"mapping_type"` // single_ayah | multi_ayah | surah_level | thematic
}

type Service struct {
	chunks *mongo.Collection
}

func NewService(db *mongo.Database) *Service {
	return &Service{chunks: db.Collection("tafsir_chunks")}
}

// GetEvidence implements the retrieval sequence from Part 1 §11:
// exact ayah mapping first (from approved sources only), vector similarity
// only as a secondary/thematic fallback.
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
	thematic, err := s.semanticFallback(ctx, surahID, queryEmbedding, 5)
	if err != nil {
		return nil, err
	}
	if len(thematic) == 0 {
		return nil, ErrInsufficientEvidence
	}
	return thematic, nil
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
