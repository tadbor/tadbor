package main

import (
	"strconv"
	"strings"
	"testing"

	"go.mongodb.org/mongo-driver/bson"

	"tadbor/backend/internal/ingestion"
	"tadbor/backend/internal/textverify"
)

func TestParseAyahList(t *testing.T) {
	cases := []struct {
		in   string
		want []int
		bad  bool
	}{
		{in: "1", want: []int{1}},
		{in: "1,34,43", want: []int{1, 34, 43}},
		{in: " 1 , 34 ", want: []int{1, 34}},
		{in: "12-15", want: []int{12, 13, 14, 15}},
		{in: "20,12-14", want: []int{12, 13, 14, 20}}, // sorted, not in input order
		{in: "1,,2", want: []int{1, 2}},
		{in: "", want: nil},
		{in: "one", bad: true},
		{in: "3-", bad: true},
		{in: "-4", bad: true},
	}
	for _, tc := range cases {
		got, err := parseAyahList(tc.in)
		if tc.bad {
			if err == nil {
				t.Errorf("parseAyahList(%q) = %v, want an error", tc.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("parseAyahList(%q): %v", tc.in, err)
			continue
		}
		if strings.Join(fmtInts(got), ",") != strings.Join(fmtInts(tc.want), ",") {
			t.Errorf("parseAyahList(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func fmtInts(v []int) []string {
	parts := make([]string, len(v))
	for i, n := range v {
		parts[i] = strconv.Itoa(n)
	}
	return parts
}

// The default sheet is the review queue. A selection that quietly appended an
// extra ayah would double it in the output, so the queue must not be padded.
func TestSelectEntriesDefaultsToTheQueueWithoutDuplicates(t *testing.T) {
	entries := []entry{
		mkEntry(1, 0, 0, false),
		mkEntry(2, 6, 0.8, false),
		mkEntry(3, 1, 0.0, false),
	}
	queue := []entry{entries[0], entries[2]}

	got, err := selectEntries(entries, queue, "", false, false)
	if err != nil {
		t.Fatalf("selectEntries: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("selected %d entries, want the 2 in the queue: %v", len(got), ayahNums(got))
	}
	seen := map[int]int{}
	for _, e := range got {
		seen[e.verse.number]++
	}
	for n, count := range seen {
		if count != 1 {
			t.Errorf("ayah %d appears %d times in one sheet", n, count)
		}
	}
}

func TestSelectEntriesAllAndExplicit(t *testing.T) {
	entries := []entry{mkEntry(1, 0, 0, false), mkEntry(2, 6, 0.8, false), mkEntry(3, 1, 0, false)}

	all, err := selectEntries(entries, entries[:1], "", false, true)
	if err != nil || len(all) != 3 {
		t.Fatalf("-all selected %d entries (err %v), want 3", len(all), err)
	}

	explicit, err := selectEntries(entries, nil, "3,1", false, false)
	if err != nil {
		t.Fatalf("-ayahs: %v", err)
	}
	if got := ayahNums(explicit); got != "1,3" {
		t.Errorf("-ayahs 3,1 selected %s, want 1,3", got)
	}

	if _, err := selectEntries(entries, nil, "99", false, false); err == nil {
		t.Error("asked for an ayah the manifest does not contain and got no error")
	}
}

// The digest must agree with check_coverage on what needs a human. If the two
// drift, the sheet either hides entries or invents them.
func TestVerdictMatchesTheCoverageRule(t *testing.T) {
	const minRun = 4
	cases := []struct {
		name  string
		score textverify.Score
		want  string
	}{
		{"quoted well", textverify.Score{LongestRun: 6}, "mapped-corroborated"},
		{"exactly at the threshold", textverify.Score{LongestRun: minRun}, "mapped-corroborated"},
		{"introduces the verse", textverify.Score{LongestRun: 1, Introduced: true}, "mapped-corroborated"},
		{"quotes by shingles only", textverify.Score{QuoteCoverage: 0.9, Quoted: true}, "mapped-corroborated"},
		{"nothing fires", textverify.Score{LongestRun: 1}, "mapped-uncorroborated"},
		{"silent", textverify.Score{}, "mapped-uncorroborated"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := verdict(tc.score, minRun); got != tc.want {
				t.Errorf("verdict = %q, want %q", got, tc.want)
			}
		})
	}
}

// A sheet a reviewer cannot act on is worse than none: every entry needs its
// passage in full, its ayah, and the provenance to tie both to bytes.
func TestRenderEntryCarriesEverythingAReviewerNeeds(t *testing.T) {
	e := entry{
		section: ingestion.Section{
			Ordinal: 7, SurahID: 12, AyahStart: 34, AyahEnd: 34,
			MappingType: ingestion.MappingSingleAyah,
			Text:        "قال تعالى hits the verse here",
		},
		verse:   verseText{number: 34, uthmani: "وَقَالُوا", simple: "وقالوا"},
		score:   textverify.Score{LongestRun: 1},
		verdict: "mapped-uncorroborated",
	}

	var b strings.Builder
	renderEntry(&b, 12, e)
	out := b.String()

	for _, want := range []string{
		"## 12:34",
		"وَقَالُوا",                     // the ayah
		"قال تعالى hits the verse here", // the passage in full
		"single_ayah",
		"needs a human",
		"- [ ] faithful",
		"- [ ] suspect",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("sheet entry is missing %q:\n%s", want, out)
		}
	}
}

// The simplified text is search-only. Printing it beside the Uthmani without
// saying so invites a reviewer to quote the wrong one.
func TestRenderEntryLabelsTheSimplifiedText(t *testing.T) {
	e := entry{
		section: ingestion.Section{Ordinal: 0, SurahID: 12, AyahStart: 1, AyahEnd: 1, Text: "نص"},
		verse:   verseText{number: 1, uthmani: "الر", simple: "الر تلك"},
		score:   textverify.Score{},
		verdict: "mapped-uncorroborated",
	}
	var b strings.Builder
	renderEntry(&b, 12, e)

	if !strings.Contains(b.String(), "never quote this as the Quran") {
		t.Error("simplified text is shown without warning that it is not quotable")
	}
}

// -verify must print provenance without the passages, so a reviewer can check the
// hashes before spending time reading.
func TestHeaderOnlyCutsBeforeTheEntries(t *testing.T) {
	md := "header text\n\n---\n\n# Entries\n\nsecret passage\n"
	got := headerOnly(md)
	if strings.Contains(got, "secret passage") || !strings.Contains(got, "header text") {
		t.Errorf("headerOnly = %q, want the header without the entries", got)
	}
	if headerOnly("no separator here") != "no separator here" {
		t.Error("headerOnly should pass through input it cannot split")
	}
}

// The sheet claims a source hash; if it renders an empty one, a reviewer is being
// invited to trust an unprovenanced passage.
func TestHeaderNeverEmitsEmptyProvenance(t *testing.T) {
	h := header{
		sourceID: "s",
		source: &ingestion.Source{
			Title: "Tafsir Ibn Kathir",
			Metadata: bson.M{
				"digitisation":         "third-party API response, not a scan of a printing",
				"upstream_resource_id": "14",
			},
		},
		version: &ingestion.SourceVersion{
			ID: "s-v1", Edition: "ed", ContentHash: "abc", RawTextRef: "corpus/x.md",
		},
		surah: 12, total: 111, queue: 19, selected: 19, minRun: 4,
		corpus: provenance{version: "v4", hash: "def"},
	}
	out := h.render()
	for _, want := range []string{"s-v1", "abc", "corpus/x.md", "v4", "def",
		"not a scan of a printing", "print edition used", "Reviewer:", "-tafsir-id 14"} {
		if !strings.Contains(out, want) {
			t.Errorf("header is missing %q", want)
		}
	}
}

// This text is an API response, not a scan. A sheet that told a reviewer to
// compare it against "the printed edition <website slug>" would be asking for a
// circular check and implying a printing that does not exist.
func TestHeaderDoesNotClaimTheWebEditionIsAPrintedOne(t *testing.T) {
	out := header{
		sourceID: "s",
		source:   &ingestion.Source{Title: "T", Metadata: bson.M{"digitisation": "API response, not a scan"}},
		version:  &ingestion.SourceVersion{Edition: "quran.com/ar-tafsir-ibn-kathir"},
		surah:    12, total: 111, queue: 19, selected: 19, minRun: 4,
	}.render()

	if strings.Contains(out, "the printed edition `quran.com") {
		t.Error("header still presents the web edition slug as a printed edition")
	}
	for _, want := range []string{"not a scan", "against a printing called", "print edition used for this review"} {
		if !strings.Contains(out, want) {
			t.Errorf("header is missing the caveat %q", want)
		}
	}
}

// A source record with no metadata must still render, not panic: the caveat is
// important but its absence is not a reason to fail the run.
func TestHeaderRendersWithoutMetadata(t *testing.T) {
	out := header{
		sourceID: "s",
		source:   &ingestion.Source{Title: "T"},
		version:  &ingestion.SourceVersion{Edition: "ed", ID: "ed-1"},
		surah:    1, total: 1, queue: 1, selected: 1, minRun: 4,
	}.render()
	if !strings.Contains(out, "T") {
		t.Error("header did not render without metadata")
	}
}

func mkEntry(ayah, run int, coverage float64, introduced bool) entry {
	return entry{
		section: ingestion.Section{Ordinal: ayah - 1, SurahID: 12, AyahStart: ayah, AyahEnd: ayah},
		verse:   verseText{number: ayah},
		score: textverify.Score{
			LongestRun: run, QuoteCoverage: coverage, Introduced: introduced,
		},
	}
}

func ayahNumbers(entries []entry) []int {
	out := make([]int, len(entries))
	for i, e := range entries {
		out[i] = e.verse.number
	}
	return out
}

func ayahNums(entries []entry) string {
	return strings.Join(fmtInts(ayahNumbers(entries)), ",")
}
