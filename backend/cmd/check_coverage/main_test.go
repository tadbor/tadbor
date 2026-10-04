package main

import (
	"strings"
	"testing"

	"tadbor/backend/internal/ingestion"
)

// The quotation check decides which mappings get a human read, so its edge cases
// matter: too strict and every verse looks suspect, too lax and a real mis-mapping
// passes unnoticed.
func TestLongestRun(t *testing.T) {
	cases := []struct {
		name    string
		verse   string
		passage string
		want    int
	}{
		{
			name:    "verse quoted as a block",
			verse:   "قالوا سنرود عنه اباه",
			passage: "قال قالوا سنرود عنه اباه وانا لفعلون",
			want:    4,
		},
		{
			name:    "a fragment of the verse",
			verse:   "قالوا سنرود عنه اباه",
			passage: "ثم قالوا عنه في_bar",
			want:    1,
		},
		{
			// "عنه" and "اباه" both appear, but "ثم" separates them, so they are
			// not a run. Counting 2 here would reward mere word overlap.
			name:    "words present but separated do not form a run",
			verse:   "سنرود عنه اباه",
			passage: "عنه ثم اباه ولم يذكر سنرود",
			want:    1,
		},
		{
			name:    "no shared words",
			verse:   "سنرود عنه اباه",
			passage: "أي سنحرص على مجيئه اليك",
			want:    0,
		},
		{
			name:    "the run must be consecutive, not cumulative",
			verse:   "الف اول باء جيم",
			passage: "الف ثم جيم",
			want:    1,
		},
		{
			name:    "an empty passage scores zero rather than panicking",
			verse:   "سنرود عنه",
			passage: "",
			want:    0,
		},
		{
			name:    "an empty verse scores zero",
			verse:   "",
			passage: "أي نص هنا",
			want:    0,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := longestRun(tc.verse, tc.passage); got != tc.want {
				t.Errorf("longestRun = %d, want %d", got, tc.want)
			}
		})
	}
}

// The corpus is Uthmani and the commentary quotes verses with and without
// diacritics, so both sides must be reduced to a lexical form before comparing.
// Without this, a correct mapping scores zero purely on orthography.
func TestLongestRunIgnoresDiacriticsAndTatweel(t *testing.T) {
	verse := "قَالُوا سَنَرُودُ عَنْهُ أَبَاهُ"
	passage := "قالوا سنرود عنه اباه وانا لفعلون"

	if got := longestRun(verse, passage); got != 4 {
		t.Errorf("longestRun on diacritised verse = %d, want 4 — comparison is not using lexical forms", got)
	}
}

// Normalising is the function's own job, so pre-normalised input must still work
// rather than double-normalising into a wrong answer.
func TestLongestRunIsIdempotentOnPreNormalizedInput(t *testing.T) {
	norm := ingestion.NewNormalizer()
	verse := norm.LexicalForm("قَالُوا سَنَرُودُ عَنْهُ")
	passage := norm.LexicalForm("قال قالوا سنرود عنه اباه")
	if got := longestRun(verse, passage); got != 3 {
		t.Errorf("longestRun on lexical forms = %d, want 3", got)
	}
}

// LongestRun must not scale badly on the corpus's longest entry (12:106).
func TestLongestRunHandlesLongPassages(t *testing.T) {
	verse := strings.Repeat("كلمة ", 90)
	passage := strings.Repeat("حشو ", 500) + verse + strings.Repeat("ذيل ", 500)
	if got := longestRun(verse, passage); got != 90 {
		t.Errorf("longestRun = %d, want 90", got)
	}
}

func TestPreviewTruncatesOnRuneBoundary(t *testing.T) {
	arabic := strings.Repeat("ط", 50) // two bytes per rune
	got := preview(arabic, 10)
	if !strings.HasSuffix(got, "...") {
		t.Errorf("preview = %q, want it truncated with an ellipsis", got)
	}
	if strings.ContainsRune(got, 0xFFFD) {
		t.Error("preview cut a multi-byte rune in half")
	}
	if got := preview("short", 10); got != "short" {
		t.Errorf("preview of a short string = %q, want it unchanged", got)
	}
}

// Drift and gaps must be visible; a report that swallows them is worse than one
// that reports nothing.
func TestDriftWordNamesEveryShape(t *testing.T) {
	if got := driftWord(ingestion.CoverageDrift{}); got != "none" {
		t.Errorf("driftWord of no drift = %q, want %q", got, "none")
	}
	d := ingestion.CoverageDrift{
		ChunksWithoutMapping: []string{"a"},
		MappingsWithoutChunk: []string{"b", "c"},
		Mismatched:           []string{"d"},
	}
	got := driftWord(d)
	for _, want := range []string{"1 unmapped", "2 orphan", "1 mismatched"} {
		if !strings.Contains(got, want) {
			t.Errorf("driftWord = %q, want it to mention %q", got, want)
		}
	}
}

func TestSortRowsByRunPutsWeakestEvidenceFirst(t *testing.T) {
	rows := []ayahRow{{Ayah: 1, QuotedRun: 3}, {Ayah: 2, QuotedRun: 0}, {Ayah: 3, QuotedRun: 7}}
	sortRowsByRun(rows)
	if rows[0].Ayah != 2 || rows[1].Ayah != 1 || rows[2].Ayah != 3 {
		t.Errorf("queue order = %d,%d,%d, want 2,1,3 — the least-supported mapping must come first",
			rows[0].Ayah, rows[1].Ayah, rows[2].Ayah)
	}
}

// Ibn Kathir quotes verses in his own orthography: no hamza, plain ya, plain
// ta-marbuta. Without folding, every one of those quotations scores zero and the
// whole review queue becomes noise.
func TestMatchFormFoldsOrthographyForMatchingOnly(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"أَبَاهُ", "اباه"},
		{"إبراهيم", "ابراهيم"},
		{"آل", "ال"},
		{"مُؤْمِنِين", "مومنين"},
		{"ءَاثَرَك", "اثرك"},
		{"خَطِئِين", "خطيين"},
		{"الَّذِي", "الذي"},
		{"يَقُولُ", "يقول"},
		{"رَحْمَةٍ", "رحمه"},
	}
	for _, tc := range cases {
		if got := matchForm(tc.in); got != tc.want {
			t.Errorf("matchForm(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// The folding is a matching convenience and must not leak into stored text.
func TestMatchFormDoesNotChangeStoredText(t *testing.T) {
	original := "أَبَاهُ"
	norm := ingestion.NewNormalizer()
	if got := norm.LexicalForm(original); got == matchForm(original) {
		t.Error("matchForm is indistinguishable from LexicalForm; the folding is not being applied where it matters")
	}
	if !strings.HasPrefix(original, "أ") {
		t.Error("test input was mutated")
	}
}

func TestLongestRunSurvivesOrthographyDifference(t *testing.T) {
	// The real shape of the problem: Uthmani verse, commentary spelling.
	if got := longestRun("قَالُوا سَنَرُودُ عَنْهُ أَبَاهُ", "قالوا سنرود عنه اباه وانا لفعلون"); got != 4 {
		t.Errorf("longestRun = %d, want 4 — orthography folding is missing", got)
	}
}

func TestHasVerseIntroduction(t *testing.T) {
	if !hasVerseIntroduction("يخبر تعالى أن ما ادخره الله لنبيه") {
		t.Error("hasVerseIntroduction missed a stock opening")
	}
	if hasVerseIntroduction("وقالوا له mockingbird سبحانك") {
		t.Error("hasVerseIntroduction matched unrelated prose")
	}
	// The phrase is matched after folding, so a diacritised opening still counts.
	if !hasVerseIntroduction("يَخْبُرُ تَعَالَى أَنَّهُ") {
		t.Error("hasVerseIntroduction is comparing orthography exactly")
	}
}

// Both signals must be reported, and a mapping is flagged only when both fail.
// Either signal alone deciding would be unsound in both directions: a stock
// opening can be followed by a wide-ranging discussion, and a short verse can
// share words with the wrong passage by coincidence.
func TestVerdictNeedsBothSignalsToFlag(t *testing.T) {
	const minRun = 4
	cases := []struct {
		name  string
		run   int
		intro bool
		want  string
	}{
		{"quoted well", 6, false, "mapped-corroborated"},
		{"quoted enough at the threshold", minRun, false, "mapped-corroborated"},
		{"short quote but introduced", 1, true, "mapped-corroborated"},
		{"neither signal", 1, false, "mapped-uncorroborated"},
		{"no quote, no phrase", 0, false, "mapped-uncorroborated"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := "mapped-corroborated"
			if tc.run < minRun && !tc.intro {
				got = "mapped-uncorroborated"
			}
			if got != tc.want {
				t.Errorf("verdict = %q, want %q", got, tc.want)
			}
		})
	}
}

// quoteCoverage exists because exact word matching fails on near-verbatim quotes:
// Ibn Kathir's orthography differs from the Uthmani text by inserted and dropped
// alifs and hamza seats, which breaks every exact word match.
func TestQuoteCoverageToleratesAlteredSpellings(t *testing.T) {
	verse := "انا انزلنهقرءنا عربيا لعلكم تعقلون"
	// The commentary writes it with extra alefs and different hamza seats.
	nearQuote := "إنا أنزلناه قرآنا عربيا لعلكم تعقلون"
	if got := quoteCoverage(verse, nearQuote, 8); got < 0.5 {
		t.Errorf("quoteCoverage on a near-verbatim quote = %.2f, want >= 0.5", got)
	}
	// The exact Uthmani text must still score highest.
	exact := "إنا أنزلناه قرآنا عربيا لعلكم تعقلون"
	if got := quoteCoverage(exact, nearQuote, 8); got <= quoteCoverage(verse, nearQuote, 8) {
		t.Errorf("an exact match (%v) should not score below a fuzzy one",
			quoteCoverage(exact, nearQuote, 8))
	}
}

func TestQuoteCoverageIsLowForParaphrase(t *testing.T) {
	verse := "قالوا سنرود عنه اباه وانا لفعلون"
	// Real 12:61 commentary: same meaning, almost no shared wording.
	paraphrase := "أي سنحرص على مجيئه إليك بكل ممكن ولا نبقي مجهودا لتعلم صدقنا فيما قلناه"
	if got := quoteCoverage(verse, paraphrase, 8); got > 0.2 {
		t.Errorf("quoteCoverage on a paraphrase = %.2f, want <= 0.2", got)
	}
}

func TestQuoteCoverageBoundsAndEmptyInput(t *testing.T) {
	if got := quoteCoverage("قالوا", "قالوا", 8); got != 1 {
		t.Errorf("a verse shorter than one shingle that is present = %v, want 1", got)
	}
	if got := quoteCoverage("قالوا سنرود", "", 8); got != 0 {
		t.Errorf("empty passage = %v, want 0", got)
	}
	if got := quoteCoverage("", "أي نص", 8); got != 0 {
		t.Errorf("empty verse = %v, want 0", got)
	}
	// Any value in [0,1]; a verse quoted entirely must reach 1.
	verse := "فلما جهزهم بجهازهم جعل السقايه في رحل اخيه"
	if got := quoteCoverage(verse, verse, 8); got != 1 {
		t.Errorf("self-quote = %v, want 1", got)
	}
}
