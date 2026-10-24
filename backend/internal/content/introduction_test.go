package content

import (
	"context"
	"testing"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo/integration/mtest"
)

func TestGetIntroductionFiltersPublished(t *testing.T) {
	mt := mtest.New(t, mtest.NewOptions().ClientType(mtest.Mock))
	defer mt.Close()

	mt.Run("returns published introduction", func(mt *mtest.T) {
		svc := NewService(mt.DB)
		first := mtest.CreateCursorResponse(
			1,
			"tadbor.surah_introductions",
			mtest.FirstBatch,
			bson.D{
				{Key: "_id", Value: "intro1"},
				{Key: "surah_id", Value: 12},
				{Key: "style", Value: "simplified_ar"},
				{Key: "sections", Value: bson.A{}},
				{Key: "source_refs", Value: bson.A{"s1"}},
				{Key: "review_status", Value: "published"},
			},
		)
		killCursors := mtest.CreateCursorResponse(0, "tadbor.surah_introductions", mtest.NextBatch)
		mt.AddMockResponses(first, killCursors)

		intro, err := svc.GetIntroduction(context.Background(), 12)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if intro.ReviewStatus != "" {
			t.Fatalf("review_status must not be exposed")
		}
	})
}
