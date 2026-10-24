package content

import (
	"context"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
)

type Service struct {
	explanations *mongo.Collection
}

func NewService(db *mongo.Database) *Service {
	return &Service{explanations: db.Collection("explanations")}
}

// GetExplanations returns only published explanations for a surah in a given style.
// This hard filter is the enforcement point for "reader never sees unreviewed content."
func (s *Service) GetExplanations(ctx context.Context, surahID int, style string) ([]Explanation, error) {
	filter := bson.M{
		"surah_id":      surahID,
		"style":         style,
		"review_status": "published",
	}
	cur, err := s.explanations.Find(ctx, filter)
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)

	var results []Explanation
	if err := cur.All(ctx, &results); err != nil {
		return nil, err
	}
	return results, nil
}

func (s *Service) GetIntroduction(ctx context.Context, surahID int) (*SurahIntroduction, error) {
	filter := bson.M{
		"surah_id":      surahID,
		"review_status": "published",
	}
	var intro SurahIntroduction
	err := s.explanations.Database().Collection("surah_introductions").FindOne(ctx, filter).Decode(&intro)
	if err != nil {
		return nil, err
	}
	return &intro, nil
}
