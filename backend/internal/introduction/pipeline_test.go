package introduction

import (
	"context"
	"errors"
	"testing"

	"tadbor/backend/internal/content"
	"tadbor/backend/internal/generation"
)

type fakeEvidenceSource struct {
	evidence []generation.Evidence
	err      error
	surahID  int
}

func (f *fakeEvidenceSource) LoadEvidence(_ context.Context, surahID int) ([]generation.Evidence, error) {
	f.surahID = surahID
	return f.evidence, f.err
}

type fakeGenerator struct {
	output  *generation.GenerationOutput
	err     error
	request generation.Request
}

func (f *fakeGenerator) GenerateIntroduction(_ context.Context, request generation.Request) (*generation.GenerationOutput, error) {
	f.request = request
	return f.output, f.err
}

type fakeDraftStore struct {
	drafts []content.SurahIntroduction
	err    error
}

func (f *fakeDraftStore) SavePending(_ context.Context, draft content.SurahIntroduction) error {
	if draft.ReviewStatus != "pending" {
		return errors.New("refusing non-pending test write")
	}
	f.drafts = append(f.drafts, draft)
	return f.err
}

func verifiedEvidence() []generation.Evidence {
	return []generation.Evidence{{
		ID: "chunk-a", Text: "مصدر موثق عن سورة يوسف", SourceID: "ibn-kathir-ar", SourceVersion: "snapshot-1",
	}}
}

func validOutput() *generation.GenerationOutput {
	return &generation.GenerationOutput{
		RequiresReview: true,
		Sections: []generation.IntroductionSection{{
			Type: "themes", Title: "موضوعات السورة",
			Claims: []generation.IntroductionClaim{{Text: "المعنى المدعوم بالمصدر", SourceRefs: []string{"1"}}},
		}},
	}
}

func TestPipelineRunPersistsTypedPendingDraftWithResolvedRefs(t *testing.T) {
	evidence := &fakeEvidenceSource{evidence: verifiedEvidence()}
	generator := &fakeGenerator{output: validOutput()}
	store := &fakeDraftStore{}
	pipeline := NewPipeline(evidence, generator, store)

	draft, err := pipeline.Run(context.Background(), 12, "simplified_ar", "beginner")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if evidence.surahID != 12 || generator.request.Task != "introduce" {
		t.Fatalf("workflow request = surah %d, task %q", evidence.surahID, generator.request.Task)
	}
	if len(store.drafts) != 1 || store.drafts[0].ReviewStatus != "pending" {
		t.Fatalf("persisted drafts = %+v, want exactly one pending draft", store.drafts)
	}
	if draft.ID != "surah-12-simplified_ar" {
		t.Fatalf("draft id = %q, want stable Mongo id", draft.ID)
	}
	if len(draft.Sections) != 1 || draft.Sections[0].Claims[0].SourceRefs[0] != "chunk-a" {
		t.Fatalf("draft sections = %+v, want typed claim with resolved chunk id", draft.Sections)
	}
	if len(draft.SourceRefs) != 1 || draft.SourceRefs[0] != "chunk-a" || draft.SourceVersionSnapshot != "snapshot-1" {
		t.Fatalf("draft provenance = refs %v version %q", draft.SourceRefs, draft.SourceVersionSnapshot)
	}
}

func TestPipelineSnapshotUsesOnlyCitedEvidenceSources(t *testing.T) {
	evidence := verifiedEvidence()
	evidence = append(evidence, generation.Evidence{
		ID: "uncited-chunk", Text: "unreferenced", SourceID: "another-source", SourceVersion: "unrelated-version",
	})
	store := &fakeDraftStore{}
	draft, err := NewPipeline(&fakeEvidenceSource{evidence: evidence}, &fakeGenerator{output: validOutput()}, store).
		Run(context.Background(), 12, "simplified_ar", "beginner")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if draft.SourceVersionSnapshot != "snapshot-1" {
		t.Fatalf("source snapshot = %q, want cited evidence snapshot only", draft.SourceVersionSnapshot)
	}
}

func TestPipelineRunFailsClosedWithoutEvidence(t *testing.T) {
	evidence := &fakeEvidenceSource{}
	generator := &fakeGenerator{output: validOutput()}
	store := &fakeDraftStore{}

	_, err := NewPipeline(evidence, generator, store).Run(context.Background(), 12, "simplified_ar", "beginner")
	if !errors.Is(err, ErrInsufficientEvidence) {
		t.Fatalf("Run error = %v, want ErrInsufficientEvidence", err)
	}
	if len(store.drafts) != 0 {
		t.Fatalf("persisted %d drafts without evidence", len(store.drafts))
	}
}

func TestPipelineRejectsEvidenceWithoutSourceProvenance(t *testing.T) {
	for _, tc := range []struct {
		name string
		edit func(*generation.Evidence)
	}{
		{name: "missing source id", edit: func(item *generation.Evidence) { item.SourceID = "" }},
		{name: "missing pinned version", edit: func(item *generation.Evidence) { item.SourceVersion = "" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			evidence := verifiedEvidence()
			tc.edit(&evidence[0])
			store := &fakeDraftStore{}
			_, err := NewPipeline(&fakeEvidenceSource{evidence: evidence}, &fakeGenerator{output: validOutput()}, store).
				Run(context.Background(), 12, "simplified_ar", "beginner")
			if !errors.Is(err, ErrInsufficientEvidence) {
				t.Fatalf("Run error = %v, want ErrInsufficientEvidence", err)
			}
			if len(store.drafts) != 0 {
				t.Fatal("evidence without source provenance was persisted")
			}
		})
	}
}

func TestPipelineRunRejectsMissingOrUnresolvableClaimRefs(t *testing.T) {
	for _, tc := range []struct {
		name string
		refs []string
	}{
		{name: "missing", refs: nil},
		{name: "unprovided position", refs: []string{"2"}},
		{name: "unknown chunk", refs: []string{"chunk-gone"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out := validOutput()
			out.Sections[0].Claims[0].SourceRefs = tc.refs
			store := &fakeDraftStore{}
			_, err := NewPipeline(&fakeEvidenceSource{evidence: verifiedEvidence()}, &fakeGenerator{output: out}, store).
				Run(context.Background(), 12, "simplified_ar", "beginner")
			if !errors.Is(err, ErrInvalidDraft) {
				t.Fatalf("Run error = %v, want ErrInvalidDraft", err)
			}
			if len(store.drafts) != 0 {
				t.Fatal("invalid claim was persisted")
			}
		})
	}
}

func TestPipelineRunRejectsUnstructuredDraftAndOtherSurahs(t *testing.T) {
	t.Run("unstructured", func(t *testing.T) {
		store := &fakeDraftStore{}
		_, err := NewPipeline(&fakeEvidenceSource{evidence: verifiedEvidence()}, &fakeGenerator{output: &generation.GenerationOutput{RequiresReview: true}}, store).
			Run(context.Background(), 12, "simplified_ar", "beginner")
		if !errors.Is(err, ErrInvalidDraft) {
			t.Fatalf("Run error = %v, want ErrInvalidDraft", err)
		}
		if len(store.drafts) != 0 {
			t.Fatal("unstructured draft was persisted")
		}
	})

	t.Run("unsupported surah", func(t *testing.T) {
		store := &fakeDraftStore{}
		_, err := NewPipeline(&fakeEvidenceSource{evidence: verifiedEvidence()}, &fakeGenerator{output: validOutput()}, store).
			Run(context.Background(), 2, "simplified_ar", "beginner")
		if err == nil {
			t.Fatal("Run accepted a surah outside the pilot")
		}
		if len(store.drafts) != 0 {
			t.Fatal("unsupported surah was persisted")
		}
	})
}

func TestPipelineRejectsWrongOutputScriptForStyle(t *testing.T) {
	out := validOutput()
	out.Sections[0].Claims[0].Text = "This claim is in English."
	store := &fakeDraftStore{}
	_, err := NewPipeline(&fakeEvidenceSource{evidence: verifiedEvidence()}, &fakeGenerator{output: out}, store).
		Run(context.Background(), 12, "simplified_ar", "beginner")
	if !errors.Is(err, ErrInvalidDraft) {
		t.Fatalf("Run error = %v, want ErrInvalidDraft", err)
	}
	if len(store.drafts) != 0 {
		t.Fatal("wrong-script draft was persisted")
	}
}

func TestPipelineRejectsUnsupportedStyleBeforeGeneration(t *testing.T) {
	generator := &fakeGenerator{output: validOutput()}
	store := &fakeDraftStore{}
	_, err := NewPipeline(&fakeEvidenceSource{evidence: verifiedEvidence()}, generator, store).
		Run(context.Background(), 12, "unsupported_style", "beginner")
	if err == nil {
		t.Fatal("Run accepted an unsupported style")
	}
	if generator.request.Task != "" || len(store.drafts) != 0 {
		t.Fatal("unsupported style reached generation or persistence")
	}
}

func TestPipelineRunDoesNotPersistWhenStoreFails(t *testing.T) {
	store := &fakeDraftStore{err: errors.New("write failed")}
	_, err := NewPipeline(&fakeEvidenceSource{evidence: verifiedEvidence()}, &fakeGenerator{output: validOutput()}, store).
		Run(context.Background(), 12, "simplified_ar", "beginner")
	if err == nil {
		t.Fatal("Run succeeded when draft persistence failed")
	}
}
