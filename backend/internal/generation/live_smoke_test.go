package generation

import (
	"os"
	"testing"
)

func TestLiveSmoke(t *testing.T) {
	if os.Getenv("LIVE_SMOKE") != "1" {
		t.Skip("set LIVE_SMOKE=1 to call the real provider")
	}
	out, err := NewService().Generate(Request{
		Task:           "simplify",
		TargetLanguage: "ar",
		TargetStyle:    "simplified_ar",
		Evidence: []Evidence{
			{ID: "tabari-surah-12-1", Text: "Al-Tabari reports that Yusuf was the son of Jacob, and that his brothers cast him into a well."},
			{ID: "tabari-surah-12-2", Text: "Al-Tabari records that Jacob was deeply grieved by what happened to his son."},
		},
	})
	if err != nil {
		t.Fatalf("live call: %v", err)
	}
	t.Logf("content: %s", out.Content)
	t.Logf("refs=%v (must be chunk ids, not numbers) claims=%d warnings=%v confidence=%q requiresReview=%v",
		out.SourceRefs, len(out.Claims), out.Warnings, out.Confidence, out.RequiresReview)
	if err := Validate(out); err != nil {
		t.Errorf("Validate rejected the draft: %v", err)
	}
}
