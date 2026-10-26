package content

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

func introductionTestDB(t *testing.T) *mongo.Database {
	t.Helper()
	uri := os.Getenv("MONGO_TEST_URI")
	if uri == "" {
		t.Skip("set MONGO_TEST_URI to run MongoDB-backed introduction API tests")
	}
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
	db := client.Database(fmt.Sprintf("tadbor_introduction_api_test_%d", time.Now().UnixNano()))
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		_ = db.Drop(cleanupCtx)
		_ = client.Disconnect(cleanupCtx)
	})
	return db
}

func seedIntroductionAttribution(t *testing.T, db *mongo.Database) {
	t.Helper()
	ctx := context.Background()
	if _, err := db.Collection("sources").InsertOne(ctx, bson.M{
		"_id": "source-1", "title": "Verified Tafsir", "author": "Author", "edition": "Edition",
		"verification_status": "verified", "licensing_status": "cleared", "current_version": "version-1",
	}); err != nil {
		t.Fatalf("seed source: %v", err)
	}
	if _, err := db.Collection("source_versions").InsertOne(ctx, bson.M{
		"_id": "version-1", "source_id": "source-1", "content_hash": "source-hash",
	}); err != nil {
		t.Fatalf("seed source version: %v", err)
	}
	if _, err := db.Collection("tafsir_chunks").InsertOne(ctx, bson.M{
		"_id": "chunk-1", "source_id": "source-1", "source_version": "version-1",
		"source_version_hash": "source-hash", "source_verified": true, "text": "controlled source fixture",
	}); err != nil {
		t.Fatalf("seed chunk: %v", err)
	}
}

func TestMongoIntroductionReadIsPublishedAndExactStyleOnly(t *testing.T) {
	db := introductionTestDB(t)
	ctx := context.Background()
	seedIntroductionAttribution(t, db)
	for _, row := range []bson.M{
		{"_id": "pending-ar", "surah_id": 12, "style": "simplified_ar", "review_status": "pending", "source_refs": []string{"chunk-1"}},
		{"_id": "published-en", "surah_id": 12, "style": "en", "level": "beginner", "review_status": "published", "source_refs": []string{"chunk-1"}},
	} {
		if _, err := db.Collection("surah_introductions").InsertOne(ctx, row); err != nil {
			t.Fatalf("seed introduction: %v", err)
		}
	}

	svc := NewService(db)
	if _, err := svc.GetIntroduction(ctx, 12, "simplified_ar"); err != ErrIntroductionProcessing {
		t.Fatalf("pending Arabic introduction error = %v, want processing", err)
	}
	got, err := svc.GetIntroduction(ctx, 12, "en")
	if err != nil {
		t.Fatalf("published English introduction: %v", err)
	}
	if got.ID != "published-en" || got.Style != "en" {
		t.Fatalf("introduction = %+v, want published English document", got)
	}
	if len(got.Attributions) != 1 || got.Attributions[0].Title != "Verified Tafsir" || got.Attributions[0].SourceVersion != "version-1" {
		t.Fatalf("attributions = %+v", got.Attributions)
	}
	if _, err := svc.GetIntroduction(ctx, 12, "egyptian_ar"); err != ErrIntroductionUnavailable {
		t.Fatalf("unavailable style error = %v, want unavailable", err)
	}
}

func TestMongoIntroductionAttributionRejectsSourceDowngrade(t *testing.T) {
	db := introductionTestDB(t)
	ctx := context.Background()
	seedIntroductionAttribution(t, db)
	if _, err := db.Collection("surah_introductions").InsertOne(ctx, bson.M{
		"_id": "published", "surah_id": 12, "style": "simplified_ar", "review_status": "published", "source_refs": []string{"chunk-1"},
	}); err != nil {
		t.Fatalf("seed introduction: %v", err)
	}
	if _, err := db.Collection("sources").UpdateOne(ctx, bson.M{"_id": "source-1"}, bson.M{"$set": bson.M{"verification_status": "unverified"}}); err != nil {
		t.Fatalf("downgrade source: %v", err)
	}
	if _, err := NewService(db).GetIntroduction(ctx, 12, "simplified_ar"); err == nil {
		t.Fatal("public API returned attribution for a downgraded source")
	}
}

func TestMongoIntroductionAttributionFailsClosedForStaleRightsOrMissingRefs(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*testing.T, *mongo.Database)
		refs   []string
	}{
		{
			name: "stale chunk version hash",
			mutate: func(t *testing.T, db *mongo.Database) {
				_, err := db.Collection("tafsir_chunks").UpdateOne(context.Background(), bson.M{"_id": "chunk-1"}, bson.M{
					"$set": bson.M{"source_version_hash": "stale-hash"},
				})
				if err != nil {
					t.Fatalf("stale chunk: %v", err)
				}
			},
			refs: []string{"chunk-1"},
		},
		{
			name: "rights no longer cleared",
			mutate: func(t *testing.T, db *mongo.Database) {
				_, err := db.Collection("sources").UpdateOne(context.Background(), bson.M{"_id": "source-1"}, bson.M{
					"$set": bson.M{"licensing_status": "pending"},
				})
				if err != nil {
					t.Fatalf("change licensing status: %v", err)
				}
			},
			refs: []string{"chunk-1"},
		},
		{name: "missing chunk ref", refs: []string{"chunk-gone"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := introductionTestDB(t)
			seedIntroductionAttribution(t, db)
			if _, err := db.Collection("surah_introductions").InsertOne(context.Background(), bson.M{
				"_id": "published", "surah_id": 12, "style": "simplified_ar", "review_status": "published", "source_refs": tc.refs,
			}); err != nil {
				t.Fatalf("seed introduction: %v", err)
			}
			if tc.mutate != nil {
				tc.mutate(t, db)
			}
			if _, err := NewService(db).GetIntroduction(context.Background(), 12, "simplified_ar"); err == nil {
				t.Fatal("public API returned introduction with invalid source attribution")
			}
		})
	}
}
