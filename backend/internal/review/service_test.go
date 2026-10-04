package review

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

// testDB mirrors internal/ingestion's helper: a per-test database, dropped
// afterwards, and skipped entirely unless MONGO_TEST_URI is set so `go test ./...`
// can never touch a working database.
//
//	MONGO_TEST_URI="mongodb://tadbor:...@localhost:27017" go test ./internal/review/
func testDB(t *testing.T) *mongo.Database {
	t.Helper()
	uri := os.Getenv("MONGO_TEST_URI")
	if uri == "" {
		t.Skip("set MONGO_TEST_URI to run MongoDB-backed review tests")
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

	db := client.Database(fmt.Sprintf("tadbor_review_test_%d", time.Now().UnixNano()))
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		_ = db.Drop(cleanupCtx)
		_ = client.Disconnect(cleanupCtx)
	})
	return db
}

// seedQueueItem writes one pending explanation plus the chunk it cites and the
// ayah it explains, which is the minimum for the dashboard to have something to
// show.
func seedQueueItem(t *testing.T, db *mongo.Database, item QueueItem, chunk *Evidence, ayahText string) {
	t.Helper()
	ctx := context.Background()

	if chunk != nil {
		if _, err := db.Collection("tafsir_chunks").InsertOne(ctx, chunk); err != nil {
			t.Fatalf("seed chunk: %v", err)
		}
	}
	if ayahText != "" {
		ayah := bson.M{
			"surah_id": item.SurahID, "ayah_number": item.AyahStart, "text_uthmani": ayahText,
		}
		if _, err := db.Collection("ayahs").InsertOne(ctx, ayah); err != nil {
			t.Fatalf("seed ayah: %v", err)
		}
	}
	if _, err := db.Collection("explanations").InsertOne(ctx, item); err != nil {
		t.Fatalf("seed explanation: %v", err)
	}
}

func sampleItem(id string, refs ...string) QueueItem {
	return QueueItem{
		ID:                    id,
		SurahID:               12,
		AyahStart:             1,
		AyahEnd:               1,
		Style:                 "simplified_ar",
		Level:                 "beginner",
		Text:                  " dissenting text",
		SourceRefs:            refs,
		ReviewStatus:          "pending",
		SourceVersionSnapshot: "ibn-kathir-surah-12 snapshot",
	}
}

func sampleChunk(id string) *Evidence {
	return &Evidence{
		ChunkID:       id,
		Text:          "قال المفسر:_then the commentary_",
		AyahStart:     1,
		AyahEnd:       1,
		MappingType:   "single_ayah",
		SourceID:      "ibn-kathir-ar",
		SourceVersion: "ibn-kathir-surah-12 snapshot",
		ContentHash:   "sha256:abc",
	}
}

// A reviewer can only check grounding if the queue resolves source_refs into
// readable text, attaches the verse, and names the source.
func TestPendingQueueResolvesEvidenceAyahAndCitation(t *testing.T) {
	db := testDB(t)
	svc := NewService(db)

	chunk := sampleChunk("chunk-1")
	item := sampleItem("exp-1", "chunk-1")
	seedQueueItem(t, db, item, chunk, "يوسف")

	_, err := db.Collection("sources").InsertOne(context.Background(), bson.M{
		"_id": "ibn-kathir-ar", "title": "تفسير ابن كثير", "author": "ابن كثير", "edition": "ar-tafsir-ibn-kathir",
	})
	if err != nil {
		t.Fatalf("seed source: %v", err)
	}

	queue, err := svc.PendingQueue(context.Background())
	if err != nil {
		t.Fatalf("PendingQueue: %v", err)
	}
	if len(queue) != 1 {
		t.Fatalf("queue length = %d, want 1", len(queue))
	}

	got := queue[0]
	if len(got.Evidence) != 1 || got.Evidence[0].ChunkID != "chunk-1" {
		t.Fatalf("evidence = %+v, want the cited chunk", got.Evidence)
	}
	if len(got.MissingRefs) != 0 {
		t.Errorf("missing refs = %v, want none", got.MissingRefs)
	}
	if got.AyahText != "يوسف" {
		t.Errorf("ayah text = %q, want the corpus text", got.AyahText)
	}
	if got.Citation.Title != "تفسير ابن كثير" || got.Citation.Author != "ابن كثير" {
		t.Errorf("citation = %+v, want the registered source", got.Citation)
	}
	if got.Citation.Version != chunk.SourceVersion {
		t.Errorf("citation version = %q, want %q", got.Citation.Version, chunk.SourceVersion)
	}
	if got.GroundingVersionMismatch {
		t.Error("grounding mismatch reported for an explanation whose evidence matches its snapshot")
	}
}

// A ref that no longer resolves must be surfaced, not skipped: an explanation
// grounded in a chunk the reviewer cannot open is not reviewable.
func TestPendingQueueReportsUnresolvableRefs(t *testing.T) {
	db := testDB(t)
	svc := NewService(db)

	seedQueueItem(t, db, sampleItem("exp-1", "chunk-gone"), nil, "يوسف")

	queue, err := svc.PendingQueue(context.Background())
	if err != nil {
		t.Fatalf("PendingQueue: %v", err)
	}
	if len(queue[0].Evidence) != 0 {
		t.Errorf("evidence = %+v, want none", queue[0].Evidence)
	}
	if len(queue[0].MissingRefs) != 1 || queue[0].MissingRefs[0] != "chunk-gone" {
		t.Errorf("missing refs = %v, want [chunk-gone]", queue[0].MissingRefs)
	}
	if queue[0].Citation.Title != "" {
		t.Errorf("citation = %+v, want empty when there is no evidence to attribute", queue[0].Citation)
	}
}

// Part 1 §20: content generated against a source version that has since moved is
// not the same text the reviewer would be approving.
func TestPendingQueueFlagsGroundingVersionMismatch(t *testing.T) {
	db := testDB(t)
	svc := NewService(db)

	chunk := sampleChunk("chunk-1")
	chunk.SourceVersion = "a newer snapshot"
	item := sampleItem("exp-1", "chunk-1")
	seedQueueItem(t, db, item, chunk, "يوسف")

	queue, err := svc.PendingQueue(context.Background())
	if err != nil {
		t.Fatalf("PendingQueue: %v", err)
	}
	if !queue[0].GroundingVersionMismatch {
		t.Error("grounding mismatch not reported for evidence from a different source version")
	}
}

// Edit & Approve is only meaningful if it writes the reviewer's text. Before
// this, the decision was recorded and the generated text published unchanged.
func TestEditApproveWritesTheReviewersText(t *testing.T) {
	db := testDB(t)
	svc := NewService(db)
	seedQueueItem(t, db, sampleItem("exp-1", "chunk-1"), sampleChunk("chunk-1"), "يوسف")

	err := svc.Decide(context.Background(), "exp-1", "me", "edit_approve", "fixed the tense", "the corrected text")
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}

	var got QueueItem
	if err := db.Collection("explanations").FindOne(context.Background(), bson.M{"_id": "exp-1"}).Decode(&got); err != nil {
		t.Fatalf("read back: %v", err)
	}
	if got.Text != "the corrected text" {
		t.Errorf("text = %q, want the reviewer's edit", got.Text)
	}
	if got.ReviewStatus != "published" {
		t.Errorf("review_status = %q, want published", got.ReviewStatus)
	}
}

func TestEditApproveWithoutTextIsRejected(t *testing.T) {
	db := testDB(t)
	svc := NewService(db)
	seedQueueItem(t, db, sampleItem("exp-1"), nil, "يوسف")

	for _, text := range []string{"", "   ", "\n\t "} {
		if err := svc.Decide(context.Background(), "exp-1", "me", "edit_approve", "", text); err == nil {
			t.Errorf("Decide with text %q succeeded, want rejection", text)
		}
	}

	// A rejected edit must not have published anything.
	var got QueueItem
	if err := db.Collection("explanations").FindOne(context.Background(), bson.M{"_id": "exp-1"}).Decode(&got); err != nil {
		t.Fatalf("read back: %v", err)
	}
	if got.ReviewStatus != "pending" {
		t.Errorf("review_status = %q, want pending", got.ReviewStatus)
	}
}

// An unrecognised decision must not silently leave a document pending while a
// review record claims it was handled.
func TestUnknownDecisionIsRejectedAndRecordsNothing(t *testing.T) {
	db := testDB(t)
	svc := NewService(db)
	seedQueueItem(t, db, sampleItem("exp-1"), nil, "يوسف")

	err := svc.Decide(context.Background(), "exp-1", "me", "looks-good", "", "")
	if !errors.Is(err, ErrBadDecision) {
		t.Errorf("Decide error = %v, want ErrBadDecision so the handler can answer 400", err)
	}

	if n, _ := db.Collection("reviews").CountDocuments(context.Background(), bson.M{}); n != 0 {
		t.Errorf("reviews written = %d, want 0", n)
	}
}

func TestDecisionStatuses(t *testing.T) {
	cases := map[string]string{
		"approve":      "published",
		"edit_approve": "published",
		"reject":       "rejected",
		// Escalation asks for a ruling this reviewer cannot give, so the item
		// must stay in the queue rather than being resolved either way.
		"escalate": "pending",
	}
	for decision, want := range cases {
		t.Run(decision, func(t *testing.T) {
			db := testDB(t)
			svc := NewService(db)
			seedQueueItem(t, db, sampleItem("exp-1"), nil, "يوسف")

			text := "edited"
			if err := svc.Decide(context.Background(), "exp-1", "me", decision, "", text); err != nil {
				t.Fatalf("Decide: %v", err)
			}

			var got QueueItem
			if err := db.Collection("explanations").FindOne(context.Background(), bson.M{"_id": "exp-1"}).Decode(&got); err != nil {
				t.Fatalf("read back: %v", err)
			}
			if got.ReviewStatus != want {
				t.Errorf("review_status = %q, want %q", got.ReviewStatus, want)
			}
		})
	}
}

// A second submission must not overwrite the first decision, and must not leave a
// review record claiming it applied. The dashboard is a plain HTML form, so
// double submits are routine.
func TestSecondDecisionOnAnAlreadyDecidedItemIsRefused(t *testing.T) {
	db := testDB(t)
	svc := NewService(db)
	seedQueueItem(t, db, sampleItem("exp-1"), nil, "يوسف")

	if err := svc.Decide(context.Background(), "exp-1", "me", "approve", "", ""); err != nil {
		t.Fatalf("first Decide: %v", err)
	}
	err := svc.Decide(context.Background(), "exp-1", "me", "reject", "changed my mind", "")
	if !errors.Is(err, ErrNotPending) {
		t.Fatalf("second Decide error = %v, want ErrNotPending", err)
	}

	var got QueueItem
	if err := db.Collection("explanations").FindOne(context.Background(), bson.M{"_id": "exp-1"}).Decode(&got); err != nil {
		t.Fatalf("read back: %v", err)
	}
	if got.ReviewStatus != "published" {
		t.Errorf("review_status = %q, want published — the second decision must not win", got.ReviewStatus)
	}

	// The audit trail must show one decision, not two.
	var records []ReviewRecord
	cur, err := db.Collection("reviews").Find(context.Background(), bson.M{"explanation_id": "exp-1"})
	if err != nil {
		t.Fatalf("read reviews: %v", err)
	}
	if err := cur.All(context.Background(), &records); err != nil {
		t.Fatalf("decode reviews: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("review records = %d, want 1 — a refused decision must not be recorded as applied", len(records))
	}
	if records[0].Decision != "approve" {
		t.Errorf("recorded decision = %q, want the one that applied", records[0].Decision)
	}
}

// §20 requires the record to name the grounding it was reviewed against.
func TestReviewRecordNamesTheGroundingItWasReviewedAgainst(t *testing.T) {
	db := testDB(t)
	svc := NewService(db)

	item := sampleItem("exp-1")
	item.PromptVersion = "simplify-v3"
	item.ModelVersion = "gemini-2.5-flash"
	seedQueueItem(t, db, item, nil, "يوسف")

	if err := svc.Decide(context.Background(), "exp-1", "me", "approve", "clear", ""); err != nil {
		t.Fatalf("Decide: %v", err)
	}

	var record ReviewRecord
	if err := db.Collection("reviews").FindOne(context.Background(), bson.M{"explanation_id": "exp-1"}).Decode(&record); err != nil {
		t.Fatalf("read review record: %v", err)
	}
	if record.SourceVersion != item.SourceVersionSnapshot {
		t.Errorf("source_version = %q, want %q", record.SourceVersion, item.SourceVersionSnapshot)
	}
	if record.PromptVersion != "simplify-v3" || record.ModelVersion != "gemini-2.5-flash" {
		t.Errorf("record = %+v, want the prompt and model in effect at review time", record)
	}
	if record.ReviewerID != "me" || record.CreatedAt == "" {
		t.Errorf("record = %+v, want reviewer and timestamp", record)
	}
}

func TestDecideOnAMissingExplanationIsAnError(t *testing.T) {
	db := testDB(t)
	svc := NewService(db)

	if err := svc.Decide(context.Background(), "nope", "me", "approve", "", ""); err == nil {
		t.Fatal("Decide on a missing explanation succeeded, want an error")
	}
}

func TestEmptyQueueIsEmptyNotNull(t *testing.T) {
	db := testDB(t)
	svc := NewService(db)

	queue, err := svc.PendingQueue(context.Background())
	if err != nil {
		t.Fatalf("PendingQueue: %v", err)
	}
	if queue == nil {
		t.Fatal("PendingQueue returned nil, want an empty slice so the API answers [] not null")
	}
	if len(queue) != 0 {
		t.Errorf("queue length = %d, want 0", len(queue))
	}
}
