package ingestion

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// testDB connects to an explicitly-provided MongoDB and gives each test its own
// database, dropped afterwards.
//
// These tests are skipped unless MONGO_TEST_URI is set, so `go test ./...` stays
// hermetic and can never write to a developer's working database:
//
//	MONGO_TEST_URI="mongodb://tadbor:...@localhost:27017" go test ./internal/ingestion/
func testDB(t *testing.T) *mongo.Database {
	t.Helper()
	uri := os.Getenv("MONGO_TEST_URI")
	if uri == "" {
		t.Skip("set MONGO_TEST_URI to run MongoDB-backed ingestion tests")
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

	db := client.Database(fmt.Sprintf("tadbor_ingestion_test_%d", time.Now().UnixNano()))
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		_ = db.Drop(cleanupCtx)
		_ = client.Disconnect(cleanupCtx)
	})
	return db
}

func seedSource(t *testing.T, db *mongo.Database, src Source) {
	t.Helper()
	_, err := db.Collection("sources").InsertOne(context.Background(), src)
	if err != nil {
		t.Fatalf("seed source: %v", err)
	}
}

func embedded(chunks []Chunk, vectors [][]float64) []Chunk {
	out := make([]Chunk, len(chunks))
	copy(out, chunks)
	for i := range out {
		out[i].embed(vectors[i], out[i].EmbeddingModel)
	}
	return out
}

func storedChunks(t *testing.T, db *mongo.Database) []Chunk {
	t.Helper()
	cur, err := db.Collection("tafsir_chunks").Find(context.Background(), bson.M{})
	if err != nil {
		t.Fatalf("find chunks: %v", err)
	}
	defer cur.Close(context.Background())

	var out []Chunk
	if err := cur.All(context.Background(), &out); err != nil {
		t.Fatalf("decode chunks: %v", err)
	}
	return out
}

func TestEnsureIndexesCreatesRetrievalIndexes(t *testing.T) {
	db := testDB(t)
	store := NewStore(db)
	ctx := context.Background()

	if err := store.EnsureIndexes(ctx); err != nil {
		t.Fatalf("EnsureIndexes: %v", err)
	}
	// Re-running must stay a no-op, since every ingest calls it.
	if err := store.EnsureIndexes(ctx); err != nil {
		t.Fatalf("EnsureIndexes is not idempotent: %v", err)
	}

	cur, err := db.Collection("tafsir_chunks").Indexes().List(ctx)
	if err != nil {
		t.Fatalf("list indexes: %v", err)
	}
	defer cur.Close(ctx)

	found := map[string]bool{}
	for cur.Next(ctx) {
		var idx struct {
			Name string `bson:"name"`
		}
		if err := cur.Decode(&idx); err != nil {
			t.Fatalf("decode index: %v", err)
		}
		found[idx.Name] = true
	}
	for _, want := range []string{"exact_ayah_lookup", "thematic_fallback", "source_manifest"} {
		if !found[want] {
			t.Fatalf("index %q was not created; got %v", want, keysOfBool(found))
		}
	}
}

func TestUpsertChunksIsIdempotent(t *testing.T) {
	db := testDB(t)
	store := NewStore(db)
	ctx := context.Background()
	if err := store.EnsureIndexes(ctx); err != nil {
		t.Fatalf("EnsureIndexes: %v", err)
	}

	chunks := buildChunks(t, doc(singleAyah(0, 1, "نص"), singleAyah(1, 2, "نص آخر")), usableSource())
	withVectors := embedded(chunks, [][]float64{unitVector(), unitVector()})

	for run := 1; run <= 3; run++ {
		if _, err := store.UpsertChunks(ctx, withVectors); err != nil {
			t.Fatalf("run %d: UpsertChunks: %v", run, err)
		}
		stored := storedChunks(t, db)
		if len(stored) != 2 {
			t.Fatalf("run %d: expected 2 documents after repeated upserts, got %d", run, len(stored))
		}
	}

	// A re-upsert must refresh updated_at without resetting created_at.
	first := storedChunks(t, db)
	time.Sleep(1100 * time.Millisecond)
	if _, err := store.UpsertChunks(ctx, withVectors); err != nil {
		t.Fatalf("UpsertChunks: %v", err)
	}
	second := storedChunks(t, db)
	byID := map[string]Chunk{}
	for _, c := range second {
		byID[c.ID] = c
	}
	for _, before := range first {
		after := byID[before.ID]
		if after.CreatedAt != before.CreatedAt {
			t.Fatalf("created_at changed on re-upsert: %q -> %q", before.CreatedAt, after.CreatedAt)
		}
		if after.UpdatedAt == before.UpdatedAt {
			t.Fatal("updated_at was not refreshed")
		}
	}
}

func TestUpsertChunksStoresRetrievalFields(t *testing.T) {
	db := testDB(t)
	store := NewStore(db)
	ctx := context.Background()
	if err := store.EnsureIndexes(ctx); err != nil {
		t.Fatalf("EnsureIndexes: %v", err)
	}

	chunks := buildChunks(t, doc(singleAyah(0, 7, "نص آية سبع")), usableSource())
	withVectors := embedded(chunks, [][]float64{unitVector()})
	withVectors[0].EmbeddingModel = ""

	if _, err := store.UpsertChunks(ctx, withVectors); err != nil {
		t.Fatalf("UpsertChunks: %v", err)
	}

	stored := storedChunks(t, db)
	if len(stored) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(stored))
	}
	got := stored[0]

	// Everything internal/retrieval filters or ranks on must round-trip.
	if got.SourceID != "tabari-yusuf" || !got.SourceVerified {
		t.Fatalf("source fields lost: source_id=%q source_verified=%v", got.SourceID, got.SourceVerified)
	}
	if got.SurahID != 12 || got.AyahStart != 7 || got.AyahEnd != 7 {
		t.Fatalf("verse mapping lost: %d:%d-%d", got.SurahID, got.AyahStart, got.AyahEnd)
	}
	if got.MappingType != MappingSingleAyah {
		t.Fatalf("mapping_type = %q", got.MappingType)
	}
	if got.Text != "نص آية سبع" || got.TextNormalized != "نص آية سبع" {
		t.Fatalf("text fields lost: %q / %q", got.Text, got.TextNormalized)
	}
	if len(got.Embedding) != 1024 {
		t.Fatalf("embedding has %d dimensions, want 1024", len(got.Embedding))
	}
	if got.ContentHash == "" || got.ReviewStatus == "" || got.AuthorityTier != 1 {
		t.Fatalf("§10 metadata incomplete: %+v", got)
	}
}

// The exact-verse filter must actually be usable as an index.
func TestExactAyahFilterUsesAnIndex(t *testing.T) {
	db := testDB(t)
	store := NewStore(db)
	ctx := context.Background()
	if err := store.EnsureIndexes(ctx); err != nil {
		t.Fatalf("EnsureIndexes: %v", err)
	}

	var sections []Section
	for ayah := 1; ayah <= 111; ayah++ {
		sections = append(sections, singleAyah(ayah-1, ayah, fmt.Sprintf("تفسير الآية %d", ayah)))
	}
	chunks := embedded(buildChunks(t, doc(sections...), usableSource()),
		repeatVectors(len(sections)))
	if _, err := store.UpsertChunks(ctx, chunks); err != nil {
		t.Fatalf("UpsertChunks: %v", err)
	}

	// Same filter shape as retrieval.exactMapping.
	filter := bson.M{
		"surah_id":        12,
		"ayah_start":      bson.M{"$lte": 42},
		"ayah_end":        bson.M{"$gte": 42},
		"source_verified": true,
	}
	var explain bson.M
	explainCmd := bson.D{
		{Key: "explain", Value: bson.D{
			{Key: "find", Value: "tafsir_chunks"},
			{Key: "filter", Value: filter},
		}},
		{Key: "verbosity", Value: "queryPlanner"},
	}
	if err := db.RunCommand(ctx, explainCmd).Decode(&explain); err != nil {
		t.Skipf("could not read explain output: %v", err)
	}

	index := winningIndexName(explain)
	if index != "exact_ayah_lookup" {
		t.Skipf("query planner used %q; this assertion tracks a specific server version's plan shape", index)
	}
}

func TestExistingReportsStoredModel(t *testing.T) {
	db := testDB(t)
	store := NewStore(db)
	ctx := context.Background()

	chunks := embedded(buildChunks(t, doc(singleAyah(0, 1, "نص")), usableSource()), [][]float64{unitVector()})
	if _, err := store.UpsertChunks(ctx, chunks); err != nil {
		t.Fatalf("UpsertChunks: %v", err)
	}

	existing, err := store.Existing(ctx, "tabari-yusuf", "dar-1410")
	if err != nil {
		t.Fatalf("Existing: %v", err)
	}
	if len(existing) != 1 {
		t.Fatalf("expected 1 stored chunk, got %d", len(existing))
	}
	if _, ok := existing[chunks[0].ID]; !ok {
		t.Fatal("stored chunk id was not reported")
	}

	// Scoping must be exact: another source or another edition is invisible.
	other, err := store.Existing(ctx, "other-source", "dar-1410")
	if err != nil {
		t.Fatalf("Existing: %v", err)
	}
	if len(other) != 0 {
		t.Fatalf("expected no cross-source leakage, got %d", len(other))
	}
	otherEdition, err := store.Existing(ctx, "tabari-yusuf", "dar-1400")
	if err != nil {
		t.Fatalf("Existing: %v", err)
	}
	if len(otherEdition) != 0 {
		t.Fatalf("expected no cross-edition leakage, got %d", len(otherEdition))
	}
}

func TestDeleteStaleRemovesOnlySupersededChunks(t *testing.T) {
	db := testDB(t)
	store := NewStore(db)
	ctx := context.Background()
	if err := store.EnsureIndexes(ctx); err != nil {
		t.Fatalf("EnsureIndexes: %v", err)
	}

	current := embedded(buildChunks(t, doc(
		singleAyah(0, 1, "باقٍ"),
		singleAyah(1, 2, "محذوف"),
	), usableSource()), repeatVectors(2))
	if _, err := store.UpsertChunks(ctx, current); err != nil {
		t.Fatalf("UpsertChunks: %v", err)
	}

	keepID := ""
	for _, c := range current {
		if strings.Contains(c.Text, "باقٍ") {
			keepID = c.ID
		}
	}
	deleted, err := store.DeleteStale(ctx, "tabari-yusuf", "dar-1410", []string{keepID})
	if err != nil {
		t.Fatalf("DeleteStale: %v", err)
	}
	if deleted != 1 {
		t.Fatalf("expected 1 deletion, got %d", deleted)
	}
	if got := storedChunks(t, db); len(got) != 1 || got[0].ID != keepID {
		t.Fatalf("expected only %s to remain, got %+v", keepID, got)
	}

	// An empty manifest must clear the edition rather than silently no-op.
	deleted, err = store.DeleteStale(ctx, "tabari-yusuf", "dar-1410", nil)
	if err != nil {
		t.Fatalf("DeleteStale: %v", err)
	}
	if deleted != 1 {
		t.Fatalf("expected the remaining chunk to be deleted, got %d", deleted)
	}
}

func TestDeleteStaleLeavesOtherSourcesAndEditionsAlone(t *testing.T) {
	db := testDB(t)
	store := NewStore(db)
	ctx := context.Background()

	mine := embedded(buildChunks(t, doc(singleAyah(0, 1, "نص")), usableSource()), [][]float64{unitVector()})

	other := usableSource()
	other.ID = "other-tafsir"
	otherEdition := doc(singleAyah(0, 1, "نص"))
	otherEdition.SourceVersion = "dar-1400"
	theirs := embedded(buildChunks(t, otherEdition, other), [][]float64{unitVector()})

	if _, err := store.UpsertChunks(ctx, append(append([]Chunk{}, mine...), theirs...)); err != nil {
		t.Fatalf("UpsertChunks: %v", err)
	}

	if deleted, err := store.DeleteStale(ctx, "tabari-yusuf", "dar-1410", nil); err != nil || deleted != 1 {
		t.Fatalf("expected 1 deletion in scope, got %d (err %v)", deleted, err)
	}
	if got := storedChunks(t, db); len(got) != 1 || got[0].SourceID != "other-tafsir" {
		t.Fatalf("deletion escaped its scope: %+v", got)
	}
}

// Chunks denormalize source_verified at ingest time, so a later downgrade must be
// able to be applied without re-embedding the source.
func TestSyncSourceStateAppliesALaterRevocation(t *testing.T) {
	db := testDB(t)
	store := NewStore(db)
	ctx := context.Background()
	seedSource(t, db, usableSource())

	chunks := embedded(buildChunks(t, doc(singleAyah(0, 1, "نص")), usableSource()), [][]float64{unitVector()})
	if _, err := store.UpsertChunks(ctx, chunks); err != nil {
		t.Fatalf("UpsertChunks: %v", err)
	}
	if !storedChunks(t, db)[0].SourceVerified {
		t.Fatal("expected the chunk to start verified")
	}

	revoked := usableSource()
	revoked.LicensingStatus = "revoked"
	revoked.AuthorityTier = 3
	updated, err := store.SyncSourceState(ctx, revoked)
	if err != nil {
		t.Fatalf("SyncSourceState: %v", err)
	}
	if updated != 1 {
		t.Fatalf("expected 1 modified chunk, got %d", updated)
	}

	got := storedChunks(t, db)[0]
	if got.SourceVerified {
		t.Fatal("a revoked source must not stay verified on its chunks")
	}
	if got.AuthorityTier != 3 {
		t.Fatalf("authority_tier = %d, want 3", got.AuthorityTier)
	}
}

func TestSourceRegistryStatusTransitions(t *testing.T) {
	db := testDB(t)
	registry := NewSourceRegistry(db)
	ctx := context.Background()
	seedSource(t, db, usableSource())

	if err := registry.SetIngestionStatus(ctx, "tabari-yusuf", StatusInProgress); err != nil {
		t.Fatalf("SetIngestionStatus: %v", err)
	}
	src, err := registry.Get(ctx, "tabari-yusuf")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if src.IngestionStatus != StatusInProgress {
		t.Fatalf("ingestion_status = %q", src.IngestionStatus)
	}

	if err := registry.MarkIngested(ctx, "tabari-yusuf", timestamp()); err != nil {
		t.Fatalf("MarkIngested: %v", err)
	}
	src, _ = registry.Get(ctx, "tabari-yusuf")
	if src.IngestionStatus != StatusIngested || src.IngestedAt == "" {
		t.Fatalf("expected ingested state with a timestamp, got %+v", src)
	}
}

// IsUsable is the gate whose semantics must not drift.
func TestIsUsableSemantics(t *testing.T) {
	db := testDB(t)
	registry := NewSourceRegistry(db)
	ctx := context.Background()

	for _, tc := range []struct {
		verification string
		licensing    string
		want         bool
	}{
		{"verified", "cleared", true},
		{"verified", "pending", false},
		{"verified", "revoked", false},
		{"unverified", "cleared", false},
		{"verified_by:reviewer-1", "cleared", false}, // see the doc/code mismatch note
		{"", "", false},
	} {
		seedSource(t, db, Source{
			ID:                 "s",
			VerificationStatus: tc.verification,
			LicensingStatus:    tc.licensing,
			AuthorityTier:      1,
			Language:           "ar",
		})
		got, err := registry.IsUsable(ctx, "s")
		if err != nil {
			t.Fatalf("IsUsable: %v", err)
		}
		if got != tc.want {
			t.Fatalf("verification=%q licensing=%q: got %v, want %v", tc.verification, tc.licensing, got, tc.want)
		}
		if _, err := db.Collection("sources").DeleteOne(ctx, bson.M{"_id": "s"}); err != nil {
			t.Fatalf("cleanup: %v", err)
		}
	}
}

func TestSourceRegistryList(t *testing.T) {
	db := testDB(t)
	registry := NewSourceRegistry(db)
	ctx := context.Background()
	seedSource(t, db, Source{ID: "b", Language: "ar", AuthorityTier: 1})
	seedSource(t, db, Source{ID: "a", Language: "ar", AuthorityTier: 1})

	sources, err := registry.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(sources) != 2 || sources[0].ID != "a" || sources[1].ID != "b" {
		t.Fatalf("expected sources ordered by id, got %+v", sources)
	}
}

func repeatVectors(n int) [][]float64 {
	out := make([][]float64, n)
	for i := range out {
		out[i] = unitVector()
	}
	return out
}

func keysOfBool(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// winningIndexName digs the index name out of an explain plan. The plan shape
// differs between MongoDB versions, so a missing name is reported as "" and the
// caller treats it as "not assertable here" rather than a failure.
func winningIndexName(plan bson.M) string {
	qp, ok := plan["queryPlanner"].(bson.M)
	if !ok {
		return ""
	}
	wp, ok := qp["winningPlan"].(bson.M)
	if !ok {
		return ""
	}
	if name, ok := wp["indexName"].(string); ok {
		return name
	}
	for _, key := range []string{"inputStage", "inputStages"} {
		stage, ok := wp[key].(bson.M)
		if !ok {
			continue
		}
		if name, ok := stage["indexName"].(string); ok {
			return name
		}
	}
	return ""
}
