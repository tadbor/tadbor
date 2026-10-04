package main

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"tadbor/backend/internal/quran"

	"testing"
)

func TestSearchFold(t *testing.T) {
	t.Run("strips pronunciation marks but keeps letters", func(t *testing.T) {
		// Uthmani with full harakat and a dagger alef.
		uthmani := "الٓر ۚ تِلْكَ ءَايَٰتُ ٱلْكِتَٰبِ ٱلْمُبِينِ"
		got := searchFold(uthmani)
		for _, mark := range []string{"َ", "ِ", "ْ", "ٰ", "ۚ"} {
			if strings.Contains(got, mark) {
				t.Fatalf("%q still contains %q", got, mark)
			}
		}
		if got != "الر تلك ءايت الكتب المبين" {
			t.Fatalf("got %q", got)
		}
	})

	// A reader's query may type any of these spellings; folding them together is
	// the whole point of text_simple.
	t.Run("folds publisher spelling variants", func(t *testing.T) {
		for _, pair := range [][2]string{
			{"آ", "ا"}, {"أ", "ا"}, {"إ", "ا"}, {"ٱ", "ا"},
			{"ى", "ي"}, {"ة", "ه"},
		} {
			if got := searchFold(pair[0]); got != searchFold(pair[1]) {
				t.Fatalf("%q folded to %q but %q folded to %q", pair[0], got, pair[1], searchFold(pair[1]))
			}
		}
	})

	t.Run("is idempotent", func(t *testing.T) {
		once := searchFold("إِنَّآ أَنزَلْنَـٰهُ قُرْءَٰنًا عَرَبِيًّا")
		if twice := searchFold(once); twice != once {
			t.Fatalf("not idempotent: %q then %q", once, twice)
		}
	})

	// quran.com writes the dagger alef with an elongation glyph next to it, so
	// real corpus text hits this constantly; if tatweel survives it becomes part
	// of the stored search key and stops matching typed queries.
	t.Run("strips tatweel", func(t *testing.T) {
		got := searchFold("ءَايَـٰتُ ٱلْكِتَـٰبِ")
		if strings.ContainsRune(got, '\u0640') {
			t.Fatalf("tatweel survived into the search key: %q", got)
		}
		if got != "ءايت الكتب" {
			t.Fatalf("got %q", got)
		}
	})

	t.Run("collapses whitespace", func(t *testing.T) {
		if got := searchFold("الر   تلك\tءايت"); got != "الر تلك ءايت" {
			t.Fatalf("got %q", got)
		}
	})

	t.Run("never returns empty for real text", func(t *testing.T) {
		if got := searchFold("َُِّْ"); got != "" {
			t.Fatalf("marks-only input should fold to empty, got %q", got)
		}
	})
}

func TestLetterKey(t *testing.T) {
	// The same word, encoded two different but legitimate Uthmani ways. These must
	// compare equal, or a real publisher variance would look like a typo.
	hamzaAbove := "ٱلْـَٔايَٰتِ"   // ...ـَٔايَٰت
	hamzaPlusAlef := "ٱلْءَايَٰتِ" // ...ءايَٰت
	if letterKey(hamzaAbove) != letterKey(hamzaPlusAlef) {
		t.Fatalf("alef-encoding variants must compare equal:\n  %q\n  %q",
			letterKey(hamzaAbove), letterKey(hamzaPlusAlef))
	}

	// Tanzil and quran.com write the dagger alef with and without tatweel.
	withTatweel := "ٱلْكِتَـٰبِ"
	withoutTatweel := "ٱلْكِتَٰبِ"
	if letterKey(withTatweel) != letterKey(withoutTatweel) {
		t.Fatal("tatweel must not affect the letter key")
	}

	// A genuine textual difference must still be caught.
	if letterKey("لَقَدْ كَانَ") == letterKey("لَمْ يَكُن") {
		t.Fatal("different words must not compare equal")
	}
}

func TestStripBasmala(t *testing.T) {
	// Both sources that fold the Basmala in put it before the real verse.
	basmala := "بِسْمِ ٱللَّهِ ٱلرَّحْمَٰنِ ٱلرَّحِيمِ"

	t.Run("removes a leading basmala", func(t *testing.T) {
		got := stripBasmala(basmala + " الٓر ۚ تِلْكَ ءَايَٰتُ ٱلْكِتَٰبِ ٱلْمُبِينِ")
		if strings.Contains(got, "بسم") {
			t.Fatalf("basmala survived: %q", got)
		}
		if !strings.HasPrefix(got, "الٓر") {
			t.Fatalf("verse text was damaged: %q", got)
		}
	})

	t.Run("leaves a verse with no basmala untouched", func(t *testing.T) {
		// The guard matters: a blanket "first four words" cut would silently
		// destroy four real words of every unaffected ayah.
		verse := "إِنَّآ أَنزَلْنَـٰهُ قُرْءَٰنًا عَرَبِيًّا لَّعَلَّكُمْ تَعْقِلُونَ"
		if got := stripBasmala(verse); got != verse {
			t.Fatalf("got %q, want unchanged", got)
		}
	})

	t.Run("does not strip a basmala that is not leading", func(t *testing.T) {
		text := "الٓر ۚ تِلْكَ ءَايَٰتُ ٱلْكِتَٰبِ بسم الله"
		if got := stripBasmala(text); got != text {
			t.Fatalf("got %q, want unchanged", got)
		}
	})

	t.Run("tolerates short input", func(t *testing.T) {
		if got := stripBasmala("بسم"); got != "بسم" {
			t.Fatalf("got %q", got)
		}
	})
}

func TestHashTextIsStableAndSensitive(t *testing.T) {
	const a = "الٓر ۚ تِلْكَ ءَايَٰتُ ٱلْكِتَٰبِ ٱلْمُبِينِ"
	if hashText(a) != hashText(a) {
		t.Fatal("hash is not deterministic")
	}
	if len(hashText(a)) != 64 {
		t.Fatalf("expected a sha256 hex digest, got %d chars", len(hashText(a)))
	}
	// One diacritic must change the hash: text_simple may be identical while the
	// verified Uthmani differs, and that difference has to be detectable.
	if hashText(a) == hashText(a+"َ") {
		t.Fatal("hash ignores a diacritic difference")
	}
}

// A cross-source that fails to download must not be read as "every ayah
// disagrees". Comparing against a missing map entry yields an empty string,
// which differs from every real text and would report 111 bogus differences
// while looking like a corrupt corpus.
func TestVerifyMarksCrossCheckSkippedWhenSourcesUnavailable(t *testing.T) {
	if testing.Short() {
		t.Skip("requires the network")
	}

	old := httpClient
	// Only the secondary sources are blocked. The primary is the corpus of
	// record, so verify is entitled to fail hard when it is unreachable.
	httpClient = &http.Client{Timeout: 10 * time.Second,
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			if strings.Contains(r.URL.Host, "quran.com") {
				return old.Do(r)
			}
			return nil, errors.New("cross-check source is unreachable")
		})}
	t.Cleanup(func() { httpClient = old })

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	client, err := mongo.Connect(ctx, options.Client().
		ApplyURI("mongodb://tadbor:tadbor_dev_password@localhost:27017"))
	if err != nil {
		t.Skipf("mongo unavailable: %v", err)
	}
	defer client.Disconnect(context.Background())

	verdicts, _, err := verify(ctx, client.Database("tadbor"), 12)
	if err != nil {
		if strings.Contains(err.Error(), "fetch primary source") {
			t.Skipf("primary source unreachable, cannot exercise the path: %v", err)
		}
		t.Fatalf("verify: %v", err)
	}
	if want := quran.AyahCount(12); len(verdicts) != want {
		t.Fatalf("got %d verdicts, want %d", len(verdicts), want)
	}
	for _, v := range verdicts {
		if !v.crossSkipped {
			t.Fatalf("ayah %d was compared with no cross-source reachable", v.number)
		}
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
