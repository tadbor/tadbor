package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"tadbor/backend/internal/ingestion"
	"tadbor/backend/internal/quran"
)

const surah12Ayahs = 111

func writeCorpus(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "corpus.md")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write corpus: %v", err)
	}
	return path
}

// corpusWith builds a complete Surah Yusuf corpus file, the way fetch_tafsir
// writes one: an ayah heading per verse, commentary underneath. overrides replaces
// the body of individual ayahs; an override of "" produces a heading with no text.
func corpusWith(overrides map[int]string) string {
	var b strings.Builder
	b.WriteString("# title\n\n")
	for ayah := 1; ayah <= surah12Ayahs; ayah++ {
		text := fmt.Sprintf("passage %d", ayah)
		if v, ok := overrides[ayah]; ok {
			text = v
		}
		fmt.Fprintf(&b, "## 12:%d\n\n%s\n\n", ayah, text)
	}
	return b.String()
}

func TestParseCorpusReadsEveryAyah(t *testing.T) {
	path := writeCorpus(t, corpusWith(map[int]string{1: "first", 111: "last"}))

	sections, err := parseCorpus(path, 12)
	if err != nil {
		t.Fatalf("parseCorpus: %v", err)
	}
	if len(sections) != surah12Ayahs {
		t.Fatalf("got %d sections, want %d", len(sections), surah12Ayahs)
	}
	if sections[0].AyahStart != 1 || sections[0].AyahEnd != 1 || sections[0].Text != "first" {
		t.Errorf("first section = %d-%d %q, want 1-1 %q",
			sections[0].AyahStart, sections[0].AyahEnd, sections[0].Text, "first")
	}
	if sections[surah12Ayahs-1].Text != "last" {
		t.Errorf("last text = %q, want %q", sections[surah12Ayahs-1].Text, "last")
	}
	// Every passage in this corpus is keyed to exactly one verse, which is the
	// only reason single_ayah is a claim rather than a guess.
	for _, s := range sections {
		if s.MappingType != ingestion.MappingSingleAyah {
			t.Errorf("ayah %d mapping_type = %q, want single_ayah", s.AyahStart, s.MappingType)
		}
		if s.Ordinal != s.AyahStart-1 {
			t.Errorf("ayah %d ordinal = %d, want %d", s.AyahStart, s.Ordinal, s.AyahStart-1)
		}
	}
}

// A manifest that silently omits a verse would report success while leaving that
// ayah with no commentary, which is the exact failure issue #5 is about.
func TestParseCorpusRefusesAGap(t *testing.T) {
	// Drop ayah 12:2 entirely.
	body := strings.Replace(corpusWith(nil), "## 12:2\n\npassage 2\n\n", "", 1)
	if body == corpusWith(nil) {
		t.Fatal("fixture did not remove 12:2")
	}
	_, err := parseCorpus(writeCorpus(t, body), 12)
	if err == nil {
		t.Fatal("parseCorpus accepted a corpus missing one of surah 12's ayahs")
	}
	if !strings.Contains(err.Error(), "12:2") {
		t.Errorf("error should name the missing ayah, got: %v", err)
	}
}

func TestParseCorpusRefusesDuplicateAyahs(t *testing.T) {
	body := strings.Replace(corpusWith(nil), "## 12:2", "## 12:1", 1)
	_, err := parseCorpus(writeCorpus(t, body), 12)
	if err == nil {
		t.Fatal("parseCorpus accepted two ayah 12:1 headings")
	}
	if !strings.Contains(err.Error(), "more than once") {
		t.Errorf("error should say the ayah repeats, got: %v", err)
	}
}

func TestParseCorpusRefusesAyahOutsideTheSurah(t *testing.T) {
	body := strings.Replace(corpusWith(nil), "## 12:1\n", "## 12:112\n", 1)
	_, err := parseCorpus(writeCorpus(t, body), 12)
	if err == nil {
		t.Fatal("parseCorpus accepted ayah 112, but surah 12 has 111")
	}
	if !strings.Contains(err.Error(), "outside surah") {
		t.Errorf("error should say the ayah is out of range, got: %v", err)
	}
}

func TestParseCorpusRefusesEmptyCommentary(t *testing.T) {
	_, err := parseCorpus(writeCorpus(t, corpusWith(map[int]string{111: ""})), 12)
	if err == nil {
		t.Fatal("parseCorpus accepted an ayah heading with no text")
	}
	if !strings.Contains(err.Error(), "no commentary text") {
		t.Errorf("error should say the text is missing, got: %v", err)
	}
}

// fetch_tafsir writes provenance metadata before the first ayah heading: the
// work's title, author, and the retrieval URL. Attaching that to ayah 1 would
// present an API citation as Ibn Kathir's words, so it must be dropped.
func TestParseCorpusDropsFrontMatter(t *testing.T) {
	front := "# Tafsir edition 14 — Surah 12\n\n**Work:** Tafsir Ibn Kathir\n" +
		"**Retrieved from:** https://api.quran.com/api/v4/tafsirs/14\n\n---\n\n"
	path := writeCorpus(t, front+corpusWith(nil))

	sections, err := parseCorpus(path, 12)
	if err != nil {
		t.Fatalf("parseCorpus: %v", err)
	}
	if len(sections) != surah12Ayahs {
		t.Fatalf("got %d sections, want %d", len(sections), surah12Ayahs)
	}
	first := sections[0].Text
	for _, leaked := range []string{"Retrieved from", "Tafsir edition", "**Work:**", "api.quran.com"} {
		if strings.Contains(first, leaked) {
			t.Errorf("ayah 1 text contains front matter %q: %q", leaked, first)
		}
	}
	if strings.TrimSpace(first) != "passage 1" {
		t.Errorf("ayah 1 text = %q, want %q", first, "passage 1")
	}
}

// The heading pattern must not fire on a prose line that merely looks like one,
// or a mention of a verse would invent a section.
func TestParseCorpusDoesNotReadProseAsHeadings(t *testing.T) {
	body := strings.Replace(corpusWith(nil), "## 12:1\n",
		"the heading for 12:1 follows\n## 12:1\n", 1)

	sections, err := parseCorpus(writeCorpus(t, body), 12)
	if err != nil {
		t.Fatalf("parseCorpus: %v", err)
	}
	if len(sections) != surah12Ayahs {
		t.Fatalf("got %d sections, want %d — a prose line was read as a heading",
			len(sections), surah12Ayahs)
	}
}

func TestParseCorpusRefusesAFileWithNoHeadings(t *testing.T) {
	_, err := parseCorpus(writeCorpus(t, "just some prose with no headings at all\n"), 12)
	if err == nil {
		t.Fatal("parseCorpus accepted a file with no ayah headings")
	}
}

func TestParseCorpusRefusesAnotherSurahsHeadings(t *testing.T) {
	body := strings.Replace(corpusWith(nil), "## 12:1\n", "## 13:1\n", 1)
	_, err := parseCorpus(writeCorpus(t, body), 12)
	if err == nil {
		t.Fatal("parseCorpus accepted a 13:1 heading while building surah 12")
	}
}

func TestParseCorpusKeepsLongPassagesWhole(t *testing.T) {
	// Paragraphs are the chunker's business, not the manifest's: splitting here
	// would bake a chunk size into a committed data file.
	long := strings.Repeat("طا ", 400)
	sections, err := parseCorpus(writeCorpus(t, corpusWith(map[int]string{3: long})), 12)
	if err != nil {
		t.Fatalf("parseCorpus: %v", err)
	}
	want := strings.TrimSpace(long)
	if sections[2].Text != want {
		t.Errorf("ayah 3 text was altered: got %d chars, want %d",
			len([]rune(sections[2].Text)), len([]rune(want)))
	}
}

// The manifest is committed and re-checked, so it must serialise stably: an
// unstable byte order would make -check fail on an unchanged corpus.
func TestManifestRoundTripsDeterministically(t *testing.T) {
	path := writeCorpus(t, corpusWith(nil))

	build := func() ([]byte, error) {
		sections, err := parseCorpus(path, 12)
		if err != nil {
			return nil, err
		}
		return json.MarshalIndent([]ingestion.Document{{
			ID:          "src-surah-12",
			Language:    "ar",
			ContentType: ingestion.ContentTypeTafsir,
			SurahID:     12,
			Sections:    sections,
		}}, "", "  ")
	}

	a, err := build()
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	b, err := build()
	if err != nil {
		t.Fatalf("marshal again: %v", err)
	}
	if string(a) != string(b) {
		t.Error("manifest bytes differ between two runs over an unchanged corpus")
	}
}

func TestEstimateChunksGrowsWithParagraphs(t *testing.T) {
	long := strings.Repeat("ا", ingestion.DefaultMaxChunkRunes+10)

	if got := estimateChunks([]ingestion.Section{{Text: strings.Repeat(long+"\n\n", 3)}}); got < 3 {
		t.Errorf("three over-long paragraphs estimated at %d chunks, want at least 3", got)
	}
	// An empty section is not a chunk; counting one would make the printed plan
	// disagree with what the chunker actually writes.
	if got := estimateChunks([]ingestion.Section{{Text: ""}}); got != 0 {
		t.Errorf("empty section estimated at %d chunks, want 0", got)
	}
}

func TestRawTextRefComesFromTheRegistry(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "corpus.json")
	if err := os.WriteFile(path, []byte(`{"source":{"id":"ibn-kathir-ar","raw_text_ref":"corpus/tafsir/x.md"}}`), 0o644); err != nil {
		t.Fatalf("write registry: %v", err)
	}

	ref, err := rawTextRef(path)
	if err != nil {
		t.Fatalf("rawTextRef: %v", err)
	}
	if ref != "corpus/tafsir/x.md" {
		t.Errorf("ref = %q, want corpus/tafsir/x.md", ref)
	}
}

// The manifest path is derived from the registry's ref, so a wrong root would
// send the tool looking for corpus/corpus/tafsir/... and fail with a confusing
// "no such file" instead of naming the real problem.
func TestRawTextRefResolvesAgainstTheRepositoryRoot(t *testing.T) {
	root := t.TempDir()
	sources := filepath.Join(root, "corpus", "sources")
	if err := os.MkdirAll(sources, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	registry := filepath.Join(sources, "src.json")
	if err := os.WriteFile(registry, []byte(`{"source":{"raw_text_ref":"corpus/tafsir/x.md"}}`), 0o644); err != nil {
		t.Fatalf("write registry: %v", err)
	}
	// The path the tool derives is root/<ref>; it must land inside root.
	ref, err := rawTextRef(registry)
	if err != nil {
		t.Fatalf("rawTextRef: %v", err)
	}
	derived := filepath.Join(filepath.Dir(filepath.Dir(filepath.Dir(registry))), ref)
	want := filepath.Join(root, "corpus", "tafsir", "x.md")
	if filepath.Clean(derived) != filepath.Clean(want) {
		t.Errorf("derived corpus path = %q, want %q", derived, want)
	}
}

func TestSurah12CountMatchesTheRegistryCorpus(t *testing.T) {
	// The manifest's completeness check is written against this count, so a
	// change here must be a deliberate one.
	if got := quran.AyahCount(12); got != surah12Ayahs {
		t.Errorf("quran.AyahCount(12) = %d, want %d", got, surah12Ayahs)
	}
}
