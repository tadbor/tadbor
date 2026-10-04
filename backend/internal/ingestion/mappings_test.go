package ingestion

import (
	"context"
	"errors"
	"strings"
	"testing"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
)

// chunk helper mirroring what the chunker produces for issue #5's corpus.
func mapChunk(id string, mapping string, start, end int) Chunk {
	return Chunk{
		ID:            id,
		SourceID:      "src",
		SourceVersion: "v1",
		ContentType:   ContentTypeTafsir,
		Language:      "ar",
		SurahID:       12,
		AyahStart:     start,
		AyahEnd:       end,
		MappingType:   mapping,
		ContentHash:   "h",
	}
}

func TestMappingForCopiesTheChunksRange(t *testing.T) {
	m, err := MappingFor(mapChunk("c1", MappingSingleAyah, 7, 7))
	if err != nil {
		t.Fatalf("MappingFor: %v", err)
	}
	if m.ID != "c1" || m.ChunkID != "c1" {
		t.Errorf("mapping id/chunk_id = %q/%q, want c1/c1", m.ID, m.ChunkID)
	}
	if m.SurahID != 12 || m.AyahStart != 7 || m.AyahEnd != 7 || m.MappingType != MappingSingleAyah {
		t.Errorf("mapping range = %d:%d-%d %s, want 12:7-7 single_ayah", m.SurahID, m.AyahStart, m.AyahEnd, m.MappingType)
	}
}

// The mapping table must never widen a chunk's authority: a mapping is the thing
// retrieval trusts to say "this chunk speaks about that verse", so a range the
// chunker would have rejected must not be recorded here either.
func TestMappingForRejectsRangesTheChunkerRejects(t *testing.T) {
	cases := []struct {
		name    string
		mapping string
		start   int
		end     int
	}{
		{"single_ayah claiming a range", MappingSingleAyah, 3, 4},
		{"single_ayah at zero", MappingSingleAyah, 0, 0},
		{"multi_ayah reversed", MappingMultiAyah, 9, 4},
		{"surah_level claiming a verse", MappingSurahLevel, 0, 4},
		{"thematic claiming a verse", MappingThematic, 1, 1},
		{"unknown type", "by_vibes", 1, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := MappingFor(mapChunk("c1", tc.mapping, tc.start, tc.end)); err == nil {
				t.Fatalf("MappingFor(%s, %d-%d) accepted a range §8 forbids", tc.mapping, tc.start, tc.end)
			}
		})
	}
}

func TestMappingsForFailsOnTheFirstInvalidChunk(t *testing.T) {
	// Deriving mappings in a loop and ignoring the error would write a partial
	// table, which is worse than none: coverage would look complete.
	_, err := MappingsFor([]Chunk{
		mapChunk("good", MappingSingleAyah, 1, 1),
		mapChunk("bad", MappingSurahLevel, 0, 5),
		mapChunk("also-good", MappingSingleAyah, 2, 2),
	})
	if err == nil {
		t.Fatal("MappingsFor accepted a batch containing an invalid chunk")
	}
}

func TestUpsertMappingsIsIdempotent(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	store := NewStore(db)

	chunks := []Chunk{
		mapChunk("c1", MappingSingleAyah, 1, 1),
		mapChunk("c2", MappingSingleAyah, 2, 2),
	}
	mappings, err := MappingsFor(chunks)
	if err != nil {
		t.Fatalf("MappingsFor: %v", err)
	}

	for i := 0; i < 3; i++ {
		if _, err := store.UpsertMappings(ctx, mappings); err != nil {
			t.Fatalf("UpsertMappings run %d: %v", i, err)
		}
	}

	n, err := db.Collection("chunk_ayah_mappings").CountDocuments(ctx, bson.M{})
	if err != nil {
		t.Fatalf("count mappings: %v", err)
	}
	if n != 2 {
		t.Errorf("after 3 identical upserts there are %d mappings, want 2 — _id must be the chunk id", n)
	}
}

// _id being the chunk id is what makes "a chunk is mapped twice" impossible.
func TestMappingIDIsTheChunkID(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	store := NewStore(db)

	mappings, err := MappingsFor([]Chunk{mapChunk("c1", MappingSingleAyah, 5, 5)})
	if err != nil {
		t.Fatalf("MappingsFor: %v", err)
	}
	if _, err := store.UpsertMappings(ctx, mappings); err != nil {
		t.Fatalf("UpsertMappings: %v", err)
	}

	var got AyahMapping
	if err := db.Collection("chunk_ayah_mappings").FindOne(ctx, bson.M{"_id": "c1"}).Decode(&got); err != nil {
		t.Fatalf("find by _id=chunk id: %v", err)
	}
	if got.ChunkID != "c1" {
		t.Errorf("chunk_id = %q, want c1", got.ChunkID)
	}
}

func TestDeleteStaleMappingsKeepsCurrentOnes(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	store := NewStore(db)

	mappings, err := MappingsFor([]Chunk{
		mapChunk("keep", MappingSingleAyah, 1, 1),
		mapChunk("drop", MappingSingleAyah, 2, 2),
	})
	if err != nil {
		t.Fatalf("MappingsFor: %v", err)
	}
	if _, err := store.UpsertMappings(ctx, mappings); err != nil {
		t.Fatalf("UpsertMappings: %v", err)
	}

	// An empty keep list must clear the source rather than silently no-op: a
	// $nin filter rejects null, so passing nil would leave everything behind.
	deleted, err := store.DeleteStaleMappings(ctx, "src", "v1", []string{})
	if err != nil {
		t.Fatalf("DeleteStaleMappings: %v", err)
	}
	if deleted != 2 {
		t.Errorf("deleted %d, want 2", deleted)
	}

	// A different source version must never be touched.
	if _, err := store.UpsertMappings(ctx, mappings); err != nil {
		t.Fatalf("re-upsert: %v", err)
	}
	if _, err := store.DeleteStaleMappings(ctx, "src", "v2", []string{"keep"}); err != nil {
		t.Fatalf("DeleteStaleMappings other version: %v", err)
	}
	n, err := db.Collection("chunk_ayah_mappings").CountDocuments(ctx, bson.M{"source_version": "v1"})
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 2 {
		t.Errorf("pruning version v2 changed v1: %d rows remain, want 2", n)
	}
}

func TestCoverageForSurahCountsUsableSeparately(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	store := NewStore(db)

	chunks := embedded([]Chunk{
		mapChunk("c1", MappingSingleAyah, 1, 1),
		mapChunk("c2", MappingSingleAyah, 1, 1), // same ayah, second chunk
		mapChunk("c3", MappingSingleAyah, 2, 2),
	}, [][]float64{{0.1}, {0.2}, {0.3}})

	// Only c3's source is approved. This is the distinction issue #5 needs: a
	// verse can have commentary that exists and is still not servable.
	for i := range chunks {
		chunks[i].SourceVerified = i == 2
		chunks[i].ReviewStatus = VerificationVerified
		chunks[i].AuthorityTier = 1
	}
	if _, err := store.UpsertChunks(ctx, chunks); err != nil {
		t.Fatalf("UpsertChunks: %v", err)
	}
	mappings, err := MappingsFor(chunks)
	if err != nil {
		t.Fatalf("MappingsFor: %v", err)
	}
	if _, err := store.UpsertMappings(ctx, mappings); err != nil {
		t.Fatalf("UpsertMappings: %v", err)
	}

	cov, err := store.CoverageForSurah(ctx, 12, 3)
	if err != nil {
		t.Fatalf("CoverageForSurah: %v", err)
	}
	if len(cov) != 3 {
		t.Fatalf("coverage has %d entries, want 3 — every ayah must appear, gaps included", len(cov))
	}

	if got := cov[1]; got.Chunks != 2 || got.Usable != 0 {
		t.Errorf("ayah 1 = %d chunks / %d usable, want 2 / 0", got.Chunks, got.Usable)
	}
	// Two chunks of one section collapse to one passage, so a verse split into
	// many paragraphs is not counted as many independent authorities.
	if got := cov[1]; got.Parents != 2 {
		t.Errorf("ayah 1 parents = %d, want 2 (two distinct whole sections)", got.Parents)
	}
	if got := cov[2]; got.Chunks != 1 || got.Usable != 1 {
		t.Errorf("ayah 2 = %d chunks / %d usable, want 1 / 1", got.Chunks, got.Usable)
	}
	if got := cov[3]; got.Chunks != 0 || got.Usable != 0 {
		t.Errorf("ayah 3 = %d chunks / %d usable, want 0 / 0 — a gap must be visible", got.Chunks, got.Usable)
	}
}

// A mapping pointing outside the surah would be served by retrieval while being
// invisible to a per-ayah coverage report, so it is an error rather than a gap.
func TestCoverageForSurahRejectsMappingsOutsideTheSurah(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	store := NewStore(db)

	stray := mapChunk("stray", MappingSingleAyah, 500, 500)
	mappings, err := MappingsFor([]Chunk{stray})
	if err != nil {
		t.Fatalf("MappingsFor: %v", err)
	}
	if _, err := store.UpsertMappings(ctx, mappings); err != nil {
		t.Fatalf("UpsertMappings: %v", err)
	}

	if _, err := store.CoverageForSurah(ctx, 12, 3); err == nil {
		t.Fatal("CoverageForSurah accepted a mapping for ayah 500 of surah 12")
	}
}

func TestVerifyMappingsConsistentIsQuietWhenBothSidesAgree(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	store := NewStore(db)

	chunks := embedded([]Chunk{
		mapChunk("c1", MappingSingleAyah, 1, 1),
		mapChunk("c2", MappingMultiAyah, 2, 3),
	}, [][]float64{{0.1}, {0.2}})
	if _, err := store.UpsertChunks(ctx, chunks); err != nil {
		t.Fatalf("UpsertChunks: %v", err)
	}
	mappings, err := MappingsFor(chunks)
	if err != nil {
		t.Fatalf("MappingsFor: %v", err)
	}
	if _, err := store.UpsertMappings(ctx, mappings); err != nil {
		t.Fatalf("UpsertMappings: %v", err)
	}

	drift, err := store.VerifyMappingsConsistent(ctx, "src", "v1")
	if err != nil {
		t.Fatalf("VerifyMappingsConsistent: %v", err)
	}
	if !drift.Empty() {
		t.Errorf("drift reported for two sides written from one slice: %+v", drift)
	}
}

// Each of the three drift shapes is a different bug, and all three are possible
// after a hand edit or a partial write.
func TestVerifyMappingsConsistentDetectsDrift(t *testing.T) {
	t.Run("chunk with no mapping", func(t *testing.T) {
		db := testDB(t)
		ctx := context.Background()
		store := NewStore(db)

		chunks := embedded([]Chunk{mapChunk("orphan", MappingSingleAyah, 1, 1)}, [][]float64{{0.1}})
		if _, err := store.UpsertChunks(ctx, chunks); err != nil {
			t.Fatalf("UpsertChunks: %v", err)
		}

		drift, err := store.VerifyMappingsConsistent(ctx, "src", "v1")
		if err != nil {
			t.Fatalf("VerifyMappingsConsistent: %v", err)
		}
		if len(drift.ChunksWithoutMapping) != 1 || drift.ChunksWithoutMapping[0] != "orphan" {
			t.Errorf("ChunksWithoutMapping = %v, want [orphan]", drift.ChunksWithoutMapping)
		}
	})

	t.Run("mapping whose chunk was pruned", func(t *testing.T) {
		db := testDB(t)
		ctx := context.Background()
		store := NewStore(db)

		mappings, err := MappingsFor([]Chunk{mapChunk("gone", MappingSingleAyah, 1, 1)})
		if err != nil {
			t.Fatalf("MappingsFor: %v", err)
		}
		if _, err := store.UpsertMappings(ctx, mappings); err != nil {
			t.Fatalf("UpsertMappings: %v", err)
		}

		drift, err := store.VerifyMappingsConsistent(ctx, "src", "v1")
		if err != nil {
			t.Fatalf("VerifyMappingsConsistent: %v", err)
		}
		if len(drift.MappingsWithoutChunk) != 1 || drift.MappingsWithoutChunk[0] != "gone" {
			t.Errorf("MappingsWithoutChunk = %v, want [gone]", drift.MappingsWithoutChunk)
		}
	})

	t.Run("mapping disagrees with its chunk's denormalized range", func(t *testing.T) {
		db := testDB(t)
		ctx := context.Background()
		store := NewStore(db)

		chunks := embedded([]Chunk{mapChunk("c1", MappingSingleAyah, 4, 4)}, [][]float64{{0.1}})
		if _, err := store.UpsertChunks(ctx, chunks); err != nil {
			t.Fatalf("UpsertChunks: %v", err)
		}
		if _, err := store.UpsertMappings(ctx, mustMappings(t, chunks...)); err != nil {
			t.Fatalf("UpsertMappings: %v", err)
		}
		// Simulate a hand edit that widened a mapping so a chunk claims a verse
		// its text never discussed — retrieval would then serve it as exact
		// evidence for the wrong ayah.
		if _, err := db.Collection("chunk_ayah_mappings").UpdateOne(ctx,
			bson.M{"_id": "c1"}, bson.M{"$set": bson.M{"ayah_start": 40, "ayah_end": 44}}); err != nil {
			t.Fatalf("widen mapping: %v", err)
		}

		drift, err := store.VerifyMappingsConsistent(ctx, "src", "v1")
		if err != nil {
			t.Fatalf("VerifyMappingsConsistent: %v", err)
		}
		if len(drift.Mismatched) != 1 || drift.Mismatched[0] != "c1" {
			t.Errorf("Mismatched = %v, want [c1]", drift.Mismatched)
		}
	})
}

func TestVerifyMappingsConsistentIgnoresOtherSources(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	store := NewStore(db)

	a := mapChunk("a1", MappingSingleAyah, 1, 1)
	a.SourceID = "other"
	a.SourceVersion = "v9"
	if _, err := store.UpsertMappings(ctx, mustMappings(t, a)); err != nil {
		t.Fatalf("UpsertMappings: %v", err)
	}

	// The stored source has nothing, so it is internally consistent: another
	// source's unmapped chunk is not this source's problem.
	drift, err := store.VerifyMappingsConsistent(ctx, "src", "v1")
	if err != nil {
		t.Fatalf("VerifyMappingsConsistent: %v", err)
	}
	if !drift.Empty() {
		t.Errorf("drift reported across sources: %+v", drift)
	}
}

func mustMappings(t *testing.T, chunks ...Chunk) []AyahMapping {
	t.Helper()
	out, err := MappingsFor(chunks)
	if err != nil {
		t.Fatalf("MappingsFor: %v", err)
	}
	return out
}

// PreviewUnverified is the one path that lets an unapproved source past the gate,
// so its limits have to be enforced rather than assumed.
func TestPreviewUnverifiedIsRefusedOnALiveRun(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	src := Source{
		ID: "src", Title: "t", Language: "ar", AuthorityTier: 1, Edition: "v1",
		LicensingStatus: LicensingCleared, VerificationStatus: "unverified",
		IngestionStatus: StatusNotStarted,
	}
	seedSource(t, db, src)

	// A caller asking to write unverified content must be refused even though it
	// set the preview flag: the flag is not a licence to bypass §6.
	_, err := NewService(db, &fakeEmbedder{}).IngestSource(ctx, "src",
		[]Document{{
			ID: "src-surah-12", Language: "ar", ContentType: ContentTypeTafsir, SurahID: 12,
			Sections: []Section{{Ordinal: 0, SurahID: 12, AyahStart: 1, AyahEnd: 1, MappingType: MappingSingleAyah, Text: "نص"}},
		}},
		IngestOptions{DryRun: false, PreviewUnverified: true})

	if !errors.Is(err, ErrSourceNotUsable) {
		t.Fatalf("a live run with PreviewUnverified returned %v, want ErrSourceNotUsable", err)
	}
	if got := storedChunks(t, db); len(got) != 0 {
		t.Fatalf("a refused live run wrote %d chunks", len(got))
	}
}

// A preview is what makes issue #5 reviewable: a manifest's mapping has to be
// checkable before anyone approves the text. It must still cost no provider calls
// and write nothing.
func TestPreviewUnverifiedRunsThePipelineWithoutWriting(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	src := Source{
		ID: "src", Title: "t", Language: "ar", AuthorityTier: 1, Edition: "v1",
		LicensingStatus: LicensingCleared, VerificationStatus: "unverified",
		IngestionStatus: StatusNotStarted,
	}
	seedSource(t, db, src)

	emb := &fakeEmbedder{}
	rep, err := NewService(db, emb).IngestSource(ctx, "src",
		[]Document{{
			ID: "src-surah-12", Language: "ar", ContentType: ContentTypeTafsir, SurahID: 12,
			Sections: []Section{{Ordinal: 0, SurahID: 12, AyahStart: 1, AyahEnd: 1, MappingType: MappingSingleAyah, Text: "نص"}},
		}},
		IngestOptions{DryRun: true, PreviewUnverified: true})

	if err != nil {
		t.Fatalf("preview of an unverified source: %v", err)
	}
	if rep.TotalChunks != 1 {
		t.Errorf("TotalChunks = %d, want 1 — the preview must still chunk", rep.TotalChunks)
	}
	if len(emb.calls) != 0 {
		t.Errorf("preview made %d provider call(s), want 0", len(emb.calls))
	}
	if got := storedChunks(t, db); len(got) != 0 {
		t.Errorf("preview wrote %d chunks, want 0", len(got))
	}
	if n := mappingCount(t, db); n != 0 {
		t.Errorf("preview wrote %d mappings, want 0", n)
	}
}

// The mapping table is part of a real ingest's output, not an optional extra.
func TestIngestWritesOneMappingPerChunk(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	seedSource(t, db, usableSource())

	rep, err := NewService(db, &fakeEmbedder{}).IngestSource(ctx, "tabari-yusuf",
		[]Document{{
			ID: "tabari-yusuf-12", Language: "ar", ContentType: ContentTypeTafsir, SurahID: 12,
			Sections: []Section{
				{Ordinal: 0, SurahID: 12, AyahStart: 1, AyahEnd: 1, MappingType: MappingSingleAyah, Text: "الأول"},
				{Ordinal: 1, SurahID: 12, AyahStart: 2, AyahEnd: 2, MappingType: MappingSingleAyah, Text: strings.Repeat("ط", DefaultMaxChunkRunes+50)},
			},
		}}, IngestOptions{})
	if err != nil {
		t.Fatalf("IngestSource: %v", err)
	}

	chunks := storedChunks(t, db)
	if rep.Mapped != len(chunks) {
		t.Errorf("Mapped = %d, want %d — every chunk needs a mapping", rep.Mapped, len(chunks))
	}
	if got := mappingCount(t, db); got != int64(len(chunks)) {
		t.Errorf("stored %d mappings for %d chunks", got, len(chunks))
	}

	// A section longer than the chunk limit splits into several chunks, and all of
	// them must point at the same ayah.
	drift, err := NewStore(db).VerifyMappingsConsistent(ctx, "tabari-yusuf", "dar-1410")
	if err != nil {
		t.Fatalf("VerifyMappingsConsistent: %v", err)
	}
	if !drift.Empty() {
		t.Errorf("drift after a normal ingest: %+v", drift)
	}
}

// Pruning a chunk must prune its mapping, or coverage would claim commentary that
// retrieval can no longer serve.
func TestIngestPruneRemovesMappingsToo(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	seedSource(t, db, usableSource())
	svc := NewService(db, &fakeEmbedder{})

	twoAyahs := func() []Document {
		return []Document{{
			ID: "tabari-yusuf-12", Language: "ar", ContentType: ContentTypeTafsir, SurahID: 12,
			Sections: []Section{
				{Ordinal: 0, SurahID: 12, AyahStart: 1, AyahEnd: 1, MappingType: MappingSingleAyah, Text: "الأول"},
				{Ordinal: 1, SurahID: 12, AyahStart: 2, AyahEnd: 2, MappingType: MappingSingleAyah, Text: "الثاني"},
			},
		}}
	}
	if _, err := svc.IngestSource(ctx, "tabari-yusuf", twoAyahs(), IngestOptions{PruneStale: true}); err != nil {
		t.Fatalf("first ingest: %v", err)
	}
	firstMappings := mappingCount(t, db)
	if firstMappings != 2 {
		t.Fatalf("after first ingest there are %d mappings, want 2", firstMappings)
	}

	// Re-ingest with ayah 2 gone.
	if _, err := svc.IngestSource(ctx, "tabari-yusuf", []Document{{
		ID: "tabari-yusuf-12", Language: "ar", ContentType: ContentTypeTafsir, SurahID: 12,
		Sections: []Section{
			{Ordinal: 0, SurahID: 12, AyahStart: 1, AyahEnd: 1, MappingType: MappingSingleAyah, Text: "الأول"},
		},
	}}, IngestOptions{PruneStale: true}); err != nil {
		t.Fatalf("second ingest: %v", err)
	}

	if got := mappingCount(t, db); got != 1 {
		t.Errorf("after pruning, %d mappings remain, want 1", got)
	}
	drift, err := NewStore(db).VerifyMappingsConsistent(ctx, "tabari-yusuf", "dar-1410")
	if err != nil {
		t.Fatalf("VerifyMappingsConsistent: %v", err)
	}
	if !drift.Empty() {
		t.Errorf("prune left the two sides inconsistent: %+v", drift)
	}
}

func mappingCount(t *testing.T, db *mongo.Database) int64 {
	t.Helper()
	n, err := db.Collection("chunk_ayah_mappings").CountDocuments(context.Background(), bson.M{})
	if err != nil {
		t.Fatalf("count mappings: %v", err)
	}
	return n
}
