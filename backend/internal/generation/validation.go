package generation

import "errors"

// Validate runs the deterministic checks from Part 2 §19 before an
// explanation is eligible for human review. This is not a substitute for
// human review — it's a cheap gate that catches obvious failures first.
func Validate(out *GenerationOutput) error {
	if out.Content == "" {
		return errors.New("empty content")
	}
	if len(out.Claims) > 0 && len(out.SourceRefs) == 0 {
		return errors.New("claims present with no source_refs — citation coverage check failed")
	}
	if !out.RequiresReview {
		return errors.New("requires_review must always be true")
	}
	return nil
}
