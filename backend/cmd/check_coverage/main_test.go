package main

import (
	"strings"
	"testing"

	"tadbor/backend/internal/ingestion"
	"tadbor/backend/internal/textverify"
)

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
			got := verdict(textverify.Score{LongestRun: tc.run, Introduced: tc.intro}, minRun)
			if got != tc.want {
				t.Errorf("verdict = %q, want %q", got, tc.want)
			}
		})
	}
}

// quoteCoverage exists because exact word matching fails on near-verbatim quotes:
// Ibn Kathir's orthography differs from the Uthmani text by inserted and dropped
// alifs and hamza seats, which breaks every exact word match.
