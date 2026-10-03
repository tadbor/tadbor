package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func TestCoversRejectsZeroZeroThematicRanges(t *testing.T) {
	// Thematic and surah-level chunks are stored with 0/0. If these ever counted
	// as covering ayah 1 they would silently become exact-verse answers, which is
	// precisely the confusion the ingestion mapping rules exist to prevent.
	thematic := chunk{AyahStart: 0, AyahEnd: 0, MappingType: "thematic"}
	for _, ayah := range []int{0, 1, 40, 111} {
		if thematic.covers(ayah) {
			t.Fatalf("thematic 0:0 must not cover ayah %d", ayah)
		}
	}

	for _, tc := range []struct {
		name       string
		c          chunk
		ayah       int
		wantCovers bool
	}{
		{"single ayah match", chunk{AyahStart: 5, AyahEnd: 5}, 5, true},
		{"single ayah miss", chunk{AyahStart: 5, AyahEnd: 5}, 6, false},
		{"range starts at ayah", chunk{AyahStart: 10, AyahEnd: 12}, 10, true},
		{"range ends at ayah", chunk{AyahStart: 10, AyahEnd: 12}, 12, true},
		{"range middle", chunk{AyahStart: 10, AyahEnd: 12}, 11, true},
		{"range before ayah", chunk{AyahStart: 10, AyahEnd: 12}, 9, false},
		{"range after ayah", chunk{AyahStart: 10, AyahEnd: 12}, 13, false},
	} {
		if got := tc.c.covers(tc.ayah); got != tc.wantCovers {
			t.Fatalf("%s: got %v, want %v", tc.name, got, tc.wantCovers)
		}
	}
}

func TestClassify(t *testing.T) {
	t.Run("exactly mapped chunk is a pass", func(t *testing.T) {
		v, _ := classify(4, []chunk{{MappingType: "single_ayah", AyahStart: 4, AyahEnd: 4}})
		if v != VerdictExact {
			t.Fatalf("got %s, want %s", v, VerdictExact)
		}
	})

	t.Run("multi-ayah chunk covering the ayah is a pass", func(t *testing.T) {
		v, _ := classify(11, []chunk{{MappingType: "multi_ayah", AyahStart: 10, AyahEnd: 12}})
		if v != VerdictExact {
			t.Fatalf("got %s, want %s", v, VerdictExact)
		}
	})

	// A thematic chunk is reachable evidence but its relevance to this ayah is
	// unproven, so it must never be recorded as a pass.
	t.Run("thematic chunk needs review", func(t *testing.T) {
		v, _ := classify(40, []chunk{{MappingType: "thematic", AyahStart: 0, AyahEnd: 0}})
		if v != VerdictThematic {
			t.Fatalf("got %s, want %s", v, VerdictThematic)
		}
	})

	t.Run("mixed exact and thematic still passes", func(t *testing.T) {
		v, _ := classify(7, []chunk{
			{MappingType: "single_ayah", AyahStart: 7, AyahEnd: 7},
			{MappingType: "thematic", AyahStart: 0, AyahEnd: 0},
		})
		if v != VerdictExact {
			t.Fatalf("got %s, want %s", v, VerdictExact)
		}
	})

	t.Run("chunk mapped to a different ayah needs review", func(t *testing.T) {
		// The mismatch case issue #7 exists to catch: evidence came back, but it
		// is anchored elsewhere, so it cannot be recorded as correct for this ayah.
		v, _ := classify(9, []chunk{{MappingType: "single_ayah", AyahStart: 3, AyahEnd: 3}})
		if v != VerdictThematic {
			t.Fatalf("got %s, want %s", v, VerdictThematic)
		}
	})

	t.Run("empty evidence is a gap", func(t *testing.T) {
		v, _ := classify(1, nil)
		if v != VerdictInsufficient {
			t.Fatalf("got %s, want %s", v, VerdictInsufficient)
		}
	})
}

func TestReportRefusesToPresentAnEmptyCorpusAsAPass(t *testing.T) {
	// The most important property of this harness: with nothing ingested, the
	// record must say validation was blocked, not imply 111 ayahs checked out.
	results := make([]result, 0, 111)
	for a := 1; a <= 111; a++ {
		results = append(results, result{Ayah: a, Verdict: VerdictInsufficient})
	}
	md := report(12, 1, 111, results, time.Unix(0, 0))

	if !strings.Contains(md, "VALIDATION BLOCKED") {
		t.Fatal("an all-gap run must be flagged as blocked")
	}
	if strings.Contains(md, "this is not a pass") == false {
		t.Fatal("the blocked banner must state explicitly that it is not a pass")
	}
	for _, want := range []string{"12:1 — no approved evidence", "12:111 — no approved evidence"} {
		if !strings.Contains(md, want) {
			t.Fatalf("gap %q missing from the record", want)
		}
	}
	if !strings.Contains(md, "| **Total** | **111** |") {
		t.Fatal("summary must account for all 111 ayahs")
	}
}

func TestReportWarnsWhenNothingHasExactMapping(t *testing.T) {
	results := []result{
		{Ayah: 1, Verdict: VerdictThematic, Chunks: []chunk{{ID: "c1", MappingType: "thematic"}}},
		{Ayah: 2, Verdict: VerdictInsufficient},
	}
	md := report(12, 1, 2, results, time.Unix(0, 0))

	if !strings.Contains(md, "No ayah had exact verse mapping") {
		t.Fatal("a surah answered entirely thematically must be called out")
	}
	if !strings.Contains(md, "Awaiting human review (1)") {
		t.Fatal("thematic results must be listed as awaiting review")
	}
	if !strings.Contains(md, "`c1`") {
		t.Fatal("the review section must name the chunks a human has to read")
	}
	if !strings.Contains(md, "12:2 — no approved evidence") {
		t.Fatal("gaps must still be listed alongside review items")
	}
}

func TestReportCleanRunHasNoBlockedBanner(t *testing.T) {
	results := []result{
		{Ayah: 1, Verdict: VerdictExact, Chunks: []chunk{{MappingType: "single_ayah", AyahStart: 1, AyahEnd: 1}}},
		{Ayah: 2, Verdict: VerdictExact, Chunks: []chunk{{MappingType: "single_ayah", AyahStart: 2, AyahEnd: 2}}},
	}
	md := report(12, 1, 2, results, time.Unix(0, 0))

	if strings.Contains(md, "VALIDATION BLOCKED") {
		t.Fatal("a run with evidence must not be marked blocked")
	}
	if !strings.Contains(md, "None: every ayah returned evidence.") {
		t.Fatal("a clean run should state there are no gaps")
	}
}

func TestPreflight(t *testing.T) {
	t.Run("accepts the real API", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			io.WriteString(w, `{"status":"ok"}`)
		}))
		defer srv.Close()
		if err := preflight(srv.Client(), srv.URL); err != nil {
			t.Fatalf("unexpected: %v", err)
		}
	})

	// The mistake this guards: the SPA dev server answers every unknown path
	// with 200 and an HTML shell, which without a preflight looks like 111
	// decode errors rather than a wrong port.
	t.Run("rejects an HTML dev server", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html")
			io.WriteString(w, "<!DOCTYPE html><html></html>")
		}))
		defer srv.Close()
		err := preflight(srv.Client(), srv.URL)
		if err == nil {
			t.Fatal("an HTML response must fail the preflight")
		}
		if !strings.Contains(err.Error(), "not the tadbor API") {
			t.Fatalf("error should name the cause, got %v", err)
		}
	})

	t.Run("rejects a non-ok status", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusServiceUnavailable)
			io.WriteString(w, `{"status":"starting"}`)
		}))
		defer srv.Close()
		if err := preflight(srv.Client(), srv.URL); err == nil {
			t.Fatal("a 503 must fail the preflight")
		}
	})

	t.Run("rejects malformed JSON", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			io.WriteString(w, `not json`)
		}))
		defer srv.Close()
		if err := preflight(srv.Client(), srv.URL); err == nil {
			t.Fatal("malformed JSON must fail the preflight")
		}
	})

	t.Run("rejects an unreachable host", func(t *testing.T) {
		if err := preflight(http.DefaultClient, "http://127.0.0.1:1"); err == nil {
			t.Fatal("an unreachable host must fail the preflight")
		}
	})
}

func TestTruncate(t *testing.T) {
	if got := truncate("short", 100); got != "short" {
		t.Fatalf("got %q", got)
	}
	// The limit counts input characters, so the ellipsis is added on top. "…" is
	// three bytes, hence counting runes rather than len.
	got := truncate(strings.Repeat("x", 50)+"tail", 10)
	if utf8.RuneCountInString(got) != 11 || !strings.HasSuffix(got, "…") {
		t.Fatalf("got %q (%d runes)", got, utf8.RuneCountInString(got))
	}
	if got := truncate("a\nb", 100); got != "a b" {
		t.Fatalf("newlines should be flattened, got %q", got)
	}
}
