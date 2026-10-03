package ingestion

import (
	"context"
	"fmt"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// Store owns every write the ingestion pipeline performs. Reads for retrieval
// live in internal/retrieval and are not affected by anything here.
type Store struct {
	chunks  *mongo.Collection
	sources *mongo.Collection
}

func NewStore(db *mongo.Database) *Store {
	return &Store{
		chunks:  db.Collection("tafsir_chunks"),
		sources: db.Collection("sources"),
	}
}

// EnsureIndexes creates the indexes retrieval's query shapes depend on. Nothing
// else in the codebase manages indexes today, so ingestion owns them — without
// these, exact-verse lookup degrades into a collection scan as soon as the
// corpus has more than a handful of sources.
//
// _id is implicitly unique, which is what makes the deterministic chunk ids
// sufficient for idempotency; no separate uniqueness constraint is needed.
func (s *Store) EnsureIndexes(ctx context.Context) error {
	_, err := s.chunks.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{
			Keys: bson.D{
				{Key: "surah_id", Value: 1},
				{Key: "ayah_start", Value: 1},
				{Key: "ayah_end", Value: 1},
				{Key: "source_verified", Value: 1},
			},
			Options: options.Index().SetName("exact_ayah_lookup"),
		},
		{
			Keys: bson.D{
				{Key: "surah_id", Value: 1},
				{Key: "mapping_type", Value: 1},
				{Key: "source_verified", Value: 1},
			},
			Options: options.Index().SetName("thematic_fallback"),
		},
		{
			Keys: bson.D{
				{Key: "source_id", Value: 1},
				{Key: "source_version", Value: 1},
			},
			Options: options.Index().SetName("source_manifest"),
		},
	})
	return err
}

// storedChunk is the resume-relevant projection of an existing chunk. Only the
// embedding model is needed to decide whether stored vectors can be reused — the
// content hash is already part of the chunk id, so an existing id already means
// the text is unchanged.
type storedChunk struct {
	ID             string `bson:"_id"`
	EmbeddingModel string `bson:"embedding_model"`
}

// Existing returns the chunk ids already stored for one (source, source version)
// along with the model their vectors came from. This is what makes a re-run cost
// zero provider calls instead of re-embedding the whole corpus.
func (s *Store) Existing(ctx context.Context, sourceID, sourceVersion string) (map[string]storedChunk, error) {
	filter := bson.M{"source_id": sourceID, "source_version": sourceVersion}
	opts := options.Find().
		SetProjection(bson.M{"_id": 1, "embedding_model": 1})

	cur, err := s.chunks.Find(ctx, filter, opts)
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)

	out := map[string]storedChunk{}
	for cur.Next(ctx) {
		var doc storedChunk
		if err := cur.Decode(&doc); err != nil {
			return nil, err
		}
		out[doc.ID] = doc
	}
	return out, cur.Err()
}

// UpsertChunks writes chunks idempotently. Every chunk id is deterministic, so
// re-running the pipeline overwrites in place rather than duplicating. Writes are
// ordered so a mid-batch failure stops immediately instead of leaving a partial,
// silently inconsistent set behind.
func (s *Store) UpsertChunks(ctx context.Context, chunks []Chunk) (*mongo.BulkWriteResult, error) {
	if len(chunks) == 0 {
		return &mongo.BulkWriteResult{}, nil
	}

	now := timestamp()
	ops := make([]mongo.WriteModel, 0, len(chunks))
	for _, c := range chunks {
		// Round-trip through BSON so the update set matches the struct's tags
		// exactly — no second copy of the field list to keep in sync.
		raw, err := bson.Marshal(c)
		if err != nil {
			return nil, fmt.Errorf("encode chunk %s: %w", c.ID, err)
		}
		var set bson.M
		if err := bson.Unmarshal(raw, &set); err != nil {
			return nil, fmt.Errorf("decode chunk %s: %w", c.ID, err)
		}
		// _id comes from the upsert filter, and created_at is written exactly
		// once — on insert — so it must not also appear in $set or MongoDB
		// rejects the update as a conflicting path.
		delete(set, "_id")
		delete(set, "created_at")
		set["updated_at"] = now
		c.UpdatedAt = now

		ops = append(ops, mongo.NewUpdateOneModel().
			SetFilter(bson.M{"_id": c.ID}).
			SetUpdate(bson.M{
				"$set":         set,
				"$setOnInsert": bson.M{"created_at": now},
			}).
			SetUpsert(true))
	}
	return s.chunks.BulkWrite(ctx, ops, options.BulkWrite().SetOrdered(true))
}

// DeleteStale removes chunks for one (source, source version) that the current
// manifest no longer contains. Without this, editing or removing a passage would
// leave the old chunk indexed and still servable by retrieval, with no way to
// tell it apart from current content. Scope is deliberately narrow: a different
// source or source version is never touched.
func (s *Store) DeleteStale(ctx context.Context, sourceID, sourceVersion string, keep []string) (int64, error) {
	// $nin rejects a null, so an empty manifest must send a real (empty) array:
	// that is what clears the edition instead of silently doing nothing.
	if keep == nil {
		keep = []string{}
	}
	filter := bson.M{
		"source_id":      sourceID,
		"source_version": sourceVersion,
		"_id":            bson.M{"$nin": keep},
	}
	res, err := s.chunks.DeleteMany(ctx, filter)
	if err != nil {
		return 0, err
	}
	return res.DeletedCount, nil
}

// SyncSourceState re-applies the registry gate to every stored chunk of a source.
// Chunks denormalize source_verified / review_status at ingest time, so a source
// that is later downgraded or de-licensed would otherwise keep serving its old
// flag until the whole source is re-ingested. Retrieval filters on
// source_verified, so this is a safety-relevant correction, not housekeeping.
func (s *Store) SyncSourceState(ctx context.Context, src Source) (int64, error) {
	usable := src.VerificationStatus == VerificationVerified && src.LicensingStatus == LicensingCleared
	res, err := s.chunks.UpdateMany(ctx,
		bson.M{"source_id": src.ID},
		bson.M{"$set": bson.M{
			"source_verified": usable,
			"review_status":   src.VerificationStatus,
			"authority_tier":  src.AuthorityTier,
			"updated_at":      timestamp(),
		}},
	)
	if err != nil {
		return 0, err
	}
	return res.ModifiedCount, nil
}
