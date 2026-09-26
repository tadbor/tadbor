package quran

import (
	"context"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
)

type Service struct {
	ayahs  *mongo.Collection
	surahs *mongo.Collection
}

func NewService(db *mongo.Database) *Service {
	return &Service{
		ayahs:  db.Collection("ayahs"),
		surahs: db.Collection("surahs"),
	}
}

func (s *Service) GetSurah(ctx context.Context, surahID int) ([]Ayah, error) {
	cur, err := s.ayahs.Find(ctx, bson.M{"surah_id": surahID}, nil)
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)

	var results []Ayah
	if err := cur.All(ctx, &results); err != nil {
		return nil, err
	}
	return results, nil
}

func (s *Service) GetAyah(ctx context.Context, surahID, ayahNumber int) (*Ayah, error) {
	var a Ayah
	err := s.ayahs.FindOne(ctx, bson.M{"surah_id": surahID, "ayah_number": ayahNumber}).Decode(&a)
	if err != nil {
		return nil, err
	}
	return &a, nil
}
