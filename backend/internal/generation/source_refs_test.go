package generation

import (
	"net/http"
	"strings"
	"testing"
)

// The model is shown the evidence as "[1] ...", "[2] ..." and answers with
// those numbers. What reaches the database must be chunk ids: the reviewer
// dashboard resolves source_refs against tafsir_chunks, and Part 1 §15 requires
// citations to be stored rather than reconstructed. A ref that survives as "1"
// is a citation to nothing that still looks like a citation.
func TestGenerateResolvesSourceRefsToChunkIDs(t *testing.T) {
	f := newFakeProvider(t, []int{http.StatusOK}, []string{okBody(t,
		`{"content":"c","source_refs":["1","2"],"claims":["cl"],"warnings":[],"confidence":"medium"}`)})

	out, err := newTestService(f.srv.URL, "k").Generate(testRequest())
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	want := []string{"chunk-one", "chunk-two"}
	if len(out.SourceRefs) != len(want) {
		t.Fatalf("SourceRefs = %v, want %v", out.SourceRefs, want)
	}
	for i, ref := range want {
		if out.SourceRefs[i] != ref {
			t.Errorf("SourceRefs[%d] = %q, want %q", i, out.SourceRefs[i], ref)
		}
	}
	if len(out.Warnings) != 0 {
		t.Errorf("valid refs produced warnings: %v", out.Warnings)
	}
}

// Citing evidence that was never sent is the one case that must not pass
// through: it would put a dangling id in a published explanation. It becomes a
// warning instead so the reviewer sees the model overreaching.
func TestGenerateRefusesRefsItCannotResolve(t *testing.T) {
	for _, ref := range []string{"3", "0", "-1", "chunk-three", "s1"} {
		t.Run(ref, func(t *testing.T) {
			f := newFakeProvider(t, []int{http.StatusOK}, []string{okBody(t,
				`{"content":"c","source_refs":["`+ref+`"],"claims":["cl"],"warnings":[],"confidence":"low"}`)})

			out, err := newTestService(f.srv.URL, "k").Generate(testRequest())
			if err != nil {
				t.Fatalf("Generate: %v", err)
			}
			if len(out.SourceRefs) != 0 {
				t.Errorf("SourceRefs = %v, want none — %q is not evidence that was sent", out.SourceRefs, ref)
			}
			if len(out.Warnings) == 0 || !strings.Contains(out.Warnings[0], "not provided") {
				t.Errorf("Warnings = %v, want one saying %q was not provided", out.Warnings, ref)
			}
		})
	}
}

// A ref the model invents must not be laundered into coverage: with the ref
// gone, the claim it supported is ungrounded and Validate has to say so.
func TestValidateFailsWhenOnlyClaimLosesItsRef(t *testing.T) {
	f := newFakeProvider(t, []int{http.StatusOK}, []string{okBody(t,
		`{"content":"c","source_refs":["9"],"claims":["a claim"],"warnings":[],"confidence":"low"}`)})

	out, err := newTestService(f.srv.URL, "k").Generate(testRequest())
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if err := Validate(out); err == nil {
		t.Error("Validate accepted a claim whose only citation did not resolve")
	}
}
