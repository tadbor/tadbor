package recitation

import (
	"context"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// Reciter mirrors the "reciters" collection.
type Reciter struct {
	ID        string `bson:"_id,omitempty" json:"id"`
	Name      string `bson:"name" json:"name"`
	StyleNote string `bson:"style_note,omitempty" json:"style_note,omitempty"`
}

// Recitation mirrors the "recitations" collection. AudioURL points at object
// storage — audio files themselves are never stored in MongoDB, only the
// reference, mirroring how explanations reference source_refs rather than
// embedding source text. See docs/ADDENDUM-recitation-audio.md.
type Recitation struct {
	ID              string `bson:"_id,omitempty" json:"id"`
	ReciterID       string `bson:"reciter_id" json:"reciter_id"`
	SurahID         int    `bson:"surah_id" json:"surah_id"`
	AyahNumber      int    `bson:"ayah_number" json:"ayah_number"`
	AudioURL        string `bson:"audio_url" json:"audio_url"`
	DurationSeconds int    `bson:"duration_seconds,omitempty" json:"duration_seconds,omitempty"`
}

type Service struct {
	reciters    *mongo.Collection
	recitations *mongo.Collection
}

func NewService(db *mongo.Database) *Service {
	return &Service{
		reciters:    db.Collection("reciters"),
		recitations: db.Collection("recitations"),
	}
}

func (s *Service) ListReciters(ctx context.Context) ([]Reciter, error) {
	cur, err := s.reciters.Find(ctx, bson.M{})
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)

	var results []Reciter
	if err := cur.All(ctx, &results); err != nil {
		return nil, err
	}
	return results, nil
}

// GetSurahRecitations returns per-ayah audio for one surah + reciter,
// ordered by ayah number so the client can play straight through.
func (s *Service) GetSurahRecitations(ctx context.Context, surahID int, reciterID string) ([]Recitation, error) {
	cur, err := s.recitations.Find(
		ctx,
		bson.M{"surah_id": surahID, "reciter_id": reciterID},
		options.Find().SetSort(bson.D{{Key: "ayah_number", Value: 1}}),
	)
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)

	var results []Recitation
	if err := cur.All(ctx, &results); err != nil {
		return nil, err
	}
	return results, nil
}
