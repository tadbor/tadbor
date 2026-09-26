package review

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
)

type Service struct {
	explanations *mongo.Collection
	reviews      *mongo.Collection
}

func NewService(db *mongo.Database) *Service {
	return &Service{
		explanations: db.Collection("explanations"),
		reviews:      db.Collection("reviews"),
	}
}

// PendingQueue returns explanations awaiting a reviewer decision.
func (s *Service) PendingQueue(ctx context.Context) ([]bson.M, error) {
	cur, err := s.explanations.Find(ctx, bson.M{"review_status": "pending"})
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)

	var results []bson.M
	if err := cur.All(ctx, &results); err != nil {
		return nil, err
	}
	return results, nil
}

// Decide records a reviewer decision and, on approval, flips the explanation
// to "published" — the only write path that can make content publicly visible.
func (s *Service) Decide(ctx context.Context, explanationID, reviewerID, decision, comments string) error {
	record := ReviewRecord{
		ExplanationID: explanationID,
		ReviewerID:    reviewerID,
		Decision:      decision,
		Comments:      comments,
		CreatedAt:     time.Now().UTC().Format(time.RFC3339),
	}
	if _, err := s.reviews.InsertOne(ctx, record); err != nil {
		return err
	}

	newStatus := "pending"
	switch decision {
	case "approve", "edit_approve":
		newStatus = "published"
	case "reject":
		newStatus = "rejected"
	}

	_, err := s.explanations.UpdateOne(ctx,
		bson.M{"_id": explanationID},
		bson.M{"$set": bson.M{"review_status": newStatus}},
	)
	return err
}
