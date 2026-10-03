package ingestion

import (
	"strings"
	"unicode"
)

// Normalizer produces the lexical form of a passage used for future exact
// matching. It never touches the stored original text.
//
// Part 1 §7 is explicit that Arabic normalization must be reversible and
// auditable: the original cleaned text is always kept alongside whatever
// normalized form is derived, and the embedded vector is computed from the
// original — diacritics are meaning-bearing in Quranic quotations inside
// commentary, so stripping them before embedding would quietly distort meaning.
type Normalizer struct{}

// NewNormalizer returns the default normalizer.
func NewNormalizer() *Normalizer { return &Normalizer{} }

// diacriticRanges are the Arabic combining marks that carry no lexical value
// for matching purposes: harakat (fatha/damma/kasra/tanween), shadda, sukun,
// superscript alef, and tatweel. Quranic annotation signs (U+06D6–U+06ED) are
// included because they are recitation marks layered over the same text.
var diacriticRanges = [...][2]rune{
	{0x0610, 0x061A}, // Arabic signs (honorifics etc.)
	{0x064B, 0x065F}, // harakat, shadda, sukun
	{0x0670, 0x0670}, // superscript alef
	{0x06D6, 0x06ED}, // Quranic annotation signs
	{0x0640, 0x0640}, // tatweel
}

// LexicalForm strips diacritics and tatweel, collapses whitespace, and trims.
// Letters, harakat-free glyph shapes, and digits are preserved exactly; the
// result is only ever used as a matching key.
func (n *Normalizer) LexicalForm(s string) string {
	stripped := strings.Map(func(r rune) rune {
		for _, rg := range diacriticRanges {
			if r >= rg[0] && r <= rg[1] {
				return -1
			}
		}
		return r
	}, s)

	var b strings.Builder
	b.Grow(len(stripped))
	space := false
	for _, r := range stripped {
		if unicode.IsSpace(r) {
			space = true
			continue
		}
		if space && b.Len() > 0 {
			b.WriteByte(' ')
		}
		space = false
		b.WriteRune(r)
	}
	return b.String()
}

// HasDiacritics reports whether a passage contains marks that LexicalForm would
// strip — useful for auditing a corpus that unexpectedly contains none.
func (n *Normalizer) HasDiacritics(s string) bool {
	for _, r := range s {
		for _, rg := range diacriticRanges {
			if r >= rg[0] && r <= rg[1] {
				return true
			}
		}
	}
	return false
}

// splitParagraphs breaks a section into paragraphs on blank lines. Headings,
// footnotes, and page markers stay attached to the paragraph that contains them
// — Part 1 §7 treats them as structure, not noise, and this deliberately does no
// more aggressive cleaning than that.
func splitParagraphs(s string) []string {
	lines := strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n")
	var (
		paras []string
		cur   []string
	)
	flush := func() {
		p := strings.TrimSpace(strings.Join(cur, "\n"))
		cur = cur[:0]
		if p != "" {
			paras = append(paras, p)
		}
	}
	for _, ln := range lines {
		if strings.TrimSpace(ln) == "" {
			flush()
			continue
		}
		cur = append(cur, ln)
	}
	flush()
	return paras
}

// splitLongParagraph breaks an oversized paragraph on whitespace, preferring the
// latest break that still fits. Only reached when a single paragraph exceeds the
// chunk size cap, which should be rare in verse-anchored text.
func splitLongParagraph(p string, maxRunes int) []string {
	if utf8RuneCount(p) <= maxRunes {
		return []string{p}
	}
	var (
		out     []string
		cur     strings.Builder
		curRune int
	)
	for _, word := range strings.Fields(p) {
		wc := utf8RuneCount(word)
		if curRune > 0 && curRune+1+wc > maxRunes {
			out = append(out, strings.TrimSpace(cur.String()))
			cur.Reset()
			curRune = 0
		}
		if curRune > 0 {
			cur.WriteByte(' ')
			curRune++
		}
		cur.WriteString(word)
		curRune += wc
	}
	if s := strings.TrimSpace(cur.String()); s != "" {
		out = append(out, s)
	}
	return out
}

// utf8RuneCount counts runes; chunk sizing is in runes, not bytes, so that
// Arabic passages are not measured at roughly half their real size.
func utf8RuneCount(s string) int { return len([]rune(s)) }
