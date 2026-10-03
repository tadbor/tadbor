package ingestion

import "testing"

func TestLexicalFormStripsDiacriticsAndTatweel(t *testing.T) {
	n := NewNormalizer()

	for _, tc := range []struct {
		name string
		in   string
		want string
	}{
		{"harakat", "الْحَمْدُ", "الحمد"},
		{"shadda and sukun", "يَقُولُونَ", "يقولون"},
		{"superscript alef", "الرَّحْمَٰن", "الرحمن"},
		{"tatweel", "مصــــر", "مصر"},
		// U+0671 (alef wasla) is a letter, not a combining mark, so it stays.
		{"quranic annotation", "ٱلرَّحْمَٰن", "ٱلرحمن"},
		{"no diacritics unchanged", "الحمد لله", "الحمد لله"},
		{"digits preserved", "الفاتحة ١٢٣", "الفاتحة ١٢٣"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := n.LexicalForm(tc.in); got != tc.want {
				t.Fatalf("LexicalForm(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestLexicalFormIsIdempotentAndStable(t *testing.T) {
	n := NewNormalizer()
	original := "تَقْرَأُ الْقُرْآنَ ٱلرَّحْمَٰن"

	once := n.LexicalForm(original)
	twice := n.LexicalForm(once)

	if once != twice {
		t.Fatalf("LexicalForm is not idempotent: %q then %q", once, twice)
	}
	if n.HasDiacritics(once) {
		t.Fatalf("lexical form still reports diacritics: %q", once)
	}
}

// The stored original is the only thing embedded, so it must survive
// normalization untouched — including Quranic quotations inside commentary,
// where diacritics carry meaning (Part 1 §7).
func TestNormalizationNeverMutatesTheTextItIsGiven(t *testing.T) {
	original := "قال المفسر: ﴿إِنَّا أَنزَلْنَاهُ قُرْآنًا عَرَبِيًّا﴾ ثم فسره"

	before := NewNormalizer().LexicalForm(original)
	if before != "قال المفسر: ﴿إنا أنزلناه قرآنا عربيا﴾ ثم فسره" {
		t.Fatalf("unexpected lexical form: %q", before)
	}
	if original != "قال المفسر: ﴿إِنَّا أَنزَلْنَاهُ قُرْآنًا عَرَبِيًّا﴾ ثم فسره" {
		t.Fatal("original text was mutated")
	}
}

func TestLexicalFormCollapsesWhitespace(t *testing.T) {
	n := NewNormalizer()
	for _, tc := range []struct{ in, want string }{
		{"  نص  ", "نص"},
		{"سطر\n\nآخر", "سطر آخر"},
		{"تباعد\tمزدوج", "تباعد مزدوج"},
		{"\n\n", ""},
	} {
		if got := n.LexicalForm(tc.in); got != tc.want {
			t.Fatalf("LexicalForm(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestHasDiacritics(t *testing.T) {
	n := NewNormalizer()
	if !n.HasDiacritics("مُحَمَّد") {
		t.Fatal("expected diacritics to be detected")
	}
	if n.HasDiacritics("محمد") {
		t.Fatal("did not expect diacritics")
	}
}

func TestSplitParagraphs(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   string
		want []string
	}{
		{"blank line separated", "أ\n\nب", []string{"أ", "ب"}},
		{"single line kept together", "أ\nب", []string{"أ\nب"}},
		{"crlf", "أ\r\n\r\nب", []string{"أ", "ب"}},
		{"blank runs collapse", "أ\n\n\n\nب", []string{"أ", "ب"}},
		{"empty", "   \n  ", nil},
		{"footnotes stay with their paragraph", "فقرة\n(1) حاشية\n\nفقرة ثانية", []string{"فقرة\n(1) حاشية", "فقرة ثانية"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := splitParagraphs(tc.in)
			if len(got) != len(tc.want) {
				t.Fatalf("got %d paragraphs %q, want %d %q", len(got), got, len(tc.want), tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("paragraph %d = %q, want %q", i, got[i], tc.want[i])
				}
			}
		})
	}
}

func TestSplitLongParagraphStaysUnderTheCap(t *testing.T) {
	const cap = 20
	words := []string{}
	for i := 0; i < 12; i++ {
		words = append(words, "كلمة")
	}

	pieces := splitLongParagraph(joinWords(words), cap)
	if len(pieces) < 2 {
		t.Fatalf("expected the paragraph to be split, got %d piece(s)", len(pieces))
	}
	for i, p := range pieces {
		if got := utf8RuneCount(p); got > cap {
			t.Fatalf("piece %d is %d runes, over the %d cap: %q", i, got, cap, p)
		}
		if p == "" {
			t.Fatalf("piece %d is empty", i)
		}
	}

	if got := splitLongParagraph("قصير", cap); len(got) != 1 || got[0] != "قصير" {
		t.Fatalf("a short paragraph must stay whole, got %q", got)
	}
}

func TestSplitLongParagraphMeasuresRunesNotBytes(t *testing.T) {
	// 30 Arabic runes are ~60 bytes. A byte-based cap would wrongly split this.
	const cap = 40
	text := ""
	for i := 0; i < 30; i++ {
		text += "ط"
	}
	if got := splitLongParagraph(text, cap); len(got) != 1 {
		t.Fatalf("expected one piece, got %d", len(got))
	}
}

func joinWords(words []string) string {
	out := ""
	for i, w := range words {
		if i > 0 {
			out += " "
		}
		out += w
	}
	return out
}
