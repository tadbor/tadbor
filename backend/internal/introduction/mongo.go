package introduction

import (
	"context"
	"errors"
	"fmt"

	"tadbor/backend/internal/content"
	"tadbor/backend/internal/generation"
	"tadbor/backend/internal/ingestion"
	"tadbor/backend/internal/retrieval"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type MongoEvidenceSource struct {
	retrieval *retrieval.Service
	registry  *ingestion.SourceRegistry
}

func NewMongoEvidenceSource(db *mongo.Database) *MongoEvidenceSource {
	return &MongoEvidenceSource{
		retrieval: retrieval.NewService(db, nil),
		registry:  ingestion.NewSourceRegistry(db),
	}
}

func (s *MongoEvidenceSource) LoadEvidence(ctx context.Context, surahID int) ([]generation.Evidence, error) {
	if surahID != PilotSurahID {
		return nil, fmt.Errorf("only surah %d introduction evidence is supported", PilotSurahID)
	}
	chunks, err := s.retrieval.GetVerifiedSurahEvidence(ctx, surahID)
	if err != nil {
		return nil, err
	}
	if len(chunks) == 0 {
		return nil, nil
	}

	usable := map[string]ingestion.SourceVersion{}
	var evidence []generation.Evidence
	for _, chunk := range chunks {
		version, ok := usable[chunk.SourceID]
		if !ok {
			isUsable, err := s.registry.IsUsable(ctx, chunk.SourceID)
			if err != nil {
				return nil, fmt.Errorf("verify source %q: %w", chunk.SourceID, err)
			}
			if !isUsable {
				return nil, fmt.Errorf("source %q is not currently verified and rights-cleared", chunk.SourceID)
			}
			pinned, err := s.registry.Version(ctx, chunk.SourceID)
			if err != nil {
				return nil, fmt.Errorf("resolve pinned source version for %q: %w", chunk.SourceID, err)
			}
			version = *pinned
			usable[chunk.SourceID] = version
		}
		if chunk.SourceVersion != version.ID || chunk.SourceVersionHash != version.ContentHash {
			return nil, fmt.Errorf("chunk %q does not match current pinned source version %q", chunk.ID, version.ID)
		}
		if chunk.ID == "" || chunk.Text == "" {
			return nil, fmt.Errorf("verified chunk from source %q is missing its id or text", chunk.SourceID)
		}
		evidence = append(evidence, generation.Evidence{
			ID: chunk.ID, Text: chunk.Text, SourceID: chunk.SourceID, SourceVersion: chunk.SourceVersion,
		})
	}
	return evidence, nil
}

type MongoDraftStore struct {
	introductions *mongo.Collection
}

func NewMongoDraftStore(db *mongo.Database) *MongoDraftStore {
	return &MongoDraftStore{introductions: db.Collection("surah_introductions")}
}

func (s *MongoDraftStore) EnsureIndexes(ctx context.Context) error {
	_, err := s.introductions.Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys:    bson.D{{Key: "surah_id", Value: 1}, {Key: "style", Value: 1}},
		Options: options.Index().SetUnique(true).SetName("surah_style_unique"),
	})
	if err != nil && mongo.IsDuplicateKeyError(err) {
		return fmt.Errorf("cannot safely create introduction uniqueness index: %w", err)
	}
	return err
}

func (s *MongoDraftStore) SavePending(ctx context.Context, draft content.SurahIntroduction) error {
	if draft.SurahID != PilotSurahID || draft.ReviewStatus != "pending" || len(draft.SourceRefs) == 0 {
		return errors.New("refusing introduction draft without pilot surah, pending status, and source refs")
	}
	if draft.ID == "" {
		draft.ID = fmt.Sprintf("surah-%d-%s", draft.SurahID, draft.Style)
	}
	filter := bson.M{
		"surah_id":      draft.SurahID,
		"style":         draft.Style,
		"review_status": bson.M{"$in": []string{"pending", "processing"}},
	}
	draftDocument, err := bsonDocument(draft)
	if err != nil {
		return fmt.Errorf("encode introduction draft: %w", err)
	}
	delete(draftDocument, "_id")
	update := bson.M{
		"$set":         draftDocument,
		"$setOnInsert": bson.M{"_id": draft.ID},
	}
	result, err := s.introductions.UpdateOne(ctx, filter, update, options.Update().SetUpsert(true))
	if err != nil {
		return err
	}
	if result.MatchedCount == 0 && result.UpsertedCount == 0 {
		return errors.New("introduction draft was not persisted")
	}
	return nil
}

func bsonDocument(value any) (bson.M, error) {
	encoded, err := bson.Marshal(value)
	if err != nil {
		return nil, err
	}
	var document bson.M
	if err := bson.Unmarshal(encoded, &document); err != nil {
		return nil, err
	}
	return document, nil
}
