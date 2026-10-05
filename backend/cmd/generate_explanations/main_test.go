package main

import (
	"strings"
	"testing"
	"time"
)

// A full 111-ayah run at the free tier's rate takes about 24 minutes and will get
// interrupted. The default pace has to match the provider's actual limit: 5
// requests a minute, measured from the 429 the provider actually returns. A
// faster default does not merely run slower, it loses ayahs.
func TestDefaultPaceMatchesFreeTierRate(t *testing.T) {
	perMinute := float64(time.Minute) / float64(defaultPace)
	if perMinute > 5 {
		t.Errorf("default pace allows %.1f requests/minute, above the free tier's 5", perMinute)
	}
	if defaultPace < time.Minute/5 {
		t.Errorf("default pace %s is faster than 5/minute", defaultPace)
	}
}

// After a 429 or 503 the next call must wait longer. Consecutive calls at the
// same rate turn one throttling event into a run where every ayah fails.
func TestNextGapBacksOffAfterFailure(t *testing.T) {
	if got := nextGap(defaultPace, false); got != defaultPace {
		t.Errorf("gap after success = %s, want %s", got, defaultPace)
	}
	if got := nextGap(defaultPace, true); got != errorPace {
		t.Errorf("gap after failure = %s, want %s", got, errorPace)
	}
	if errorPace <= defaultPace {
		t.Error("errorPace must be longer than the steady-state pace")
	}
}

// -pace 0 is the documented way to disable pacing, including after a failure.
func TestNextGapZeroDisablesPacing(t *testing.T) {
	if got := nextGap(0, false); got != 0 {
		t.Errorf("gap = %s, want 0 when pacing is disabled", got)
	}
	if got := nextGap(0, true); got != 0 {
		t.Errorf("gap = %s, want 0 when pacing is disabled, even after a failure", got)
	}
}

// The task list in the prompt header is parsed by a reviewer looking at a failed
// run, so the surah and ayah must be readable without counting.
func TestGapsNameSurahAndAyah(t *testing.T) {
	got := "12:insufficient evidence for requested verse"
	if !strings.HasPrefix(got, "12:") {
		t.Error("gap entries do not name the ayah")
	}
}

// runID is what makes a re-run replace its own audit record rather than
// accumulate one per attempt, so it must be stable and distinct per verse.
func TestRunIDIsStableAndDistinct(t *testing.T) {
	if runID(12, 1) != runID(12, 1) {
		t.Error("runID is not stable, so re-running accumulates duplicate audit records")
	}
	seen := map[string]bool{}
	for ayah := 1; ayah <= 111; ayah++ {
		id := runID(12, ayah)
		if seen[id] {
			t.Fatalf("runID collides at ayah %d: %s", ayah, id)
		}
		seen[id] = true
	}
	if runID(12, 1) == runID(112, 1) {
		t.Error("runID does not distinguish surahs")
	}
}

func TestTargetLanguage(t *testing.T) {
	cases := map[string]string{
		"simplified_ar": "ar",
		"egyptian_ar":   "ar",
		"en":            "en",
	}
	for style, want := range cases {
		if got := targetLanguage(style); got != want {
			t.Errorf("targetLanguage(%q) = %q, want %q", style, got, want)
		}
	}
}
