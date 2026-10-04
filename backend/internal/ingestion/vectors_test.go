package ingestion

import (
	"context"
	"math"
	"testing"

	"tadbor/backend/internal/embedding"
)

func TestStoredVectorAuditAcceptsWhatThePipelineWrites(t *testing.T) {
	v := unitVector()
	doc := storedVector{
		ID:         "c1",
		SourceID:   "tabari-yusuf",
		Text:       "نص آية",
		Embedding:  v,
		Model:      embedding.ModelID,
		Dimensions: len(v),
		Normalized: true,
		Hash:       EmbeddingHash(embedding.ModelID, [][]float64{v}),
	}

	a := doc.audit()
	if a.NeedsEmbedding() {
		t.Fatalf("a freshly embedded chunk must not need embedding: %v", a.Defects)
	}
	if !a.HasVector || !a.HashMatches {
		t.Fatalf("expected a hash-verified vector, got %+v", a)
	}
	if a.Text != "نص آية" || a.SourceID != "tabari-yusuf" {
		t.Fatalf("audit lost the fields the job needs: %+v", a)
	}
}

// A chunk that was never embedded decodes from a BSON null into a nil slice,
// because Chunk.Embedding has no omitempty. That is the ordinary state this job
// exists to fix, and it must be reported as needing a vector — not as an error and
// not as already-satisfied.
func TestStoredVectorAuditFlagsMissingVector(t *testing.T) {
	a := storedVector{ID: "c1", SourceID: "s", Text: "نص"}.audit()

	if a.HasVector {
		t.Fatal("expected HasVector false")
	}
	if a.HashMatches {
		t.Fatal("a chunk with no vector cannot have a matching hash")
	}
	if !a.NeedsEmbedding() {
		t.Fatal("a chunk with no vector must need embedding")
	}
	if len(a.Defects) != 1 || a.Defects[0] != "no vector stored" {
		t.Fatalf("expected one precise defect, got %v", a.Defects)
	}
}

func TestStoredVectorAuditFlagsForeignAndCorruptVectors(t *testing.T) {
	v := unitVector()
	valid := func() storedVector {
		return storedVector{
			Embedding:  v,
			Model:      embedding.ModelID,
			Dimensions: len(v),
			Normalized: true,
			Hash:       EmbeddingHash(embedding.ModelID, [][]float64{v}),
		}
	}

	for _, tc := range []struct {
		name    string
		corrupt func(*storedVector)
		want    string
	}{
		{
			// Two embedding spaces in one collection make cosine meaningless
			// between neighbours, so a vector from any other model is re-done.
			name:    "other model",
			corrupt: func(s *storedVector) { s.Model = "BAAI/bge-small" },
			want:    `vector came from model "BAAI/bge-small", not "` + embedding.ModelID + `"`,
		},
		{
			name: "wrong width",
			corrupt: func(s *storedVector) {
				s.Embedding = v[:len(v)-1]
			},
			want: "vector has 1023 dimensions, want 1024",
		},
		{
			// Retrieval computes cosine in Go, which assumes unit length.
			name:    "not normalized",
			corrupt: func(s *storedVector) { s.Normalized = false },
			want:    "vector is not marked normalized",
		},
		{
			name:    "no hash",
			corrupt: func(s *storedVector) { s.Hash = "" },
			want:    "no embedding_hash recorded",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			doc := valid()
			tc.corrupt(&doc)
			a := doc.audit()
			if !a.NeedsEmbedding() {
				t.Fatalf("expected %s to need embedding", tc.name)
			}
			if !contains(a.Defects, tc.want) {
				t.Fatalf("defects = %v, want one to be %q", a.Defects, tc.want)
			}
		})
	}
}

// The strongest check available without the provider: recompute the fingerprint
// from the vector itself. Every field can look right while the vector beside them is
// truncated or hand-edited, and only this catches it.
func TestStoredVectorAuditDetectsAVectorItsOwnHashDoesNotCover(t *testing.T) {
	v := unitVector()
	edited := append([]float64(nil), v...)
	edited[0] = math.Sqrt(1 - 0) // no longer unit length, and no longer covered

	doc := storedVector{
		Embedding:  edited,
		Model:      embedding.ModelID,
		Dimensions: len(edited),
		Normalized: true,
		Hash:       EmbeddingHash(embedding.ModelID, [][]float64{v}),
	}

	a := doc.audit()
	if a.HashMatches {
		t.Fatal("expected the hash to no longer match the edited vector")
	}
	if !contains(a.Defects, "embedding_hash does not match the stored vector") {
		t.Fatalf("defects = %v", a.Defects)
	}
	if !a.NeedsEmbedding() {
		t.Fatal("an unaccounted-for vector must be re-embedded, not trusted")
	}
}

func contains(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}

// The repair path, end to end against a real MongoDB: chunks stored without vectors
// (the state a chunk is in between ingest and this job), then filled in.
func TestSetEmbeddingsFillsChunksStoredWithoutVectors(t *testing.T) {
	db := testDB(t)
	store := NewStore(db)
	ctx := context.Background()
	if err := store.EnsureIndexes(ctx); err != nil {
		t.Fatalf("EnsureIndexes: %v", err)
	}

	chunks := buildChunks(t, doc(
		singleAyah(0, 1, "نص الآية الأولى"),
		singleAyah(1, 2, "نص الآية الثانية"),
	), usableSource())

	// UpsertChunks persists whatever it is given, so a chunk with no vector lands
	// with `embedding: null` — exactly what cmd/embed_chunks is asked to find.
	if _, err := store.UpsertChunks(ctx, chunks); err != nil {
		t.Fatalf("UpsertChunks: %v", err)
	}

	before, err := store.AuditEmbeddings(ctx, "tabari-yusuf")
	if err != nil {
		t.Fatalf("AuditEmbeddings: %v", err)
	}
	if len(before) != 2 {
		t.Fatalf("expected 2 audited chunks, got %d", len(before))
	}
	for _, a := range before {
		if !a.NeedsEmbedding() {
			t.Fatalf("chunk %s has no vector yet but does not need embedding", a.ID)
		}
	}

	writes := []EmbeddedChunk{
		{ID: chunks[0].ID, Vector: unitVector()},
		{ID: chunks[1].ID, Vector: unitVector()},
	}
	res, err := store.SetEmbeddings(ctx, embedding.ModelID, writes)
	if err != nil {
		t.Fatalf("SetEmbeddings: %v", err)
	}
	if res.MatchedCount != 2 {
		t.Fatalf("matched %d chunks, want 2", res.MatchedCount)
	}

	after, err := store.AuditEmbeddings(ctx, "tabari-yusuf")
	if err != nil {
		t.Fatalf("AuditEmbeddings: %v", err)
	}
	for _, a := range after {
		if a.NeedsEmbedding() {
			t.Fatalf("chunk %s still needs embedding: %v", a.ID, a.Defects)
		}
		if !a.HashMatches || !a.HasVector || !a.Normalized || a.Dimensions != embedding.ExpectedDimensions {
			t.Fatalf("chunk %s: provenance not recorded: %+v", a.ID, a)
		}
	}

	// Nothing else about the chunk may change: this writes vector fields only.
	got := map[string]Chunk{}
	for _, c := range storedChunks(t, db) {
		got[c.ID] = c
	}
	for _, original := range chunks {
		after := got[original.ID]
		if after.Text != original.Text || after.ContentHash != original.ContentHash {
			t.Fatalf("chunk %s text was rewritten: %+v", original.ID, after)
		}
		if after.CreatedAt != "" && after.CreatedAt != after.UpdatedAt && after.EmbeddingDimensions == 0 {
			t.Fatal("created_at must not be touched by an embedding write")
		}
	}
}

func TestSetEmbeddingsIsIdempotentAndRefusesUnknownChunks(t *testing.T) {
	db := testDB(t)
	store := NewStore(db)
	ctx := context.Background()

	chunks := buildChunks(t, doc(singleAyah(0, 1, "نص")), usableSource())
	if _, err := store.UpsertChunks(ctx, chunks); err != nil {
		t.Fatalf("UpsertChunks: %v", err)
	}

	writes := []EmbeddedChunk{{ID: chunks[0].ID, Vector: unitVector()}}
	for run := 1; run <= 3; run++ {
		if _, err := store.SetEmbeddings(ctx, embedding.ModelID, writes); err != nil {
			t.Fatalf("run %d: SetEmbeddings: %v", run, err)
		}
	}
	if got := storedChunks(t, db); len(got) != 1 {
		t.Fatalf("expected the corpus to stay at 1 chunk, got %d", len(got))
	}

	// An id that is not in the index must fail loudly. Silently matching nothing is
	// how a batch job reports success while having embedded nothing.
	if _, err := store.SetEmbeddings(ctx, embedding.ModelID, []EmbeddedChunk{{ID: "not-a-chunk", Vector: unitVector()}}); err == nil {
		t.Fatal("expected an error for a chunk id that is not stored")
	}
}

func TestSetEmbeddingsRefusesAVectorThatCannotBeCompared(t *testing.T) {
	db := testDB(t)
	store := NewStore(db)
	ctx := context.Background()

	chunks := buildChunks(t, doc(singleAyah(0, 1, "نص")), usableSource())
	if _, err := store.UpsertChunks(ctx, chunks); err != nil {
		t.Fatalf("UpsertChunks: %v", err)
	}

	zero := make([]float64, embedding.ExpectedDimensions)
	if _, err := store.SetEmbeddings(ctx, embedding.ModelID, []EmbeddedChunk{{ID: chunks[0].ID, Vector: zero}}); err == nil {
		t.Fatal("expected a zero vector to be refused")
	}
	nan := unitVector()
	nan[3] = math.NaN()
	if _, err := store.SetEmbeddings(ctx, embedding.ModelID, []EmbeddedChunk{{ID: chunks[0].ID, Vector: nan}}); err == nil {
		t.Fatal("expected a NaN vector to be refused")
	}

	audits, err := store.AuditEmbeddings(ctx, "tabari-yusuf")
	if err != nil {
		t.Fatalf("AuditEmbeddings: %v", err)
	}
	if len(audits) != 1 || !audits[0].NeedsEmbedding() {
		t.Fatalf("a refused vector must not have been written: %+v", audits)
	}
}
