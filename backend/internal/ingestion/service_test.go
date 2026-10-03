package ingestion

import (
	"context"
	"errors"
	"strings"
	"testing"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"

	"tadbor/backend/internal/embedding"
	"tadbor/backend/internal/retrieval"
)

// fakeEmbedder records every call so tests can assert how much a run cost.
type fakeEmbedder struct {
	calls   [][]string
	failAt  int // 1-based index of the batch that should fail; 0 means never
	failErr error
}

func (f *fakeEmbedder) Embed(_ context.Context, texts []string) ([][]float64, error) {
	f.calls = append(f.calls, append([]string(nil), texts...))
	if f.failAt > 0 && len(f.calls) == f.failAt {
		return nil, f.failErr
	}
	return repeatVectors(len(texts)), nil
}

func (f *fakeEmbedder) total() int {
	n := 0
	for _, c := range f.calls {
		n += len(c)
	}
	return n
}

func ingestDoc(texts ...string) Document {
	d := doc()
	for i, txt := range texts {
		d.Sections = append(d.Sections, singleAyah(i, i+1, txt))
	}
	return d
}

func newTestService(t *testing.T, db *mongo.Database, e embedding.Embedder) *Service {
	t.Helper()
	return NewService(db, e)
}

// The gate must run before the provider is ever contacted: an unapproved source
// costs nothing.
func TestIngestRefusesUnusableSourcesWithoutEmbedding(t *testing.T) {
	for _, tc := range []struct {
		name string
		src  func() Source
	}{
		{"unverified", func() Source {
			s := usableSource()
			s.VerificationStatus = "unverified"
			return s
		}},
		{"licensing pending", func() Source {
			s := usableSource()
			s.LicensingStatus = "pending"
			return s
		}},
		{"tier unassigned", func() Source {
			s := usableSource()
			s.VerificationStatus = ""
			return s
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := testDB(t)
			seedSource(t, db, tc.src())
			emb := &fakeEmbedder{}

			_, err := newTestService(t, db, emb).IngestSource(context.Background(), "tabari-yusuf",
				[]Document{ingestDoc("نص")}, IngestOptions{})

			if !errors.Is(err, ErrSourceNotUsable) {
				t.Fatalf("expected ErrSourceNotUsable, got %v", err)
			}
			if len(emb.calls) != 0 {
				t.Fatalf("expected zero provider calls, got %d", len(emb.calls))
			}
			if got := storedChunks(t, db); len(got) != 0 {
				t.Fatalf("expected no indexed chunks, got %d", len(got))
			}
		})
	}
}

func TestIngestUnknownSource(t *testing.T) {
	db := testDB(t)
	emb := &fakeEmbedder{}

	_, err := newTestService(t, db, emb).IngestSource(context.Background(), "nope", nil, IngestOptions{})
	if err == nil {
		t.Fatal("expected an error for an unknown source")
	}
	if len(emb.calls) != 0 {
		t.Fatal("expected zero provider calls")
	}
}

// With no real corpus yet, an empty manifest is a completed no-op.
func TestIngestEmptyManifestIsANoOp(t *testing.T) {
	db := testDB(t)
	seedSource(t, db, usableSource())
	emb := &fakeEmbedder{}

	rep, err := newTestService(t, db, emb).IngestSource(context.Background(), "tabari-yusuf", nil, IngestOptions{})
	if err != nil {
		t.Fatalf("an empty manifest should not fail: %v", err)
	}
	if rep.TotalChunks != 0 || rep.EmbedCalls != 0 || rep.Written != 0 {
		t.Fatalf("expected an empty report, got %+v", rep)
	}
	if len(emb.calls) != 0 {
		t.Fatal("expected zero provider calls")
	}
	if got := storedChunks(t, db); len(got) != 0 {
		t.Fatalf("expected nothing indexed, got %d", len(got))
	}
}

func TestIngestWritesChunksAndRecordsProvenance(t *testing.T) {
	db := testDB(t)
	seedSource(t, db, usableSource())
	emb := &fakeEmbedder{}

	rep, err := newTestService(t, db, emb).IngestSource(context.Background(), "tabari-yusuf",
		[]Document{ingestDoc("نص آية واحدة", "نص آية اثنتين")}, IngestOptions{})
	if err != nil {
		t.Fatalf("IngestSource: %v", err)
	}

	if rep.TotalChunks != 2 || rep.Embedded != 2 || rep.Written != 2 || rep.EmbedCalls != 1 {
		t.Fatalf("unexpected report: %+v", rep)
	}
	if rep.SourceVersion != "dar-1410" {
		t.Fatalf("source_version = %q", rep.SourceVersion)
	}

	stored := storedChunks(t, db)
	if len(stored) != 2 {
		t.Fatalf("expected 2 indexed chunks, got %d", len(stored))
	}
	for _, c := range stored {
		if len(c.Embedding) != embedding.ExpectedDimensions {
			t.Fatalf("chunk %s stored %d dimensions", c.ID, len(c.Embedding))
		}
		if c.EmbeddingModel != embedding.ModelID {
			t.Fatalf("chunk %s model = %q", c.ID, c.EmbeddingModel)
		}
		if !c.EmbeddingNormalized {
			t.Fatalf("chunk %s not marked normalized", c.ID)
		}
		if c.EmbeddingHash == "" {
			t.Fatalf("chunk %s has no embedding hash", c.ID)
		}
		if c.CreatedAt == "" || c.UpdatedAt == "" {
			t.Fatalf("chunk %s missing timestamps", c.ID)
		}
	}

	src, err := NewSourceRegistry(db).Get(context.Background(), "tabari-yusuf")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if src.IngestionStatus != StatusIngested || src.IngestedAt == "" {
		t.Fatalf("source not marked ingested: %+v", src)
	}
}

// The whole point of deterministic ids: a second run over unchanged input must
// cost nothing and change nothing.
func TestIngestIsIdempotentAndReusesEmbeddings(t *testing.T) {
	db := testDB(t)
	seedSource(t, db, usableSource())
	docs := []Document{ingestDoc("نص آية واحدة", "نص آية اثنتين")}

	first := &fakeEmbedder{}
	svc := newTestService(t, db, first)
	if _, err := svc.IngestSource(context.Background(), "tabari-yusuf", docs, IngestOptions{}); err != nil {
		t.Fatalf("first run: %v", err)
	}
	if first.total() != 2 {
		t.Fatalf("first run should embed 2 texts, got %d", first.total())
	}

	second := &fakeEmbedder{}
	svc = newTestService(t, db, second)
	rep, err := svc.IngestSource(context.Background(), "tabari-yusuf", docs, IngestOptions{})
	if err != nil {
		t.Fatalf("second run: %v", err)
	}

	if second.total() != 0 {
		t.Fatalf("second run re-embedded %d texts; it must reuse stored vectors", second.total())
	}
	if rep.Reused != 2 || rep.Embedded != 0 || rep.EmbedCalls != 0 {
		t.Fatalf("unexpected report: %+v", rep)
	}
	if got := storedChunks(t, db); len(got) != 2 {
		t.Fatalf("expected the index to stay at 2 chunks, got %d", len(got))
	}
}

// Only the changed passage should be re-embedded.
func TestIngestEmbedsOnlyTheDelta(t *testing.T) {
	db := testDB(t)
	seedSource(t, db, usableSource())
	svc := newTestService(t, db, &fakeEmbedder{})
	if _, err := svc.IngestSource(context.Background(), "tabari-yusuf",
		[]Document{ingestDoc("الأصل", "الثابت")}, IngestOptions{}); err != nil {
		t.Fatalf("first run: %v", err)
	}

	emb := &fakeEmbedder{}
	svc = newTestService(t, db, emb)
	rep, err := svc.IngestSource(context.Background(), "tabari-yusuf",
		[]Document{ingestDoc("الأصل", "المعدَّل")}, IngestOptions{})
	if err != nil {
		t.Fatalf("second run: %v", err)
	}

	if rep.Reused != 1 || rep.Embedded != 1 {
		t.Fatalf("expected 1 reuse and 1 embed, got %+v", rep)
	}
	if got := emb.calls[0]; len(got) != 1 || !strings.Contains(got[0], "المعدَّل") {
		t.Fatalf("expected only the edited text to be embedded, got %v", got)
	}
	// The superseded chunk for the edited verse survives until pruning runs —
	// see TestIngestPrunesSupersededChunks for the other half of that contract.
	if got := storedChunks(t, db); len(got) != 3 {
		t.Fatalf("expected 3 chunks before pruning (2 superseded + 1 new), got %d", len(got))
	}
}

// Changing the embedding model must invalidate every stored vector: mixing two
// embedding spaces in one collection is not recoverable by retrieval.
func TestIngestReembedsWhenTheModelChanges(t *testing.T) {
	db := testDB(t)
	seedSource(t, db, usableSource())
	docs := []Document{ingestDoc("نص")}

	if _, err := newTestService(t, db, &fakeEmbedder{}).IngestSource(context.Background(), "tabari-yusuf", docs, IngestOptions{}); err != nil {
		t.Fatalf("first run: %v", err)
	}

	emb := &fakeEmbedder{}
	rep, err := newTestService(t, db, emb).IngestSource(context.Background(), "tabari-yusuf", docs,
		IngestOptions{EmbeddingModel: "BAAI/bge-m3-v2"})
	if err != nil {
		t.Fatalf("second run: %v", err)
	}
	if rep.Reused != 0 || rep.Embedded != 1 {
		t.Fatalf("expected a full re-embed, got %+v", rep)
	}
	if got := storedChunks(t, db); len(got) != 1 || got[0].EmbeddingModel != "BAAI/bge-m3-v2" {
		t.Fatalf("stored chunk not updated: %+v", storedChunks(t, db))
	}
}

// Pruning is what stops an edited passage from leaving a stale, still-servable
// chunk behind.
func TestIngestPrunesSupersededChunks(t *testing.T) {
	db := testDB(t)
	seedSource(t, db, usableSource())

	if _, err := newTestService(t, db, &fakeEmbedder{}).IngestSource(context.Background(), "tabari-yusuf",
		[]Document{ingestDoc("الأصل", "الثابت")}, IngestOptions{}); err != nil {
		t.Fatalf("first run: %v", err)
	}
	if got := storedChunks(t, db); len(got) != 2 {
		t.Fatalf("expected 2 chunks, got %d", len(got))
	}

	rep, err := newTestService(t, db, &fakeEmbedder{}).IngestSource(context.Background(), "tabari-yusuf",
		[]Document{ingestDoc("الأصل")}, IngestOptions{PruneStale: true})
	if err != nil {
		t.Fatalf("second run: %v", err)
	}
	if rep.Deleted != 1 {
		t.Fatalf("expected 1 pruned chunk, got %d", rep.Deleted)
	}
	if got := storedChunks(t, db); len(got) != 1 || !strings.Contains(got[0].Text, "الأصل") {
		t.Fatalf("expected only the remaining passage, got %+v", storedChunks(t, db))
	}
}

func TestIngestWithoutPruningLeavesStaleChunks(t *testing.T) {
	db := testDB(t)
	seedSource(t, db, usableSource())

	if _, err := newTestService(t, db, &fakeEmbedder{}).IngestSource(context.Background(), "tabari-yusuf",
		[]Document{ingestDoc("الأصل", "الثابت")}, IngestOptions{}); err != nil {
		t.Fatalf("first run: %v", err)
	}
	if _, err := newTestService(t, db, &fakeEmbedder{}).IngestSource(context.Background(), "tabari-yusuf",
		[]Document{ingestDoc("الأصل")}, IngestOptions{PruneStale: false}); err != nil {
		t.Fatalf("second run: %v", err)
	}
	if got := storedChunks(t, db); len(got) != 2 {
		t.Fatalf("expected the stale chunk to survive without pruning, got %d", len(got))
	}
}

// Appending to a manifest must not re-embed the passages already stored.
func TestIngestOnlyEmbedsAnAppendedSection(t *testing.T) {
	db := testDB(t)
	seedSource(t, db, usableSource())
	svc := newTestService(t, db, &fakeEmbedder{})

	if _, err := svc.IngestSource(context.Background(), "tabari-yusuf",
		[]Document{ingestDoc("الأولى", "الثانية")}, IngestOptions{PruneStale: true}); err != nil {
		t.Fatalf("first run: %v", err)
	}

	emb := &fakeEmbedder{}
	rep, err := newTestService(t, db, emb).IngestSource(context.Background(), "tabari-yusuf",
		[]Document{ingestDoc("الأولى", "الثانية", "الثالثة")}, IngestOptions{PruneStale: true})
	if err != nil {
		t.Fatalf("second run: %v", err)
	}

	if rep.Reused != 2 || rep.Embedded != 1 || rep.Deleted != 0 {
		t.Fatalf("appending should cost one embed and no deletions, got %+v", rep)
	}
	if got := emb.calls[0]; len(got) != 1 || !strings.Contains(got[0], "الثالثة") {
		t.Fatalf("expected only the appended text to be embedded, got %v", got)
	}
}

func TestIngestBatchesByConfiguredSize(t *testing.T) {
	db := testDB(t)
	seedSource(t, db, usableSource())

	var sections []Section
	for i := 1; i <= 5; i++ {
		sections = append(sections, singleAyah(i-1, i, "نص "+string(rune('أ'+i-1))))
	}
	d := doc(sections...)

	emb := &fakeEmbedder{}
	rep, err := newTestService(t, db, emb).IngestSource(context.Background(), "tabari-yusuf",
		[]Document{d}, IngestOptions{BatchSize: 2})
	if err != nil {
		t.Fatalf("IngestSource: %v", err)
	}
	if rep.EmbedCalls != 3 {
		t.Fatalf("expected 3 batches for 5 chunks at size 2, got %d", rep.EmbedCalls)
	}
	for i, call := range emb.calls {
		if len(call) > 2 {
			t.Fatalf("batch %d carried %d texts, over the configured size", i, len(call))
		}
	}
}

// A failed provider call must leave the index untouched — a half-written batch
// would be indistinguishable from a complete one.
func TestIngestWritesNothingWhenEmbeddingFails(t *testing.T) {
	db := testDB(t)
	seedSource(t, db, usableSource())
	emb := &fakeEmbedder{failAt: 1, failErr: &embedding.BatchError{Start: 0, End: 2, Attempts: 3, Err: embedding.ErrUnavailable}}

	rep, err := newTestService(t, db, emb).IngestSource(context.Background(), "tabari-yusuf",
		[]Document{ingestDoc("نص أول", "نص ثانٍ")}, IngestOptions{BatchSize: 2})
	if err == nil {
		t.Fatal("expected the failure to surface")
	}
	if !errors.Is(err, embedding.ErrUnavailable) {
		t.Fatalf("expected the provider error to be preserved, got %v", err)
	}
	if rep.Written != 0 {
		t.Fatalf("report claims %d writes despite the failure", rep.Written)
	}
	if got := storedChunks(t, db); len(got) != 0 {
		t.Fatalf("expected an untouched index, got %d chunks", len(got))
	}

	src, _ := NewSourceRegistry(db).Get(context.Background(), "tabari-yusuf")
	if src.IngestionStatus != StatusFailed {
		t.Fatalf("ingestion_status = %q, want %q so an interrupted run stays visible", src.IngestionStatus, StatusFailed)
	}
}

func TestIngestRefusesMisalignedVectors(t *testing.T) {
	db := testDB(t)
	seedSource(t, db, usableSource())

	_, err := newTestService(t, db, shortEmbedder{}).IngestSource(context.Background(), "tabari-yusuf",
		[]Document{ingestDoc("نص أول", "نص ثانٍ")}, IngestOptions{})
	if !errors.Is(err, embedding.ErrUnexpectedShape) {
		t.Fatalf("expected ErrUnexpectedShape, got %v", err)
	}
	if got := storedChunks(t, db); len(got) != 0 {
		t.Fatalf("expected nothing indexed, got %d", len(got))
	}
}

func TestIngestDryRunMakesNoCallsAndNoWrites(t *testing.T) {
	db := testDB(t)
	seedSource(t, db, usableSource())
	emb := &fakeEmbedder{}

	rep, err := newTestService(t, db, emb).IngestSource(context.Background(), "tabari-yusuf",
		[]Document{ingestDoc("نص")}, IngestOptions{DryRun: true, PruneStale: true})
	if err != nil {
		t.Fatalf("IngestSource: %v", err)
	}
	if rep.TotalChunks != 1 {
		t.Fatalf("a dry run should still report what it would do, got %+v", rep)
	}
	if len(emb.calls) != 0 {
		t.Fatal("a dry run must not call the provider")
	}
	if got := storedChunks(t, db); len(got) != 0 {
		t.Fatalf("a dry run must not write, got %d chunks", len(got))
	}
}

func TestIngestReportsPlanReuseOnADryRun(t *testing.T) {
	db := testDB(t)
	seedSource(t, db, usableSource())
	docs := []Document{ingestDoc("نص")}

	if _, err := newTestService(t, db, &fakeEmbedder{}).IngestSource(context.Background(), "tabari-yusuf", docs, IngestOptions{}); err != nil {
		t.Fatalf("first run: %v", err)
	}

	emb := &fakeEmbedder{}
	rep, err := newTestService(t, db, emb).IngestSource(context.Background(), "tabari-yusuf", docs, IngestOptions{DryRun: true})
	if err != nil {
		t.Fatalf("dry run: %v", err)
	}
	if rep.Reused != 1 || rep.Embedded != 0 {
		t.Fatalf("dry run should show the full reuse, got %+v", rep)
	}
	if len(emb.calls) != 0 {
		t.Fatal("a dry run must not call the provider")
	}
}

// Two editions of one work must not be silently merged (Part 1 §6).
func TestIngestRefusesAMixedEditionManifest(t *testing.T) {
	db := testDB(t)
	seedSource(t, db, usableSource())

	second := ingestDoc("نص")
	second.ID = "doc-2"
	second.SourceVersion = "dar-1400"

	_, err := newTestService(t, db, &fakeEmbedder{}).IngestSource(context.Background(), "tabari-yusuf",
		[]Document{ingestDoc("نص"), second}, IngestOptions{})
	if err == nil || !strings.Contains(err.Error(), "one edition at a time") {
		t.Fatalf("expected a mixed-edition rejection, got %v", err)
	}
}

// Document metadata may be omitted; the source registry fills it in.
func TestIngestFillsDocumentMetadataFromTheSource(t *testing.T) {
	db := testDB(t)
	src := usableSource()
	seedSource(t, db, src)

	d := Document{ID: "doc-x", SurahID: 12, Sections: []Section{singleAyah(0, 1, "نص")}}
	if _, err := newTestService(t, db, &fakeEmbedder{}).IngestSource(context.Background(), "tabari-yusuf",
		[]Document{d}, IngestOptions{}); err != nil {
		t.Fatalf("IngestSource: %v", err)
	}

	got := storedChunks(t, db)[0]
	if got.SourceVersion != src.Edition {
		t.Fatalf("source_version = %q, want the source edition %q", got.SourceVersion, src.Edition)
	}
	if got.Language != src.Language || got.ContentType != ContentTypeTafsir {
		t.Fatalf("metadata not filled from the source: %q / %q", got.Language, got.ContentType)
	}
}

func TestRevalidateAppliesRegistryChanges(t *testing.T) {
	db := testDB(t)
	seedSource(t, db, usableSource())

	if _, err := newTestService(t, db, &fakeEmbedder{}).IngestSource(context.Background(), "tabari-yusuf",
		[]Document{ingestDoc("نص")}, IngestOptions{}); err != nil {
		t.Fatalf("ingest: %v", err)
	}

	_, err := db.Collection("sources").UpdateOne(context.Background(), bson.M{"_id": "tabari-yusuf"},
		bson.M{"$set": bson.M{"licensing_status": "revoked", "authority_tier": 3}})
	if err != nil {
		t.Fatalf("revoke: %v", err)
	}

	updated, err := newTestService(t, db, &fakeEmbedder{}).Revalidate(context.Background(), "tabari-yusuf")
	if err != nil {
		t.Fatalf("Revalidate: %v", err)
	}
	if updated != 1 {
		t.Fatalf("expected 1 updated chunk, got %d", updated)
	}

	got := storedChunks(t, db)[0]
	if got.SourceVerified {
		t.Fatal("a revoked source must not remain verified on its chunks")
	}
	if got.AuthorityTier != 3 {
		t.Fatalf("authority_tier = %d, want 3", got.AuthorityTier)
	}
}

type shortEmbedder struct{}

func (shortEmbedder) Embed(_ context.Context, texts []string) ([][]float64, error) {
	return repeatVectors(len(texts) - 1), nil
}

// The write model is a superset of the retrieval read model. If ingestion ever
// writes a field retrieval filters on with a different name or type, evidence
// lookup silently returns nothing — so the two are asserted against each other
// here rather than assumed.
func TestIngestedChunksAreRetrievableByExactVerse(t *testing.T) {
	db := testDB(t)
	seedSource(t, db, usableSource())

	d := doc(
		singleAyah(0, 1, "تفسير الآية الأولى"),
		singleAyah(1, 2, "تفسير الآية الثانية"),
		singleAyah(2, 3, "حديث عن معنى الصبر"),
	)
	// The third section is thematic: it must stay out of exact-verse results.
	d.Sections[2].MappingType = MappingThematic
	d.Sections[2].AyahStart, d.Sections[2].AyahEnd = 0, 0

	if _, err := newTestService(t, db, &fakeEmbedder{}).IngestSource(context.Background(), "tabari-yusuf",
		[]Document{d}, IngestOptions{}); err != nil {
		t.Fatalf("IngestSource: %v", err)
	}

	retrievalSvc := retrieval.NewService(db)

	evidence, err := retrievalSvc.GetEvidence(context.Background(), 12, 2, nil)
	if err != nil {
		t.Fatalf("GetEvidence: %v", err)
	}
	if len(evidence) != 1 {
		t.Fatalf("expected exactly the chunk mapped to 12:2, got %d", len(evidence))
	}
	if evidence[0].Text != "تفسير الآية الثانية" {
		t.Fatalf("retrieval returned the wrong chunk: %q", evidence[0].Text)
	}
	if evidence[0].SurahID != 12 || evidence[0].AyahStart != 2 || evidence[0].MappingType != MappingSingleAyah {
		t.Fatalf("retrieval read back mismatched metadata: %+v", evidence[0])
	}
	if len(evidence[0].Embedding) != embedding.ExpectedDimensions {
		t.Fatalf("retrieval read %d embedding dimensions", len(evidence[0].Embedding))
	}
	if !evidence[0].SourceVerified {
		t.Fatal("retrieval did not see the chunk as source-verified")
	}

	// An unmapped verse currently falls through to the thematic chunk, because
	// internal/retrieval/handlers.go passes no query embedding — thematic
	// fallback needs one, which is Day 3 work. Asserted here to pin the boundary:
	// ingestion must not turn a thematic chunk into an exact-verse answer.
	thematic, err := retrievalSvc.GetEvidence(context.Background(), 12, 40, nil)
	if err != nil {
		t.Fatalf("thematic fallback should answer an unmapped verse: %v", err)
	}
	if len(thematic) != 1 || thematic[0].MappingType != MappingThematic {
		t.Fatalf("expected only the thematic chunk, got %+v", thematic)
	}
}

// A source that loses its verified status must disappear from retrieval, even
// though its chunks are still stored.
func TestRevokedSourceDropsOutOfRetrieval(t *testing.T) {
	db := testDB(t)
	seedSource(t, db, usableSource())
	svc := newTestService(t, db, &fakeEmbedder{})

	if _, err := svc.IngestSource(context.Background(), "tabari-yusuf",
		[]Document{doc(singleAyah(0, 1, "تفسير الآية الأولى"))}, IngestOptions{}); err != nil {
		t.Fatalf("IngestSource: %v", err)
	}

	if _, err := NewSourceRegistry(db).Get(context.Background(), "tabari-yusuf"); err != nil {
		t.Fatalf("Get: %v", err)
	}
	if _, err := db.Collection("sources").UpdateOne(context.Background(), bson.M{"_id": "tabari-yusuf"},
		bson.M{"$set": bson.M{"licensing_status": "revoked"}}); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if _, err := svc.Revalidate(context.Background(), "tabari-yusuf"); err != nil {
		t.Fatalf("Revalidate: %v", err)
	}

	if _, err := retrieval.NewService(db).GetEvidence(context.Background(), 12, 1, nil); !errors.Is(err, retrieval.ErrInsufficientEvidence) {
		t.Fatalf("a revoked source must not serve evidence, got %v", err)
	}
}
