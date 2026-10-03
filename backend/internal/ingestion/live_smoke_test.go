package ingestion

import (
	"context"
	"os"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"tadbor/backend/internal/embedding"
)

// TestLiveIngestSmoke exercises the real embedding provider end to end. It is
// opt-in because it spends quota and needs credentials:
//
//	LIVE_INGEST_SMOKE=1 \
//	EMBEDDING_API_URL=... EMBEDDING_API_KEY=... \
//	MONGO_TEST_URI=mongodb://... \
//	go test -run TestLiveIngestSmoke ./internal/ingestion/
//
// It writes to a throwaway database that is dropped afterwards, so it can never
// touch real corpus data.
func TestLiveIngestSmoke(t *testing.T) {
	if os.Getenv("LIVE_INGEST_SMOKE") != "1" {
		t.Skip("set LIVE_INGEST_SMOKE=1 to run the live ingestion smoke test")
	}
	uri := os.Getenv("MONGO_TEST_URI")
	if uri == "" {
		t.Fatal("MONGO_TEST_URI must point at a throwaway database for the live smoke test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	client, err := mongo.Connect(ctx, options.Client().ApplyURI(uri))
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer client.Disconnect(ctx)
	db := client.Database("tadbor_ingestion_live_smoke")
	if err := db.Drop(ctx); err != nil {
		t.Fatalf("drop: %v", err)
	}
	defer db.Drop(context.Background())

	embedClient, err := embedding.NewClient(os.Getenv("EMBEDDING_API_URL"), os.Getenv("EMBEDDING_API_KEY"))
	if err != nil {
		t.Fatalf("embedding client: %v", err)
	}

	src := usableSource()
	if _, err := db.Collection("sources").InsertOne(ctx, src); err != nil {
		t.Fatalf("seed source: %v", err)
	}

	// One Arabic passage and one English passage, so the smoke test would catch a
	// provider that only handles one script.
	d := Document{
		ID:          "live-smoke",
		SurahID:     12,
		ContentType: ContentTypeTafsir,
		Sections: []Section{
			singleAyah(0, 1, "المعنى المأثور عن السور المكية في تفسير أول سورة يوسف"),
			singleAyah(1, 2, "The meaning of revelation in the interpretation of the opening of Surah Yusuf"),
		},
	}

	report, err := NewService(db, embedClient).IngestSource(ctx, src.ID, []Document{d}, IngestOptions{BatchSize: 2})
	if err != nil {
		t.Fatalf("IngestSource: %v", err)
	}
	if report.Embedded != 2 || report.Written != 2 {
		t.Fatalf("unexpected report: %+v", report)
	}

	cur, err := db.Collection("tafsir_chunks").Find(ctx, bson.M{})
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	var stored []Chunk
	if err := cur.All(ctx, &stored); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(stored) != 2 {
		t.Fatalf("expected 2 stored chunks, got %d", len(stored))
	}
	for _, c := range stored {
		if len(c.Embedding) != embedding.ExpectedDimensions {
			t.Fatalf("chunk %s: %d dimensions, want %d", c.ID, len(c.Embedding), embedding.ExpectedDimensions)
		}
		if err := embedding.ValidateVector(c.Embedding, embedding.ExpectedDimensions); err != nil {
			t.Fatalf("chunk %s: stored vector is unusable: %v", c.ID, err)
		}
		if c.EmbeddingModel != embedding.ModelID {
			t.Fatalf("chunk %s: model %q", c.ID, c.EmbeddingModel)
		}
		// The Arabic passage carries diacritics, so its lexical form must differ
		// from the original; the English one has none and is left alone.
		if NewNormalizer().HasDiacritics(c.Text) && c.TextNormalized == c.Text {
			t.Fatalf("chunk %s: expected a distinct lexical form for diacritic text", c.ID)
		}
	}

	// Two runs of the same manifest must not double the corpus or re-embed.
	second, err := NewService(db, embedClient).IngestSource(ctx, src.ID, []Document{d}, IngestOptions{BatchSize: 2})
	if err != nil {
		t.Fatalf("second IngestSource: %v", err)
	}
	if second.Reused != 2 || second.EmbedCalls != 0 {
		t.Fatalf("a repeat run must reuse stored vectors, got %+v", second)
	}

	count, err := db.Collection("tafsir_chunks").CountDocuments(ctx, bson.M{})
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 2 {
		t.Fatalf("expected the index to stay at 2 chunks, got %d", count)
	}
}
