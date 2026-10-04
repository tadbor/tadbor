package main

import (
	"context"
	"strings"
	"testing"
	"time"

	"tadbor/backend/internal/ingestion"
)

// audit builds the audit rows this command acts on. Defects are set the way
// internal/ingestion sets them: a chunk with none is already embedded.
func audit(id string, defects ...string) ingestion.EmbeddingAudit {
	return ingestion.EmbeddingAudit{ID: id, Text: "نص " + id, Defects: defects}
}

func ids(audits []ingestion.EmbeddingAudit) []string {
	out := make([]string, len(audits))
	for i, a := range audits {
		out[i] = a.ID
	}
	return out
}

func TestSelectPendingKeepsOnlyChunksMissingAVector(t *testing.T) {
	pending := selectPending([]ingestion.EmbeddingAudit{
		audit("done"),
		audit("needs-one", "no vector stored"),
		audit("done-too"),
		audit("needs-two", "no embedding_hash recorded"),
	}, 0)

	got := ids(pending)
	if len(got) != 2 {
		t.Fatalf("selected %v, want the two chunks without a vector", got)
	}
	// A stable order is what makes an interrupted run and its continuation agree on
	// which window comes next.
	if got[0] != "needs-one" || got[1] != "needs-two" {
		t.Fatalf("selection is not ordered by chunk id: %v", got)
	}
}

func TestSelectPendingResumesInBoundedSlices(t *testing.T) {
	audits := []ingestion.EmbeddingAudit{
		audit("a", "no vector stored"),
		audit("b", "no vector stored"),
		audit("c", "no vector stored"),
	}

	if got := ids(selectPending(audits, 2)); len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("limit 2 selected %v", got)
	}
	// A limit at or above the backlog is not a truncation, and must not look like
	// one: "re-run to continue" has to mean something.
	if got := ids(selectPending(audits, 3)); len(got) != 3 {
		t.Fatalf("limit 3 selected %v", got)
	}
	if got := ids(selectPending(audits, 99)); len(got) != 3 {
		t.Fatalf("limit 99 selected %v", got)
	}
	if got := selectPending(nil, 5); len(got) != 0 {
		t.Fatalf("expected nothing pending, got %v", ids(got))
	}
}

func TestCallsForCountsTheProvidersRequests(t *testing.T) {
	for _, tc := range []struct {
		selected, batch, want int
	}{
		{0, 32, 0},    // nothing to do spends nothing
		{1, 32, 1},    //
		{32, 32, 1},   // an exact multiple must not ask for an empty extra request
		{33, 32, 2},   //
		{454, 32, 15}, // one surah of Ibn Kathir, the real case
		{454, 64, 8},  //
		{5, 0, 0},     // a nonsensical batch size must not divide by zero
	} {
		if got := callsFor(tc.selected, tc.batch); got != tc.want {
			t.Errorf("callsFor(%d, %d) = %d, want %d", tc.selected, tc.batch, got, tc.want)
		}
	}
}

// Pacing must not outlive the run: a cancelled context has to end the wait instead
// of sitting out the remaining delay.
func TestSleepIsInterruptible(t *testing.T) {
	if err := sleep(context.Background(), time.Millisecond); err != nil {
		t.Fatalf("sleep: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	start := time.Now()
	if err := sleep(ctx, time.Minute); err == nil {
		t.Fatal("expected sleep to report the cancellation")
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("sleep ignored the cancelled context for %s", elapsed)
	}
}

func TestVerdictRefusesToPassAnEmptyCorpus(t *testing.T) {
	// "Every chunk has an embedding" is vacuously true of zero chunks. Reporting
	// that as a pass would be a green light for a pipeline that has not run.
	ok, msg := verdict(run{Model: "BAAI/bge-m3", Dimensions: 1024})
	if ok {
		t.Fatalf("an empty corpus must not pass: %s", msg)
	}
	if !strings.Contains(msg, "Ingestion has not run") {
		t.Fatalf("the failure must say why, got %q", msg)
	}
}

func TestVerdict(t *testing.T) {
	ok, msg := verdict(run{
		Model: "BAAI/bge-m3", Dimensions: 1024,
		Audited: 454, WithVector: 454,
	})
	if !ok || !strings.Contains(msg, "PASS") || !strings.Contains(msg, "454") {
		t.Fatalf("a fully embedded corpus must pass, got ok=%v msg=%q", ok, msg)
	}

	ok, msg = verdict(run{
		Model: "BAAI/bge-m3", Dimensions: 1024,
		Audited: 454, WithVector: 358, NeedingEmbedding: 97,
	})
	if ok {
		t.Fatalf("a corpus with 97 unembedded chunks must fail, got %q", msg)
	}
	for _, want := range []string{"FAIL", "97 of 454", "-live"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("failure message %q is missing %q", msg, want)
		}
	}
}
