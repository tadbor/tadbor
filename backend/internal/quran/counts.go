package quran

// AyahCounts is the number of ayahs in each surah, indexed by surah number
// minus one: index 0 is Al-Fatihah (7 ayahs), index 11 is Yusuf (111).
//
// It is derived from the same source of record as the corpus itself and is
// treated as an invariant, not as data to be edited by hand. A transcription
// error here silently corrupts every offset computed from it, which is exactly
// how an earlier hand-typed copy came to sum to 6190 instead of 6236. Keep the
// totals pinned by the test in ayah_counts_test.go.
//
// The offsets into a flat, concatenated ayah file (Tanzil's export, for one)
// are the sum of the counts of every preceding surah, which is why surah 12
// starts at line 1596.
var AyahCounts = []int{
	7, 286, 200, 176, 120, 165,
	206, 75, 129, 109, 123, 111,
	43, 52, 99, 128, 111, 110,
	98, 135, 112, 78, 118, 64,
	77, 227, 93, 88, 69, 60,
	34, 30, 73, 54, 45, 83,
	182, 88, 75, 85, 54, 53,
	89, 59, 37, 35, 38, 29,
	18, 45, 60, 49, 62, 55,
	78, 96, 29, 22, 24, 13,
	14, 11, 11, 18, 12, 12,
	30, 52, 52, 44, 28, 28,
	20, 56, 40, 31, 50, 40,
	46, 42, 29, 19, 36, 25,
	22, 17, 19, 26, 30, 20,
	15, 21, 11, 8, 8, 19,
	5, 8, 8, 11, 11, 8,
	3, 9, 5, 4, 7, 3,
	6, 3, 5, 4, 5, 6,
}

// TotalAyahs is the number of ayahs in the whole Quran.
const TotalAyahs = 6236

// AyahCount returns how many ayahs surah has.
//
// It is deliberately strict: a surah outside 1..114 or a zero count is a
// programming error, and returning a wrong count would be indistinguishable
// from real data at the call site.
func AyahCount(surah int) int {
	if surah < 1 || surah > len(AyahCounts) {
		return 0
	}
	return AyahCounts[surah-1]
}

// AyahOffset returns the zero-based line/ayah offset at which surah starts
// within a flat file holding every ayah of the Quran in surah order.
func AyahOffset(surah int) int {
	offset := 0
	for i := 0; i < surah-1 && i < len(AyahCounts); i++ {
		offset += AyahCounts[i]
	}
	return offset
}
