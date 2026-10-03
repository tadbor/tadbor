package ingestion

import (
	"strings"
	"testing"
)

func usableSource() Source {
	return Source{
		ID:                 "tabari-yusuf",
		Title:              "Tafsir al-Tabari",
		Author:             "Ibn Jarir al-Tabari",
		Language:           "ar",
		AuthorityTier:      1,
		LicensingStatus:    LicensingCleared,
		VerificationStatus: VerificationVerified,
		Edition:            "dar-1410",
	}
}

func doc(sections ...Section) Document {
	return Document{
		ID:            "doc-1",
		SourceVersion: "dar-1410",
		Language:      "ar",
		ContentType:   ContentTypeTafsir,
		SurahID:       12,
		Sections:      sections,
	}
}

func singleAyah(ordinal, ayah int, text string) Section {
	return Section{
		Ordinal:     ordinal,
		SurahID:     12,
		AyahStart:   ayah,
		AyahEnd:     ayah,
		MappingType: MappingSingleAyah,
		Text:        text,
	}
}

func buildChunks(t *testing.T, d Document, src Source) []Chunk {
	t.Helper()
	chunks, err := NewChunker(0).Build(d, src)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	return chunks
}

func TestChunkerKeepsVerseAnchoredBoundaries(t *testing.T) {
	// The point of §9: commentary on distinct verses must never be merged, and a
	// single verse's commentary must not be split across chunks.
	d := doc(
		singleAyah(0, 1, "أول فقرة في الآية الأولى"),
		singleAyah(1, 2, "أول فقرة في الآية الثانية"),
	)
	chunks := buildChunks(t, d, usableSource())

	if len(chunks) != 2 {
		t.Fatalf("expected one chunk per verse section, got %d", len(chunks))
	}
	if chunks[0].AyahStart != 1 || chunks[0].AyahEnd != 1 {
		t.Fatalf("chunk 0 mapped to %d-%d, want 1-1", chunks[0].AyahStart, chunks[0].AyahEnd)
	}
	if chunks[1].AyahStart != 2 || chunks[1].AyahEnd != 2 {
		t.Fatalf("chunk 1 mapped to %d-%d, want 2-2", chunks[1].AyahStart, chunks[1].AyahEnd)
	}
	for i, c := range chunks {
		if c.ParentChunkID != "" {
			t.Fatalf("chunk %d has parent %q; a one-paragraph section needs none", i, c.ParentChunkID)
		}
	}
}

func TestChunkerPreservesMultiAyahRanges(t *testing.T) {
	d := doc(Section{
		Ordinal:     0,
		SurahID:     12,
		AyahStart:   4,
		AyahEnd:     6,
		MappingType: MappingMultiAyah,
		Text:        "كلام واحد عن الآيات ٤ إلى ٦",
	})
	chunks := buildChunks(t, d, usableSource())

	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(chunks))
	}
	if chunks[0].AyahStart != 4 || chunks[0].AyahEnd != 6 {
		t.Fatalf("range collapsed to %d-%d, want 4-6", chunks[0].AyahStart, chunks[0].AyahEnd)
	}
}

// surah_level and thematic chunks must carry no verse range, because retrieval's
// exact-mapping filter (ayah_start <= ayah && ayah_end >= ayah) would otherwise
// let them satisfy a verse-specific lookup — the opposite of the precedence
// order in Part 1 §8.
func TestChunkerLeavesUnanchoredTypesWithoutARange(t *testing.T) {
	for _, mapping := range []string{MappingSurahLevel, MappingThematic} {
		t.Run(mapping, func(t *testing.T) {
			d := doc(Section{
				Ordinal:     0,
				SurahID:     12,
				MappingType: mapping,
				Text:        "مقدمة أو كلام موضوعي",
			})
			chunks := buildChunks(t, d, usableSource())

			if len(chunks) != 1 {
				t.Fatalf("expected 1 chunk, got %d", len(chunks))
			}
			if chunks[0].AyahStart != 0 || chunks[0].AyahEnd != 0 {
				t.Fatalf("%s chunk claimed range %d-%d, want 0-0", mapping, chunks[0].AyahStart, chunks[0].AyahEnd)
			}
		})
	}
}

func TestChunkerSplitsOversizedSectionsAndLinksParent(t *testing.T) {
	long := strings.Repeat("كلمة ", 900) // well past the default cap
	d := doc(singleAyah(0, 3, long))
	chunks := buildChunks(t, d, usableSource())

	if len(chunks) < 2 {
		t.Fatalf("expected the section to be split, got %d chunk(s)", len(chunks))
	}

	parent := chunks[0].ParentChunkID
	if parent == "" {
		t.Fatal("expected a parent chunk id on a split section")
	}
	for i, c := range chunks {
		if c.ParentChunkID != parent {
			t.Fatalf("chunk %d has parent %q, want the shared %q", i, c.ParentChunkID, parent)
		}
		if utf8RuneCount(c.Text) > DefaultMaxChunkRunes {
			t.Fatalf("chunk %d is %d runes, over the cap", i, utf8RuneCount(c.Text))
		}
		// Every piece must still map to the same verse.
		if c.AyahStart != 3 || c.AyahEnd != 3 {
			t.Fatalf("chunk %d lost its verse anchor: %d-%d", i, c.AyahStart, c.AyahEnd)
		}
	}
	// The parent id must not itself be a stored chunk.
	for _, c := range chunks {
		if c.ID == parent {
			t.Fatal("a chunk must not be its own parent")
		}
	}
}

func TestChunkerSplitsOnParagraphsNotSize(t *testing.T) {
	d := doc(singleAyah(0, 1, "الفقرة الأولى\n\nالفقرة الثانية\n\nالفقرة الثالثة"))
	chunks := buildChunks(t, d, usableSource())

	if len(chunks) != 3 {
		t.Fatalf("expected one chunk per paragraph, got %d", len(chunks))
	}
	for i, want := range []string{"الفقرة الأولى", "الفقرة الثانية", "الفقرة الثالثة"} {
		if chunks[i].Text != want {
			t.Fatalf("chunk %d text = %q, want %q", i, chunks[i].Text, want)
		}
	}
}

func TestChunkerProducesDeterministicIDs(t *testing.T) {
	d := doc(
		singleAyah(0, 1, "نص أول"),
		singleAyah(1, 2, "نص ثانٍ"),
	)
	src := usableSource()

	first := buildChunks(t, d, src)
	second := buildChunks(t, d, src)

	if len(first) != len(second) {
		t.Fatalf("chunk counts differ between runs: %d vs %d", len(first), len(second))
	}
	for i := range first {
		if first[i].ID != second[i].ID {
			t.Fatalf("chunk %d id is not stable: %s vs %s", i, first[i].ID, second[i].ID)
		}
	}
	if first[0].ID == first[1].ID {
		t.Fatal("distinct chunks must not share an id")
	}
}

func TestChunkIDVariesWithEveryInput(t *testing.T) {
	key := ChunkKey{AyahStart: 1, AyahEnd: 1, MappingType: MappingSingleAyah, ContentHash: "hash"}
	base := ChunkID("src", "v1", key, 0)
	for _, tc := range []struct {
		name string
		got  string
	}{
		{"source", ChunkID("other", "v1", key, 0)},
		{"source version", ChunkID("src", "v2", key, 0)},
		{"ayah range", ChunkID("src", "v1", ChunkKey{AyahStart: 2, AyahEnd: 2, MappingType: MappingSingleAyah, ContentHash: "hash"}, 0)},
		{"mapping type", ChunkID("src", "v1", ChunkKey{AyahStart: 1, AyahEnd: 1, MappingType: MappingThematic, ContentHash: "hash"}, 0)},
		{"content", ChunkID("src", "v1", ChunkKey{AyahStart: 1, AyahEnd: 1, MappingType: MappingSingleAyah, ContentHash: "other"}, 0)},
		{"duplicate index", ChunkID("src", "v1", key, 1)},
	} {
		if tc.got == base {
			t.Fatalf("changing the %s must change the chunk id", tc.name)
		}
	}
}

// The same content anchored to two different verses is two distinct chunks.
func TestChunkIDSeparatesIdenticalTextAtDifferentVerses(t *testing.T) {
	at1 := ChunkID("src", "v1", ChunkKey{AyahStart: 1, AyahEnd: 1, MappingType: MappingSingleAyah, ContentHash: "same"}, 0)
	at2 := ChunkID("src", "v1", ChunkKey{AyahStart: 2, AyahEnd: 2, MappingType: MappingSingleAyah, ContentHash: "same"}, 0)
	if at1 == at2 {
		t.Fatal("identical text at different verses must not collide")
	}
}

// Repeated verbatim text within one document must produce distinct, stable ids
// instead of silently overwriting itself.
func TestChunkerDisambiguatesVerbatimRepeats(t *testing.T) {
	d := doc(
		singleAyah(0, 1, "بسم الله"),
		singleAyah(1, 2, "بسم الله"),
		singleAyah(2, 3, "نص آخر"),
	)
	src := usableSource()

	first := buildChunks(t, d, src)
	second := buildChunks(t, d, src)

	if first[0].ID == first[1].ID {
		t.Fatal("verbatim repeats at different verses collided")
	}
	if first[0].ID != second[0].ID || first[1].ID != second[1].ID {
		t.Fatal("ids for verbatim repeats are not stable across runs")
	}

	// Repeats within one verse section also need distinct ids.
	same := doc(
		Section{Ordinal: 0, SurahID: 12, AyahStart: 5, AyahEnd: 5, MappingType: MappingSingleAyah, Text: "مكرر\n\nمكرر"},
	)
	repeats := buildChunks(t, same, src)
	if len(repeats) != 2 {
		t.Fatalf("expected 2 chunks, got %d", len(repeats))
	}
	if repeats[0].ID == repeats[1].ID {
		t.Fatal("verbatim repeats within one section collided")
	}
}

// Structural edits must not invalidate stored vectors for untouched passages:
// that is the difference between re-embedding one chunk and re-embedding a corpus.
func TestChunkIDsSurviveAnEarlierStructuralEdit(t *testing.T) {
	src := usableSource()
	before := buildChunks(t, doc(
		singleAyah(0, 1, "الآية الأولى"),
		singleAyah(1, 2, "الآية الثانية"),
		singleAyah(2, 3, "الآية الثالثة"),
	), src)

	after := buildChunks(t, doc(
		singleAyah(0, 1, "الآية الأولى"),
		singleAyah(1, 2, "الآية الثانية"),
		singleAyah(2, 3, "الآية الثالثة"),
		singleAyah(3, 4, "الآية الرابعة"),
	), src)

	if len(after) != len(before)+1 {
		t.Fatalf("expected 4 chunks, got %d", len(after))
	}
	for i := range before {
		if before[i].ID != after[i].ID {
			t.Fatalf("appending a section changed chunk %d's id; it would be needlessly re-embedded", i)
		}
	}
}

func TestChunkerDerivesNormalizedTextAndProvenance(t *testing.T) {
	src := usableSource()
	chunks := buildChunks(t, doc(singleAyah(0, 1, "الْحَمْدُ لِلَّهِ")), src)

	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(chunks))
	}
	c := chunks[0]

	if c.Text != "الْحَمْدُ لِلَّهِ" {
		t.Fatalf("original text was altered: %q", c.Text)
	}
	if c.TextNormalized != "الحمد لله" {
		t.Fatalf("lexical form = %q, want %q", c.TextNormalized, "الحمد لله")
	}
	if c.ContentHash != ContentHash(c.Text) {
		t.Fatal("content hash does not match the stored text")
	}
	if c.SourceVerified != true {
		t.Fatal("expected source_verified for a verified, cleared source")
	}
	if c.ReviewStatus != VerificationVerified {
		t.Fatalf("review_status = %q", c.ReviewStatus)
	}
	if c.AuthorityTier != 1 {
		t.Fatalf("authority_tier = %d, want 1", c.AuthorityTier)
	}
	if c.DocumentID != "doc-1" || c.SourceID != src.ID || c.SourceVersion != "dar-1410" {
		t.Fatalf("provenance not carried: %+v", c)
	}
}

func TestChunkerMarksUnusableSourceUnverified(t *testing.T) {
	src := usableSource()
	src.LicensingStatus = "pending"
	chunks := buildChunks(t, doc(singleAyah(0, 1, "نص")), src)

	if chunks[0].SourceVerified {
		t.Fatal("a source pending rights clearance must not be marked verified")
	}
}

func TestChunkerRejectsInvalidInput(t *testing.T) {
	src := usableSource()
	for _, tc := range []struct {
		name string
		doc  Document
	}{
		{"no sections", Document{ID: "d", SourceVersion: "v1", Language: "ar", ContentType: ContentTypeTafsir, SurahID: 12}},
		{"missing document id", func() Document {
			d := doc(singleAyah(0, 1, "نص"))
			d.ID = ""
			return d
		}()},
		{"missing language", func() Document {
			d := doc(singleAyah(0, 1, "نص"))
			d.Language = ""
			return d
		}()},
		{"missing source version", func() Document {
			d := doc(singleAyah(0, 1, "نص"))
			d.SourceVersion = ""
			return d
		}()},
		{"missing surah", func() Document {
			d := doc(singleAyah(0, 1, "نص"))
			d.SurahID = 0
			return d
		}()},
		{"unknown content type", func() Document {
			d := doc(singleAyah(0, 1, "نص"))
			d.ContentType = "blog_post"
			return d
		}()},
		{"single_ayah with a range", doc(Section{Ordinal: 0, SurahID: 12, AyahStart: 1, AyahEnd: 2, MappingType: MappingSingleAyah, Text: "نص"})},
		{"multi_ayah inverted", doc(Section{Ordinal: 0, SurahID: 12, AyahStart: 6, AyahEnd: 4, MappingType: MappingMultiAyah, Text: "نص"})},
		{"thematic claiming a verse", doc(Section{Ordinal: 0, SurahID: 12, AyahStart: 3, AyahEnd: 3, MappingType: MappingThematic, Text: "نص"})},
		{"unknown mapping type", doc(Section{Ordinal: 0, SurahID: 12, AyahStart: 1, AyahEnd: 1, MappingType: "vibes", Text: "نص"})},
		{"whitespace only section", doc(singleAyah(0, 1, "   \n  "))},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := NewChunker(0).Build(tc.doc, src); err == nil {
				t.Fatal("expected an error")
			}
		})
	}
}

func TestChunkerIgnoresWhitespaceOnlySectionsButKeepsRealOnes(t *testing.T) {
	d := doc(
		Section{Ordinal: 0, SurahID: 12, MappingType: MappingSurahLevel, Text: ""},
		singleAyah(1, 1, "نص حقيقي"),
	)
	chunks := buildChunks(t, d, usableSource())
	if len(chunks) != 1 {
		t.Fatalf("expected the empty section to be skipped, got %d chunk(s)", len(chunks))
	}
}

func TestChunkerFallsBackToDocumentSurah(t *testing.T) {
	d := doc(Section{Ordinal: 0, MappingType: MappingThematic, Text: "كلام موضوعي"})
	chunks := buildChunks(t, d, usableSource())
	if chunks[0].SurahID != 12 {
		t.Fatalf("surah_id = %d, want the document's 12", chunks[0].SurahID)
	}
}
