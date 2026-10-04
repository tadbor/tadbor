// Package textverify decides how well a passage of commentary corroborates the
// ayah it is mapped to.
//
// The signals here are evidence, not proof. A tafsir passage that quotes its
// ayah at length is far more likely to be faithful and correctly keyed than one
// that paraphrases it, because a long verbatim quote is checkable by a human
// against a printed edition. That is what these functions measure, so a reviewer
// knows which entries need their eyes rather than a machine's blessing.
//
// There is deliberately no single "is this right" verdict here. Paraphrase is a
// real and expected feature of tafsir, so a low score means "read this one", not
// "this one is wrong".
package textverify

import (
	"strings"

	"tadbor/backend/internal/ingestion"
)

// ShingleSize is the number of runes compared at once when matching quotations.
const ShingleSize = 8

// QuotedThreshold is the share of a verse's shingles that must appear in a passage
// before the mapping counts as corroborated by quotation.
//
// Half is a plain "mostly quoted" reading rather than a value tuned to make a
// review queue look short. On Surah Yusuf the median entry scores 0.59, and the
// entries below 0.2 are the ones that genuinely paraphrase instead of quoting.
const QuotedThreshold = 0.5

// VerseIntroducingPhrases are the stock openings Ibn Kathir uses before expounding
// a verse ("He informs, may He be exalted, that..."). They are a second,
// independent signal that a passage is expounding the verse it is keyed to.
//
// They cover only part of the corpus — 36 of surah Yusuf's 111 entries — because
// the phrasing varies with context. That is why they corroborate the quotation
// check instead of replacing it.
var VerseIntroducingPhrases = []string{
	"يخبر تعالى",
	"يقول تعالى",
	"قال تعالى",
}

// Score is the evidence one passage offers for being correctly keyed to its ayah.
type Score struct {
	// LongestRun is the length in words of the longest consecutive run of the
	// ayah's words that also occurs in the passage, after match-form folding.
	LongestRun int
	// QuoteCoverage is the fraction of the ayah's ShingleSize shingles that occur
	// in the passage, in [0, 1].
	QuoteCoverage float64
	// Quoted reports whether QuoteCoverage reaches QuotedThreshold.
	Quoted bool
	// Introduced reports whether the passage contains a stock verse-introducing
	// phrase.
	Introduced bool
}

// Corroborated reports whether any signal fired: a quoted verse, or an explicit
// announcement that the passage is about to expound it.
func (s Score) Corroborated() bool { return s.Quoted || s.Introduced }

// ScorePassage measures how strongly a passage corroborates its ayah.
func ScorePassage(verse, passage string) Score {
	cov := QuoteCoverage(verse, passage, ShingleSize)
	return Score{
		LongestRun:    LongestQuotedRun(verse, passage),
		QuoteCoverage: cov,
		Quoted:        cov >= QuotedThreshold,
		Introduced:    HasVerseIntroduction(passage),
	}
}

// LongestQuotedRun returns the length in words of the longest consecutive run of
// the verse's words that also occurs in the passage.
//
// It requires the quotation's words to match exactly, and Ibn Kathir's do not:
// he writes "إنا أنزلناه قرآنا عربيا" where the ayah reads "إنا أنزلناه قرآنا عربيا"
// with its Uthmani spellings, so inserted or dropped alifs and hamza seats break
// every exact word match. That is why QuoteCoverage is needed alongside this.
func LongestQuotedRun(verse, passage string) int {
	verse = MatchForm(verse)
	passage = MatchForm(passage)

	words := strings.Fields(verse)
	if len(words) == 0 || passage == "" {
		return 0
	}
	best := 0
	for i := range words {
		for j := i + 1; j <= len(words); j++ {
			if !strings.Contains(passage, strings.Join(words[i:j], " ")) {
				break
			}
			if j-i > best {
				best = j - i
			}
		}
	}
	return best
}

// QuoteCoverage returns the fraction of the verse's k-rune shingles that also occur
// in the passage.
//
// Unlike LongestQuotedRun it tolerates the orthographic differences between
// Uthmani verse text and the mufassir's own spelling, so a passage that quotes a
// verse nearly verbatim still scores highly; it collapses when the passage merely
// paraphrases.
func QuoteCoverage(verse, passage string, k int) float64 {
	v := []rune(MatchForm(verse))
	p := []rune(MatchForm(passage))
	if len(p) == 0 || len(v) == 0 {
		return 0
	}
	if len(v) < k {
		if strings.Contains(passage, verse) {
			return 1
		}
		return 0
	}

	passageShingles := make(map[string]struct{}, len(p))
	for i := 0; i+k <= len(p); i++ {
		passageShingles[string(p[i:i+k])] = struct{}{}
	}

	total := len(v) - k + 1
	if total <= 0 {
		return 0
	}
	hit := 0
	for i := 0; i+k <= len(v); i++ {
		if _, ok := passageShingles[string(v[i:i+k])]; ok {
			hit++
		}
	}
	return float64(hit) / float64(total)
}

// HasVerseIntroduction reports whether the passage contains one of the stock
// verse-introducing phrases.
func HasVerseIntroduction(passage string) bool {
	flat := MatchForm(passage)
	for _, p := range VerseIntroducingPhrases {
		if strings.Contains(flat, MatchForm(p)) {
			return true
		}
	}
	return false
}

// MatchForm reduces text for quotation matching only.
//
// It layers orthography folding on top of LexicalForm because the two sides of
// this comparison are written by different hands: ayahs are Uthmani, while the
// mufassir quotes them in his own orthography. In practice Qur'anic quotations
// appear without hamza ("اباه" for "أَبَاهُ") and with final ya and ta-marbuta
// spelled plainly, so a diacritics-only comparison scores zero for a mapping that
// is plainly correct — and scores it zero silently.
//
// This is a matching key and nothing else. LexicalForm stays reversible and stored
// text is never folded: hamza and ya/alef-maqsura are meaning-bearing in Qur'anic
// text, so this must never reach chunk text, an embedding, or anything a reader
// sees.
func MatchForm(s string) string {
	folded := strings.Map(func(r rune) rune {
		switch r {
		case 'أ', 'إ', 'آ', 'ٱ': // أ إ آ ٱ
			return 'ا'
		case 'ؤ': // ؤ
			return 'و'
		case 'ئ': // ئ
			return 'ي'
		case 'ى': // ى
			return 'ي'
		case 'ة': // ة
			return 'ه'
		case 'ء': // ء
			return -1
		case 0x0654: // ؔ combining hamza above
			return -1
		}
		return r
	}, ingestion.NewNormalizer().LexicalForm(s))
	return strings.TrimSpace(folded)
}
