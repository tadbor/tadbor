package retrieval

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

func testDB(t *testing.T) *mongo.Database {
	t.Helper()
	uri := os.Getenv("MONGO_TEST_URI")
	if uri == "" {
		t.Skip("set MONGO_TEST_URI to run MongoDB-backed retrieval tests")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client, err := mongo.Connect(ctx, options.Client().ApplyURI(uri))
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	if err := client.Ping(ctx, nil); err != nil {
		client.Disconnect(ctx)
		t.Fatalf("ping: %v", err)
	}

	db := client.Database(fmt.Sprintf("tadbor_retrieval_test_%d", time.Now().UnixNano()))
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		_ = db.Drop(cleanupCtx)
		_ = client.Disconnect(cleanupCtx)
	})
	return db
}

// stubEmbedder records what it was asked to embed and returns a fixed vector.
type stubEmbedder struct {
	calls [][]string
	vec   []float64
	err   error
}

func (s *stubEmbedder) Embed(_ context.Context, texts []string) ([][]float64, error) {
	s.calls = append(s.calls, texts)
	if s.err != nil {
		return nil, s.err
	}
	out := make([][]float64, len(texts))
	for i := range texts {
		out[i] = s.vec
	}
	return out, nil
}

func insertChunk(t *testing.T, db *mongo.Database, c bson.M) {
	t.Helper()
	_, err := db.Collection("tafsir_chunks").InsertOne(context.Background(), c)
	if err != nil {
		t.Fatalf("insert chunk: %v", err)
	}
}

func exactChunk(ayah int) bson.M {
	return bson.M{
		"_id":             fmt.Sprintf("exact-%d", ayah),
		"source_verified": true,
		"surah_id":        12,
		"ayah_start":      ayah,
		"ayah_end":        ayah,
		"mapping_type":    "single_ayah",
		"text":            fmt.Sprintf("commentary on %d", ayah),
		"embedding":       []float64{1, 0, 0},
	}
}

func thematicChunk(id string) bson.M {
	return bson.M{
		"_id":             id,
		"source_verified": true,
		"surah_id":        12,
		"ayah_start":      0,
		"ayah_end":        0,
		"mapping_type":    "thematic",
		"text":            "thematic commentary " + id,
		"embedding":       []float64{0, 1, 0},
	}
}

func insertAyahText(t *testing.T, db *mongo.Database, ayah int, text string) {
	t.Helper()
	_, err := db.Collection("ayahs").InsertOne(context.Background(), bson.M{
		"surah_id":     12,
		"ayah_number":  ayah,
		"text_uthmani": text,
	})
	if err != nil {
		t.Fatalf("insert ayah: %v", err)
	}
}

// The regression this file exists for: with no query embedding, every thematic
// candidate scores cosine 0.0 against the nil query, so ranking them would
// return arbitrary rows in an arbitrary order. Part 1 §12 requires a hard stop
// instead, because the generation pipeline treats whatever comes back as
// evidence.
func TestGetEvidenceWithoutQueryEmbeddingRefusesThematicChunks(t *testing.T) {
	db := testDB(t)
	insertChunk(t, db, thematicChunk("t1"))
	insertChunk(t, db, thematicChunk("t2"))
	insertChunk(t, db, thematicChunk("t3"))
	svc := NewService(db, nil)

	got, err := svc.GetEvidence(context.Background(), 12, 7, nil)
	if !errors.Is(err, ErrInsufficientEvidence) {
		t.Fatalf("expected ErrInsufficientEvidence, got chunks=%d err=%v", len(got), err)
	}
	if len(got) != 0 {
		t.Fatalf("expected no evidence, got %d chunks", len(got))
	}
}

// An empty (non-nil) slice is the same "no query" case and must behave alike.
func TestGetEvidenceWithEmptySliceRefusesThematicChunks(t *testing.T) {
	db := testDB(t)
	insertChunk(t, db, thematicChunk("t1"))
	svc := NewService(db, nil)

	if _, err := svc.GetEvidence(context.Background(), 12, 7, []float64{}); !errors.Is(err, ErrInsufficientEvidence) {
		t.Fatalf("expected ErrInsufficientEvidence for an empty query embedding, got %v", err)
	}
}

// With a real query vector the thematic fallback is legitimate and must work.
func TestGetEvidenceWithQueryEmbeddingReturnsThematicChunks(t *testing.T) {
	db := testDB(t)
	insertChunk(t, db, thematicChunk("t1"))
	svc := NewService(db, nil)

	got, err := svc.GetEvidence(context.Background(), 12, 7, []float64{0, 1, 0})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 thematic chunk, got %d", len(got))
	}
}

// Exact mapping must be preferred over thematic evidence when both exist, and
// must not require an embedder at all.
func TestExactMappingWinsAndNeedsNoEmbedder(t *testing.T) {
	db := testDB(t)
	insertChunk(t, db, exactChunk(3))
	insertChunk(t, db, thematicChunk("t1"))
	insertAyahText(t, db, 3, "verse three")

	stub := &stubEmbedder{vec: []float64{0, 1, 0}}
	svc := NewService(db, stub)

	got, err := svc.GetEvidenceForAyah(context.Background(), 12, 3)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 || got[0].MappingType != "single_ayah" {
		t.Fatalf("expected the exact single_ayah chunk, got %+v", got)
	}
	if len(stub.calls) != 0 {
		t.Fatalf("exact evidence must not cost an embedding call, got %v", stub.calls)
	}
}

// The whole point of computing the query lazily: the common path (mapped
// evidence exists) must not reach the embedding provider at all.
func TestGetEvidenceForAyahEmbedsOnlyWhenThereIsAGap(t *testing.T) {
	db := testDB(t)
	insertChunk(t, db, thematicChunk("t1"))
	insertAyahText(t, db, 9, "verse nine")

	stub := &stubEmbedder{vec: []float64{0, 1, 0}}
	svc := NewService(db, stub)

	got, err := svc.GetEvidenceForAyah(context.Background(), 12, 9)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected the thematic chunk, got %d", len(got))
	}
	if len(stub.calls) != 1 || len(stub.calls[0]) != 1 || stub.calls[0][0] != "verse nine" {
		t.Fatalf("expected exactly one embed call for the verse text, got %v", stub.calls)
	}
}

// Without an embedder a gap is a gap, not a licence to return unranked chunks.
func TestGetEvidenceForAyahWithoutEmbedderReportsTheGap(t *testing.T) {
	db := testDB(t)
	insertChunk(t, db, thematicChunk("t1"))
	insertAyahText(t, db, 9, "verse nine")

	if _, err := NewService(db, nil).GetEvidenceForAyah(context.Background(), 12, 9); !errors.Is(err, ErrInsufficientEvidence) {
		t.Fatalf("expected ErrInsufficientEvidence, got %v", err)
	}
}

// A provider outage must not be reported as "no evidence found" — the two mean
// different things and conflating them lets an outage look like a content gap.
func TestGetEvidenceForAyahSurfacesProviderFailureAsAnError(t *testing.T) {
	db := testDB(t)
	insertAyahText(t, db, 9, "verse nine")

	stub := &stubEmbedder{err: errors.New("502 from provider")}
	svc := NewService(db, stub)

	got, err := svc.GetEvidenceForAyah(context.Background(), 12, 9)
	if err == nil {
		t.Fatal("expected the provider failure to surface")
	}
	if errors.Is(err, ErrInsufficientEvidence) {
		t.Fatal("provider failure was misreported as insufficient evidence")
	}
	if len(got) != 0 {
		t.Fatalf("expected no evidence alongside the error, got %d", len(got))
	}
}

// A verse with no loaded text cannot produce a thematic query, so it is a gap.
func TestGetEvidenceForAyahWithoutAyahTextReportsTheGap(t *testing.T) {
	db := testDB(t)
	insertChunk(t, db, thematicChunk("t1"))

	svc := NewService(db, &stubEmbedder{vec: []float64{0, 1, 0}})
	if _, err := svc.GetEvidenceForAyah(context.Background(), 12, 42); !errors.Is(err, ErrInsufficientEvidence) {
		t.Fatalf("expected ErrInsufficientEvidence for an ayah with no text, got %v", err)
	}
}

// source_verified is the gate that keeps unapproved sources out of answers, in
// both the exact and the thematic path.
func TestUnverifiedSourcesAreNeverReturned(t *testing.T) {
	db := testDB(t)
	unverifiedExact := exactChunk(4)
	unverifiedExact["_id"] = "unverified-exact"
	unverifiedExact["source_verified"] = false
	insertChunk(t, db, unverifiedExact)

	unverifiedThematic := thematicChunk("unverified-thematic")
	unverifiedThematic["source_verified"] = false
	insertChunk(t, db, unverifiedThematic)

	svc := NewService(db, nil)

	if _, err := svc.GetEvidence(context.Background(), 12, 4, []float64{0, 1, 0}); !errors.Is(err, ErrInsufficientEvidence) {
		t.Fatalf("unverified source leaked through retrieval: %v", err)
	}
}

func TestCosineSimilarityEdgeCases(t *testing.T) {
	for _, tc := range []struct {
		name string
		a, b []float64
		want float64
	}{
		{"identical unit vectors", []float64{1, 0}, []float64{1, 0}, 1},
		{"orthogonal", []float64{1, 0}, []float64{0, 1}, 0},
		{"opposite", []float64{1, 0}, []float64{-1, 0}, -1},
		{"nil query", nil, []float64{1, 0}, 0},
		{"length mismatch", []float64{1, 0}, []float64{1, 0, 0}, 0},
		{"zero vector", []float64{0, 0}, []float64{1, 0}, 0},
		{"scaled", []float64{3, 4}, []float64{6, 8}, 1},
	} {
		if got := cosineSimilarity(tc.a, tc.b); got != tc.want {
			t.Fatalf("%s: got %v, want %v", tc.name, got, tc.want)
		}
	}
}
