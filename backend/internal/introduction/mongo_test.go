package introduction

import (
	"context"
	"fmt"
	"testing"
	"time"

	"tadbor/backend/internal/content"
	"tadbor/backend/internal/mongotest"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

func mongoIntroductionTestDB(t *testing.T) *mongo.Database {
	t.Helper()
	uri := mongotest.URI(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	client, err := mongo.Connect(ctx, options.Client().ApplyURI(uri))
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	if err := client.Ping(ctx, nil); err != nil {
		_ = client.Disconnect(ctx)
		t.Fatalf("ping: %v", err)
	}
	db := client.Database(fmt.Sprintf("tadbor_introduction_workflow_test_%d", time.Now().UnixNano()))
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		_ = db.Drop(cleanupCtx)
		_ = client.Disconnect(cleanupCtx)
	})
	return db
}

func TestMongoDraftStorePersistsStablePendingClaimDocument(t *testing.T) {
	db := mongoIntroductionTestDB(t)
	ctx := context.Background()
	store := NewMongoDraftStore(db)
	if err := store.EnsureIndexes(ctx); err != nil {
		t.Fatalf("EnsureIndexes: %v", err)
	}
	draft := content.SurahIntroduction{
		ID: "surah-12-simplified_ar", SurahID: 12, Style: "simplified_ar", Level: "beginner",
		ReviewStatus: "pending", SourceRefs: []string{"chunk-1"}, SourceVersionSnapshot: "source-v1",
		Sections: []content.Section{{Type: "themes", Title: "Themes", Content: "Grounded claim.",
			SourceRefs: []string{"chunk-1"}, Claims: []content.Claim{{Text: "Grounded claim.", SourceRefs: []string{"chunk-1"}}}}},
	}
	if err := store.SavePending(ctx, draft); err != nil {
		t.Fatalf("SavePending: %v", err)
	}
	var got bson.M
	if err := db.Collection("surah_introductions").FindOne(ctx, bson.M{"_id": draft.ID}).Decode(&got); err != nil {
		t.Fatalf("read persisted draft: %v", err)
	}
	if got["review_status"] != "pending" || got["source_version_snapshot"] != "source-v1" {
		t.Fatalf("persisted status/provenance = %v / %v", got["review_status"], got["source_version_snapshot"])
	}
	sections, ok := got["sections"].(primitive.A)
	if !ok || len(sections) != 1 {
		t.Fatalf("persisted sections = %#v", got["sections"])
	}
}

func TestMongoDraftStoreRefusesNonPendingOrUncitedDraft(t *testing.T) {
	store := &MongoDraftStore{}
	for _, draft := range []content.SurahIntroduction{
		{SurahID: 12, Style: "simplified_ar", ReviewStatus: "published", SourceRefs: []string{"chunk-1"}},
		{SurahID: 12, Style: "simplified_ar", ReviewStatus: "pending"},
	} {
		if err := store.SavePending(context.Background(), draft); err == nil {
			t.Fatalf("SavePending accepted unsafe draft: %+v", draft)
		}
	}
}
