package ingestion

// This file owns a chunk's vector *after* the chunk is stored: reading back the
// state a stored vector is in, and writing vectors onto chunks that already exist.
//
// It is deliberately separate from Store.UpsertChunks. The ingest path writes a
// chunk and its vector in one call, because it only ever persists a chunk after
// embedding it (Service.IngestSource). What lives here is the repair path used by
// cmd/embed_chunks (issue #6): attach a vector to a chunk that is already indexed,
// so an interrupted batch run resumes where it stopped and a corpus carrying
// vectors from a superseded model can be repaired without re-chunking a passage.

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"tadbor/backend/internal/embedding"
)

// storedVector is the raw shape of one chunk's embedding fields as they sit in
// MongoDB.
//
// The `embedding` field carries no omitempty in Chunk, so a chunk that was never
// embedded decodes from a BSON null into a nil slice. That is the ordinary state of
// a chunk awaiting this job, not a corruption to be repaired.
type storedVector struct {
	ID         string    `bson:"_id"`
	SourceID   string    `bson:"source_id"`
	Text       string    `bson:"text"`
	Embedding  []float64 `bson:"embedding"`
	Model      string    `bson:"embedding_model"`
	Dimensions int       `bson:"embedding_dimensions"`
	Normalized bool      `bson:"embedding_normalized"`
	Hash       string    `bson:"embedding_hash"`
}

// EmbeddingAudit is one stored chunk reduced to the facts that decide whether its
// vector can be trusted and whether it still needs to be (re-)embedded.
type EmbeddingAudit struct {
	ID       string
	SourceID string
	// Text is the chunk text as it must be embedded: diacritics intact, never the
	// lexical form (Part 1 §7).
	Text string

	HasVector  bool
	Model      string
	Dimensions int
	Normalized bool
	// HashMatches reports whether embedding_hash is present and equals the hash of
	// the vector stored beside it. It is always false when no vector is stored.
	HashMatches bool

	// Defects is empty exactly when the vector is what this pipeline would have
	// written: present, the right width, produced by the current model, marked
	// normalized, and fingerprinted by a hash that still matches the vector.
	Defects []string
}

// NeedsEmbedding reports whether this chunk's vector has to be (re-)generated.
// A chunk with a healthy vector is never selected, which is what makes the batch job
// safe to re-run and free on a second pass.
func (a EmbeddingAudit) NeedsEmbedding() bool { return len(a.Defects) > 0 }

// audit is the whole decision for one chunk, kept separate from the read so it can
// be exercised without a database.
func (s storedVector) audit() EmbeddingAudit {
	a := EmbeddingAudit{
		ID:         s.ID,
		SourceID:   s.SourceID,
		Text:       s.Text,
		HasVector:  len(s.Embedding) > 0,
		Model:      s.Model,
		Dimensions: s.Dimensions,
		Normalized: s.Normalized,
	}

	if !a.HasVector {
		a.Defects = append(a.Defects, "no vector stored")
	}
	// A vector is only comparable to another one if both came from the same model,
	// so a stored vector from any other model is a defect even when it is perfectly
	// well formed. Mixing spaces in one collection is the failure this guards.
	if a.HasVector && s.Model != embedding.ModelID {
		a.Defects = append(a.Defects, fmt.Sprintf("vector came from model %q, not %q", s.Model, embedding.ModelID))
	}
	if a.HasVector && len(s.Embedding) != embedding.ExpectedDimensions {
		a.Defects = append(a.Defects, fmt.Sprintf("vector has %d dimensions, want %d", len(s.Embedding), embedding.ExpectedDimensions))
	}
	if a.HasVector && !s.Normalized {
		// Retrieval computes cosine in Go, which assumes unit length.
		a.Defects = append(a.Defects, "vector is not marked normalized")
	}

	switch {
	case !a.HasVector:
	case s.Hash == "":
		a.Defects = append(a.Defects, "no embedding_hash recorded")
	case s.Hash == EmbeddingHash(s.Model, [][]float64{s.Embedding}):
		a.HashMatches = true
	default:
		a.Defects = append(a.Defects, "embedding_hash does not match the stored vector")
	}
	return a
}

// AuditEmbeddings reports the vector state of every stored chunk, narrowed to one
// source when sourceID is non-empty. Results are ordered by chunk id so two runs —
// and two machines — agree on what is left to do.
//
// It reads the stored vectors rather than only their metadata because the strongest
// check available without the provider is recomputing each chunk's embedding_hash
// from the vector next to it. A mismatch means the vector and the provenance
// recorded for it no longer describe the same thing, which reading the metadata
// fields alone can never reveal. The cost is one pass over corpus × dimensions,
// which is why the cursor is streamed and each vector is released after hashing.
func (s *Store) AuditEmbeddings(ctx context.Context, sourceID string) ([]EmbeddingAudit, error) {
	filter := bson.M{}
	if sourceID != "" {
		filter["source_id"] = sourceID
	}
	cur, err := s.chunks.Find(ctx, filter, options.Find().SetSort(bson.D{{Key: "_id", Value: 1}}))
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)

	var out []EmbeddingAudit
	for cur.Next(ctx) {
		var doc storedVector
		if err := cur.Decode(&doc); err != nil {
			return nil, err
		}
		out = append(out, doc.audit())
	}
	return out, cur.Err()
}

// EmbeddedChunk pairs one stored chunk id with the vector recorded for it.
type EmbeddedChunk struct {
	ID     string
	Vector []float64
}

// SetEmbeddings records vectors against chunks that are already indexed, creating
// nothing. Writes are ordered so a mid-batch failure stops immediately instead of
// leaving a half-embedded window behind, and every write is keyed by _id, so the
// call is idempotent: re-recording an identical vector changes nothing and is not
// an error.
//
// This is the only path in the codebase that writes `embedding` onto a chunk that
// already exists, which is why it also owns the invariant the rest of the pipeline
// relies on — a stored vector always arrives with the four provenance fields beside
// it. Retrieval reads a vector with no model, width, or hash next to it and has
// nowhere to record that it could not tell which space the vector came from.
func (s *Store) SetEmbeddings(ctx context.Context, model string, chunks []EmbeddedChunk) (*mongo.BulkWriteResult, error) {
	if len(chunks) == 0 {
		return &mongo.BulkWriteResult{}, nil
	}
	model = modelOrDefault(model)
	now := timestamp()

	ops := make([]mongo.WriteModel, 0, len(chunks))
	ids := make([]string, 0, len(chunks))
	for _, c := range chunks {
		if strings.TrimSpace(c.ID) == "" {
			return nil, errors.New("set embeddings: chunk id is required")
		}
		// Width is the provider client's gate (embedding.ValidateVector with the
		// model's expected width); what is checked here is that the vector is
		// comparable at all, so a bad batch fails at the write boundary instead of
		// being discovered by a cosine comparison weeks later.
		if err := embedding.ValidateVector(c.Vector, 0); err != nil {
			return nil, fmt.Errorf("chunk %s: %w", c.ID, err)
		}
		ops = append(ops, mongo.NewUpdateOneModel().
			SetFilter(bson.M{"_id": c.ID}).
			SetUpdate(bson.M{"$set": bson.M{
				"embedding":            c.Vector,
				"embedding_model":      model,
				"embedding_dimensions": len(c.Vector),
				"embedding_normalized": true,
				"embedding_hash":       EmbeddingHash(model, [][]float64{c.Vector}),
				"updated_at":           now,
			}}))
		ids = append(ids, c.ID)
	}

	res, err := s.chunks.BulkWrite(ctx, ops, options.BulkWrite().SetOrdered(true))
	if err != nil {
		return nil, err
	}
	// MatchedCount rather than ModifiedCount: re-recording an identical vector is a
	// no-op write, and a re-run must be silent rather than a failure.
	if res.MatchedCount != int64(len(chunks)) {
		missing, mErr := s.missingChunkIDs(ctx, ids)
		if mErr != nil {
			return nil, fmt.Errorf("set embeddings: %d of %d chunks are not in the index: %w", len(chunks)-int(res.MatchedCount), len(chunks), mErr)
		}
		return nil, fmt.Errorf("set embeddings: %d of %d chunks are not in the index: %s\n"+
			"  the vectors that did land are kept — re-run to embed the rest",
			len(missing), len(chunks), strings.Join(missing, ", "))
	}
	return res, nil
}

// missingChunkIDs returns the ids from ids that the collection does not hold, in
// the order given.
func (s *Store) missingChunkIDs(ctx context.Context, ids []string) ([]string, error) {
	cur, err := s.chunks.Find(ctx,
		bson.M{"_id": bson.M{"$in": ids}},
		options.Find().SetProjection(bson.M{"_id": 1}),
	)
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)

	seen := map[string]bool{}
	for cur.Next(ctx) {
		var doc struct {
			ID string `bson:"_id"`
		}
		if err := cur.Decode(&doc); err != nil {
			return nil, err
		}
		seen[doc.ID] = true
	}
	if err := cur.Err(); err != nil {
		return nil, err
	}

	var missing []string
	for _, id := range ids {
		if !seen[id] {
			missing = append(missing, id)
		}
	}
	sort.Strings(missing)
	return missing, nil
}
