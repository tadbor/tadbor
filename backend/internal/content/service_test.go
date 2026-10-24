package content

import (
	"context"
	"testing"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo/integration/mtest"
)

func TestGetExplanationsOnlyPublished(t *testing.T) {
	mt := mtest.New(t, mtest.NewOptions().ClientType(mtest.Mock))
	defer mt.Close()

	mt.Run("returns only published explanations", func(mt *mtest.T) {
		svc := NewService(mt.DB)
		first := mtest.CreateCursorResponse(
			1,
			"tadbor.explanations",
			mtest.FirstBatch,
			bson.D{
				{Key: "_id", Value: "exp1"},
				{Key: "surah_id", Value: 12},
				{Key: "ayah_start", Value: 1},
				{Key: "ayah_end", Value: 1},
				{Key: "style", Value: "simplified_ar"},
				{Key: "level", Value: "beginner"},
				{Key: "text", Value: "test"},
				{Key: "source_refs", Value: bson.A{"ref1"}},
				{Key: "review_status", Value: "published"},
			},
		)
		killCursors := mtest.CreateCursorResponse(0, "tadbor.explanations", mtest.NextBatch)
		mt.AddMockResponses(first, killCursors)

		results, err := svc.GetExplanations(context.Background(), 12, "simplified_ar")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(results) != 1 {
			t.Fatalf("expected 1 result, got %d", len(results))
		}
		if results[0].ReviewStatus != "" {
			t.Fatalf("review_status must not be exposed")
		}
		if results[0].SourceVersion != "" {
			t.Fatalf("source_version_snapshot must not be exposed")
		}
	})
}
