package generation

import (
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

// maxContentRunes is the length ceiling for one explanation.
//
// Set deliberately loose. A legitimate Simplified Arabic explanation of a
// single ayah is a few hundred words, so this is not a style limit — it is
// aimed at one specific failure: a model that has stopped making progress and
// is now looping, which produces a perfectly fluent explanation of anything
// up to whatever the context window allows. That failure is dangerous here
// because it looks like a real answer to a human skimming it. A limit tight
// enough to catch it does not need to be tight enough to catch padding.
const maxContentRunes = 2000

// Validate runs the deterministic checks from Part 1 §19 before an explanation
// is eligible for human review. This is not a substitute for human review —
// it's a cheap gate that catches obvious failures first.
//
// It takes the request as well as the output because three of the checks are
// about the relationship between them: content in the wrong script is only
// wrong relative to what was asked for.
//
// Two of §19's five checks are not here and cannot be: Quran text integrity
// and verse-mapping validation need the canonical corpus, and source
// validation needs the source registry. All three are database lookups, and
// this package deliberately takes no database handle — so those run in the
// batch flow (issue #10), which is where the chunks and verses are in hand.
// What is here is everything decidable from the request and the draft alone.
func Validate(req Request, out *GenerationOutput) error {
	if out.Content == "" {
		return errors.New("empty content")
	}
	if n := utf8.RuneCountInString(out.Content); n > maxContentRunes {
		return fmt.Errorf("content is %d runes, over the %d limit — usually a model that has stopped and started repeating", n, maxContentRunes)
	}
	if len(out.Claims) > 0 && len(out.SourceRefs) == 0 {
		return errors.New("claims present with no source_refs — citation coverage check failed")
	}
	if !out.RequiresReview {
		return errors.New("requires_review must always be true")
	}
	return checkScript(req.TargetStyle, out.Content)
}

// checkScript implements §19's "output is actually in the requested target
// language" by counting letter scripts. It is not language identification and
// does not pretend to be: a pass means the text is written in the right
// script, not the right dialect.
//
// It cannot tell Simplified from Egyptian Arabic, because they share a script,
// and §17 is explicit that dialect fidelity is a human checklist and not an
// automated similarity score — "it is said that" quietly becoming a flat
// assertion is the failure worth catching, and character n-grams do not catch
// it. Writing a dialect detector here would reintroduce, in a worse form, the
// thing §17 rules out, so this stops at the script and says so.
func checkScript(style, content string) error {
	arabic, latin := countScripts(content)

	if strings.HasSuffix(style, "_ar") {
		if arabic == 0 {
			return fmt.Errorf("style %q asked for Arabic and the content has no Arabic letters in it", style)
		}
		if arabic < latin {
			return fmt.Errorf("style %q asked for Arabic but the content is mostly Latin script: %d Arabic letters, %d Latin", style, arabic, latin)
		}
		return nil
	}

	if style == "en" {
		if latin == 0 {
			return fmt.Errorf("style \"en\" asked for English and the content has no Latin letters in it")
		}
		if latin < arabic {
			return fmt.Errorf("style \"en\" asked for English but the content is mostly Arabic script: %d Latin letters, %d Arabic", latin, arabic)
		}
		return nil
	}

	// An unrecognised style fails loudly instead of skipping. A style added
	// later without a rule here would otherwise put unvalidated text in front
	// of a reviewer who believes it was validated.
	return fmt.Errorf("no script rule for target style %q — add one before generating in that style", style)
}

// countScripts counts letters by script and ignores everything else. Punctuation,
// whitespace, digits and Quranic annotation marks are not evidence of a
// language either way: Arabic explanations legitimately contain Latin digits,
// and counting them as Latin letters would fail valid output.
func countScripts(s string) (arabic, latin int) {
	for _, r := range s {
		switch {
		case unicode.Is(unicode.Arabic, r):
			arabic++
		case r < unicode.MaxASCII && unicode.IsLetter(r):
			latin++
		}
	}
	return arabic, latin
}
