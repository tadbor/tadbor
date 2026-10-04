package generation

import (
	"strings"
	"testing"
)

// Valid output for a given style, so each case below differs from a passing
// draft in exactly one way.
func req(style string) Request {
	return Request{
		Task:           "simplify",
		TargetLanguage: "ar",
		TargetStyle:    style,
		Evidence:       []Evidence{{ID: "c1", Text: "…"}},
	}
}

func goodArabic() *GenerationOutput {
	return &GenerationOutput{
		Content:        "أن يوسف ابن يعقوب، وألقاه إخوته في بئر، وحزن يعقوب عليه.",
		SourceRefs:     []string{"c1"},
		Claims:         []string{"يوسف ابن يعقوب"},
		Warnings:       []string{},
		Confidence:     "medium",
		RequiresReview: true,
	}
}

func goodEnglish() *GenerationOutput {
	return &GenerationOutput{
		Content:        "Yusuf was the son of Jacob, whom his brothers cast into a well.",
		SourceRefs:     []string{"c1"},
		Claims:         []string{"Yusuf was the son of Jacob"},
		Warnings:       []string{},
		Confidence:     "medium",
		RequiresReview: true,
	}
}

// Every case here is a draft that must never reach a human reviewer as if it
// were sound. The ones that matter most are the last two: a draft in the wrong
// script and a draft that loops both look completely plausible to a reviewer
// skimming, which is the whole reason this gate exists.
func TestValidateRejects(t *testing.T) {
	cases := []struct {
		name string
		req  Request
		out  *GenerationOutput
		want string
	}{
		{
			name: "empty content",
			req:  req("simplified_ar"),
			out:  &GenerationOutput{Content: "", RequiresReview: true},
			want: "empty content",
		},
		{
			name: "claims with no source_refs",
			req:  req("simplified_ar"),
			out:  &GenerationOutput{Content: "يوسف ابن يعقوب.", Claims: []string{"a"}, RequiresReview: true},
			want: "citation coverage",
		},
		{
			name: "requires_review forced false",
			req:  req("simplified_ar"),
			out:  &GenerationOutput{Content: "يوسف ابن يعقوب.", SourceRefs: []string{"c1"}, RequiresReview: false},
			want: "requires_review",
		},
		{
			name: "Arabic requested, English produced",
			req:  req("simplified_ar"),
			out:  goodEnglish(),
			want: "no Arabic letters",
		},
		{
			name: "English requested, Arabic produced",
			req:  req("en"),
			out:  goodArabic(),
			want: "no Latin letters",
		},
		{
			name: "Arabic requested, mostly Latin produced",
			req:  req("egyptian_ar"),
			out: &GenerationOutput{
				// The Quranic name in Arabic, dropped into an otherwise Latin
				// sentence: enough to pass a bare "contains Arabic" check,
				// which is the obvious wrong implementation of this rule.
				Content:        "Yusuf was the son of Jacob, and his brothers threw him into a well, and Jacob grieved greatly over what had happened to يوسف.",
				SourceRefs:     []string{"c1"},
				RequiresReview: true,
			},
			want: "mostly Latin",
		},
		{
			name: "runaway length",
			req:  req("simplified_ar"),
			out: &GenerationOutput{
				Content:        strings.Repeat("يوسف ابن يعقوب. ", 300),
				SourceRefs:     []string{"c1"},
				RequiresReview: true,
			},
			want: "over the",
		},
		{
			name: "style with no script rule",
			req:  req("fr"),
			out:  goodEnglish(),
			want: "no script rule",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := Validate(tc.req, tc.out)
			if err == nil {
				t.Fatalf("Validate accepted a draft it must reject (%s)", tc.name)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error = %q, want it to mention %q", err, tc.want)
			}
		})
	}
}

func TestValidateAccepts(t *testing.T) {
	cases := []struct {
		name string
		req  Request
		out  *GenerationOutput
	}{
		{"arabic for simplified_ar", req("simplified_ar"), goodArabic()},
		{"arabic for egyptian_ar", req("egyptian_ar"), goodArabic()},
		{"english for en", req("en"), goodEnglish()},
		{
			name: "digits and punctuation are not a second language",
			req:  req("en"),
			out: &GenerationOutput{
				// Latin digits and quotes must not count as Latin letters, and
				// Quranic annotation must not count as Arabic letters — either
				// would flip the comparison on a perfectly good draft.
				Content:        `Yusuf (يوسف) was the son of Jacob — 12:1, "quoted".`,
				SourceRefs:     []string{"c1"},
				RequiresReview: true,
			},
		},
		{
			name: "a Latin name inside Arabic is not a language switch",
			req:  req("simplified_ar"),
			out: &GenerationOutput{
				Content:        "ورد اسم يوسف في سفر التكوين، ويسمونه يوسف بن يعقوب.",
				SourceRefs:     []string{"c1"},
				RequiresReview: true,
			},
		},
		{
			name: "no claims and no refs is not a coverage failure",
			req:  req("simplified_ar"),
			out: &GenerationOutput{
				// Coverage is only violated when claims exist to be uncovered.
				// An empty claims array is an under-specified draft, which is
				// §15's problem to describe, not this gate's to reject.
				Content:        "يوسف ابن يعقوب.",
				SourceRefs:     []string{"c1"},
				Claims:         []string{},
				RequiresReview: true,
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := Validate(tc.req, tc.out); err != nil {
				t.Errorf("Validate rejected a valid draft: %v", err)
			}
		})
	}
}

// The bound exists to catch looping, so it has to be tight enough to catch a
// loop and loose enough not to reject a long legitimate explanation.
func TestLengthBoundIsInTheRightPlace(t *testing.T) {
	long := strings.Repeat("يوسف ", 390) // 1950 runes, under the 2000 limit
	if err := Validate(req("simplified_ar"), &GenerationOutput{
		Content: long, SourceRefs: []string{"c1"}, RequiresReview: true,
	}); err != nil {
		t.Errorf("a %d-rune explanation was rejected, which is not what the limit is for: %v", len([]rune(long)), err)
	}

	over := strings.Repeat("يوسف ", maxContentRunes/5+1)
	if err := Validate(req("simplified_ar"), &GenerationOutput{
		Content: over, SourceRefs: []string{"c1"}, RequiresReview: true,
	}); err == nil {
		t.Errorf("a %d-rune draft was accepted, over the %d limit", len([]rune(over)), maxContentRunes)
	}
}
