package quran

import "testing"

func TestAyahCountsCoverTheWholeQuran(t *testing.T) {
	if got := len(AyahCounts); got != 114 {
		t.Fatalf("got %d surahs, want 114", got)
	}

	total := 0
	for i, n := range AyahCounts {
		if n <= 0 {
			t.Errorf("surah %d has a non-positive ayah count %d", i+1, n)
		}
		total += n
	}
	if total != TotalAyahs {
		t.Errorf("ayah counts sum to %d, want %d", total, TotalAyahs)
	}

	// Spot values that other packages and tools rely on.
	for surah, want := range map[int]int{1: 7, 2: 286, 9: 129, 12: 111, 18: 110, 114: 6} {
		if got := AyahCount(surah); got != want {
			t.Errorf("AyahCount(%d) = %d, want %d", surah, got, want)
		}
	}
}

func TestAyahCountRejectsSurahsOutsideTheQuran(t *testing.T) {
	for _, surah := range []int{0, -1, 115, 1000} {
		if got := AyahCount(surah); got != 0 {
			t.Errorf("AyahCount(%d) = %d, want 0", surah, got)
		}
	}
}

// AyahOffset locates a surah inside a flat file of all 6236 ayahs. Tanzil's
// Uthmani export is laid out this way, and offset 1596 is where surah 12 was
// confirmed to start, so a wrong offset would silently read the wrong verses.
func TestAyahOffsetLocatesSurahInAFlatFile(t *testing.T) {
	for surah, want := range map[int]int{1: 0, 2: 7, 3: 293, 12: 1596, 114: 6230} {
		if got := AyahOffset(surah); got != want {
			t.Errorf("AyahOffset(%d) = %d, want %d", surah, got, want)
		}
	}

	// Every surah's range must sit inside the file and must not overlap the next.
	for surah := 1; surah <= 114; surah++ {
		start := AyahOffset(surah)
		end := start + AyahCount(surah)
		if start < 0 || end > TotalAyahs {
			t.Fatalf("surah %d spans %d..%d, outside 0..%d", surah, start, end, TotalAyahs)
		}
		if surah < 114 && end != AyahOffset(surah+1) {
			t.Fatalf("surah %d ends at %d but surah %d starts at %d", surah, end, surah+1, AyahOffset(surah+1))
		}
	}
}
