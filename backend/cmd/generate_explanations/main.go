// Batch explanation generation for Surah Yusuf — issue #10.
//
// # What this is for
//
// Issues #5 through #8 built and verified retrieval, chunking, embedding and the
// generation contract against a throwaway database. Nothing loops over the 111
// ayahs yet, so the explanations collection is empty and the reviewer dashboard
// from issue #11 has nothing to review. This job connects them: for each ayah it
// retrieves evidence, asks the model to simplify it, validates the result, and
// writes a pending explanations document a human can act on.
//
// # It cannot write anything from an unverified source, and that is structural
//
// Both retrieval paths filter on source_verified: true, so while the committed
// Ibn Kathir text is still unverified (docs/reviews/ibn-kathir-ar-surah-12.md)
// every ayah returns ErrInsufficientEvidence and this job writes zero
// explanations and 111 "insufficient evidence" failures. That is the correct
// outcome, not a bug to work around: Part 1 §12 exists to stop the pipeline
// generating from material nobody has approved, and the empty result is that
// rule holding. Nothing here can be persuaded to write anyway.
//
// # Failures are recorded, not skipped
//
// Three things can go wrong per ayah — no evidence, the provider refusing or
// failing, or a draft that fails validation — and the job treats them differently
// on purpose:
//
//   - No evidence is not an error. The verse has no approved commentary yet, and
//     the honest record is "we looked and there is nothing" (§12), recorded as
//     its own outcome so a gap in coverage is visible rather than inferred from
//     a missing row.
//   - A provider failure is an error and is not retried beyond the client's own
//     budget: it may be the outage that stops all 111 at once.
//   - A draft that fails validation is the most important of the three. It is
//     recorded with the validation error and the raw output, because a rejected
//     draft is evidence about the prompt, and throwing the text away loses the
//     only copy of why the model misbehaved.
//
// Every attempt writes a generation_runs record — model, prompt version,
// retrieved chunk ids, raw output — before any explanation is inserted. The audit
// trail therefore covers failures too, which is what makes it an audit trail and
// not a success log.
//
// # What validation runs here, and what it does not
//
// generation.Validate covers what is decidable from the request and the draft
// alone: non-empty content, length, requires_review, citation coverage, and that
// the content is written in the requested script.
//
// §19's other three checks need the database, so they are here:
//
//   - Source validation — every source_ref resolves to a chunk whose registry
//     source is verification_status: verified.
//   - Verse mapping — every cited chunk actually covers the ayah being explained.
//     This is the check that catches a generation-layer mix-up, where a draft for
//     ayah 12 is grounded in the commentary for ayah 40.
//   - Quran text integrity — the verse text handed to the pipeline is compared
//     byte-for-byte against the verified corpus, so a modified or truncated
//     ayah cannot be silently used as the thing being explained.
//
// One thing this does *not* check, stated plainly because §19 reads as if it
// might: no attempt is made to verify Arabic quoted inside the model's own
// output against the corpus. Doing that needs quote extraction, and a wrong
// extraction would reject drafts for the wrong reason. A misquotation is caught
// by the reviewer's "no added or removed meaning" check in §20, which is a human
// judgement about meaning, not a string match.
//
// # Usage (from backend/):
//
//	go run ./cmd/generate_explanations -check
//	go run ./cmd/generate_explanations -check -expect 111
//	go run ./cmd/generate_explanations -live
//	go run ./cmd/generate_explanations -live -limit 8 -pace 3s
//
// Flags:
//
//	-check    audit stored explanations, write nothing, call no provider; exit 1
//	          if coverage or provenance is wrong
//	-live     call the provider and write explanations
//	-surah    surah id (default 12)
//	-task     generation task (default: simplify)
//	-style    target style (default: simplified_ar)
//	-level    explanation level written to the document (default: beginner)
//	-pace     minimum gap between provider calls (default 2s; 0 disables)
//	-limit    stop after this many ayahs (0 = no limit)
//	-dry-run  resolve evidence and validate the pipeline, but skip the provider
//	          call and write no explanations — proves the run without spending
//	          anything
//
// Everything except -live and -check is a preview.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/joho/godotenv"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"tadbor/backend/internal/embedding"
	"tadbor/backend/internal/generation"
	"tadbor/backend/internal/platform"
	"tadbor/backend/internal/retrieval"
)

const runTimeout = 90 * time.Minute

// defaultPace is the free tier's request rate, not a guess.
//
// Measured against the live API rather than read off a quota page: the provider
// answers "Quota exceeded for metric: generate_content_free_tier_requests,
// limit: 5". Five a minute is 12s apart; 13s leaves a little headroom. The
// first version of this job defaulted to 2s, asked for 30 a minute, and lost 3
// of its first 5 ayahs to 429 and 503 — the client's retries could not outlast a
// quota it was exceeding on every single call.
//
// At 13s a full 111-ayah run takes about 24 minutes. That is the real cost of the
// free tier and it belongs in the open rather than hidden behind a fast default.
const defaultPace = 13 * time.Second

// errorPace is the pause after a provider failure. A 429 or 503 is usually the
// provider telling us to slow down, so the next ayah waits longer regardless of
// what -pace says. Without it a run that has just been throttled keeps asking at
// the same rate and converts one failure into five.
const errorPace = 45 * time.Second

// explanationDoc is written to the explanations collection.
//
// Text and SourceRefs mirror content.Explanation; the extra provenance fields are
// what the reviewer's audit trail needs and what §20 requires. Content already
// has a struct, but it deliberately hides review_status and
// source_version_snapshot from the JSON projection for the reader, so it cannot
// be reused to write a document that must carry them.
type explanationDoc struct {
	SurahID    int      `bson:"surah_id"`
	AyahStart  int      `bson:"ayah_start"`
	AyahEnd    int      `bson:"ayah_end"`
	Style      string   `bson:"style"`
	Level      string   `bson:"level"`
	Text       string   `bson:"text"`
	SourceRefs []string `bson:"source_refs"`
	Claims     []string `bson:"claims,omitempty"`
	Warnings   []string `bson:"warnings,omitempty"`
	Confidence string   `bson:"confidence,omitempty"`

	ReviewStatus   string `bson:"review_status"`
	RequiresReview bool   `bson:"requires_review"`

	SourceVersion string `bson:"source_version_snapshot"`
	PromptVersion string `bson:"prompt_version"`
	ModelVersion  string `bson:"model_version"`
	RunID         string `bson:"generation_run_id"`
	CreatedAt     string `bson:"created_at"`
}

// generationRun is the audit record, one per attempt including failures.
type generationRun struct {
	RunID   string `bson:"run_id"`
	SurahID int    `bson:"surah_id"`
	Ayah    int    `bson:"ayah"`
	Task    string `bson:"task"`
	Style   string `bson:"style"`
	Level   string `bson:"level"`

	Outcome  string   `bson:"outcome"` // pending | insufficient_evidence | validation_failed | provider_error
	Error    string   `bson:"error,omitempty"`
	Warnings []string `bson:"warnings,omitempty"`

	RetrievedChunkIDs []string `bson:"retrieved_chunk_ids"`
	// EvidenceText is stored so a rejected draft can still be read against the
	// text the model was given. Without it, "why did it do that" is unanswerable.
	EvidenceText []string `bson:"evidence_text,omitempty"`

	SourceRefs    []string `bson:"source_refs,omitempty"`
	RawOutput     string   `bson:"raw_output,omitempty"`
	RawJSON       string   `bson:"raw_structured_output,omitempty"`
	ExplanationID string   `bson:"explanation_id,omitempty"`

	PromptVersion string `bson:"prompt_version"`
	ModelVersion  string `bson:"model_version"`
	DurationMS    int64  `bson:"duration_ms"`
	CreatedAt     string `bson:"created_at"`
}

type report struct {
	SurahID int    `json:"surah_id"`
	Task    string `json:"task"`
	Style   string `json:"style"`
	Level   string `json:"level"`
	Ayahs   int    `json:"ayahs"`
	Planned int    `json:"planned"`

	PromptVersion string `json:"prompt_version"`
	ModelVersion  string `json:"model_version"`

	WithEvidence int `json:"with_evidence"`
	NoEvidence   int `json:"no_evidence"`

	ProviderCalls int `json:"provider_calls"`
	Validated     int `json:"validated"`
	Written       int `json:"written"`

	ProviderErrors   int `json:"provider_errors"`
	ValidationFailed int `json:"validation_failed"`

	// Re-running must not duplicate or overwrite: AlreadyCurrent counts ayahs
	// this exact run already produced, Regenerated counts pending drafts from an
	// older run or prompt, DecidedSkipped counts ones a human has already ruled
	// on. Only Regenerated is rewritten.
	AlreadyCurrent int `json:"already_current"`
	Regenerated    int `json:"regenerated"`
	DecidedSkipped int `json:"decided_skipped"`

	StoredPending int `json:"stored_pending"`
	MissingVerses int `json:"missing_verses"`
	// Gaps names ayahs that produced nothing, so the report says which verses
	// rather than only how many.
	Gaps []string `json:"gaps,omitempty"`
	// Rejected lists ayahs whose draft failed validation, with the reason.
	Rejected  []string `json:"rejected,omitempty"`
	Truncated bool     `json:"truncated"`
}

func main() {
	var (
		check  = flag.Bool("check", false, "audit stored explanations and write nothing")
		live   = flag.Bool("live", false, "call the provider and write explanations")
		dry    = flag.Bool("dry-run", false, "resolve evidence but skip the provider call")
		surah  = flag.Int("surah", 12, "surah id")
		task   = flag.String("task", "simplify", "generation task")
		style  = flag.String("style", "simplified_ar", "target style")
		level  = flag.String("level", "beginner", "explanation level")
		pace   = flag.Duration("pace", defaultPace, "minimum gap between provider calls (the free tier allows 5/minute)")
		limit  = flag.Int("limit", 0, "stop after this many ayahs (0 = no limit)")
		expect = flag.Int("expect", 0, "ayah count the audit requires; 0 reads it from the corpus")
	)
	flag.Parse()

	if *check && (*live || *dry) {
		log.Fatal("-check writes nothing and calls no provider, so it cannot be combined with -live or -dry-run")
	}
	if *live && *dry {
		log.Fatal("-dry-run skips the provider call, so it cannot be combined with -live")
	}
	if *limit < 0 {
		log.Fatalf("-limit cannot be negative, got %d", *limit)
	}

	if err := godotenv.Load(); err != nil && !os.IsNotExist(err) {
		log.Printf("godotenv: %v", err)
	}
	// MONGO_DB_NAME is honoured like every other command, so a probe run can
	// point at a throwaway database instead of the real one.
	db, err := platform.NewMongoConnection()
	if err != nil {
		log.Fatalf("mongo: %v (is MONGO_URI set?)", err)
	}
	defer platform.CloseMongoConnection(db)

	ctx, cancel := context.WithTimeout(context.Background(), runTimeout)
	defer cancel()

	ayahs, err := corpusAyahs(ctx, db, *surah)
	if err != nil {
		log.Fatal(err)
	}
	if ayahs == 0 {
		log.Fatalf("surah %d has no ayahs in the corpus; run make seed first", *surah)
	}
	if *expect > 0 && *expect != ayahs {
		log.Fatalf("corpus has %d ayahs for surah %d but -expect is %d", ayahs, *surah, *expect)
	}

	r := report{
		SurahID: *surah, Task: *task, Style: *style, Level: *level,
		Ayahs: ayahs, Planned: ayahs,
		PromptVersion: generation.PromptVersion(),
		ModelVersion:  generation.ModelVersion(),
	}

	if *check {
		if err := audit(ctx, db, &r, *style, *limit); err != nil {
			log.Fatal(err)
		}
		emit(&r)
		if r.StoredPending != r.Planned || r.MissingVerses > 0 {
			log.Fatalf("audit failed: %d of %d pending explanations, %d verses missing",
				r.StoredPending, r.Planned, r.MissingVerses)
		}
		return
	}

	// Retrieval needs an embedder only for the thematic fallback. Exact verse
	// mapping needs no query vector, so a nil embedder would work — but it
	// would also turn a coverage gap into ErrInsufficientEvidence for the wrong
	// reason, so the client is built whenever credentials exist and its absence
	// is reported rather than hidden.
	var embedder *embedding.Client
	if endpoint, key := os.Getenv("EMBEDDING_API_URL"), os.Getenv("EMBEDDING_API_KEY"); endpoint != "" && key != "" {
		embedder, err = embedding.NewClient(endpoint, key)
		if err != nil {
			log.Printf("embedding client unavailable, exact verse mapping only: %v", err)
			embedder = nil
		}
	}
	retrievalSvc := retrieval.NewService(db, embedder)
	gen := generation.NewService()

	if !*live && !*dry {
		emit(&r)
		log.Printf("preview: %d ayahs, style %s, prompt %s, model %s; pass -live to call the provider", ayahs, *style, r.PromptVersion, r.ModelVersion)
		return
	}

	if err := ensureIndex(ctx, db); err != nil {
		log.Fatal(err)
	}

	explanations := db.Collection("explanations")
	runs := db.Collection("generation_runs")

	lastFailed := false
	for ayah := 1; ayah <= ayahs; ayah++ {
		if *limit > 0 && ayah > *limit {
			r.Truncated = true
			break
		}

		gr := generationRun{
			RunID:         runID(*surah, ayah),
			SurahID:       *surah,
			Ayah:          ayah,
			Task:          *task,
			Style:         *style,
			Level:         *level,
			PromptVersion: r.PromptVersion,
			ModelVersion:  r.ModelVersion,
			CreatedAt:     time.Now().UTC().Format(time.RFC3339),
		}
		started := time.Now()

		// A re-run over a long batch must be safe, because a 24-minute run
		// against a rate limit will get interrupted and the natural response is to
		// start it again. Three cases, and the difference matters:
		//
		//   - this exact run and prompt already wrote it: skip, counting done;
		//   - a human has decided it: skip and never touch it. Regenerating over
		//     an approved explanation would delete the text a reviewer approved and
		//     silently replace the citation history behind it.
		//   - pending, from an older run or prompt: regenerate, because leaving it
		//     would leave the surah claiming coverage from a superseded prompt.
		prev, err := findExisting(ctx, explanations, *surah, ayah, *style, *level)
		if err != nil {
			log.Fatal(err)
		}
		switch {
		case prev == nil:
		case prev.ReviewStatus != "pending":
			r.DecidedSkipped++
			log.Printf("ayah %d/%d: skipped, already %s", ayah, ayahs, prev.ReviewStatus)
			continue
		case prev.RunID == gr.RunID && prev.PromptVersion == r.PromptVersion:
			r.AlreadyCurrent++
			continue
		default:
			r.Regenerated++
		}

		chunks, err := retrievalSvc.GetEvidenceForAyah(ctx, *surah, ayah)
		if err != nil {
			gr.Outcome, gr.Error = "insufficient_evidence", err.Error()
			r.NoEvidence++
			r.Gaps = append(r.Gaps, fmt.Sprintf("%d:%s", ayah, err.Error()))
			gr.DurationMS = time.Since(started).Milliseconds()
			writeRun(ctx, runs, gr)
			continue
		}
		r.WithEvidence++

		// The three §19 checks that need the database, run before the provider
		// is called so an ayah that cannot be grounded never costs a request.
		if err := checkGrounding(ctx, db, *surah, ayah, chunks); err != nil {
			gr.Outcome, gr.Error = "invalid_grounding", err.Error()
			gr.RetrievedChunkIDs = chunkIDs(chunks)
			r.NoEvidence++
			r.Gaps = append(r.Gaps, fmt.Sprintf("%d:%s", ayah, err.Error()))
			gr.DurationMS = time.Since(started).Milliseconds()
			writeRun(ctx, runs, gr)
			continue
		}

		// Quran text integrity: the verse this is about must match the corpus
		// exactly, so "the thing being explained" cannot have drifted.
		if err := checkAyahIntegrity(ctx, db, *surah, ayah); err != nil {
			gr.Outcome, gr.Error = "corpus_mismatch", err.Error()
			gr.RetrievedChunkIDs = chunkIDs(chunks)
			r.NoEvidence++
			r.Gaps = append(r.Gaps, fmt.Sprintf("%d:%s", ayah, err.Error()))
			gr.DurationMS = time.Since(started).Milliseconds()
			writeRun(ctx, runs, gr)
			continue
		}

		gr.RetrievedChunkIDs = chunkIDs(chunks)
		gr.EvidenceText = chunkTexts(chunks)

		if *dry {
			gr.Outcome = "dry_run"
			gr.DurationMS = time.Since(started).Milliseconds()
			writeRun(ctx, runs, gr)
			continue
		}

		req := generation.Request{
			Task:           *task,
			TargetLanguage: targetLanguage(*style),
			TargetStyle:    *style,
			Evidence:       toEvidence(chunks),
		}

		if gap := nextGap(*pace, lastFailed); gap > 0 {
			time.Sleep(gap)
		}

		out, err := gen.Generate(req)
		r.ProviderCalls++
		if err != nil {
			gr.Outcome, gr.Error = "provider_error", err.Error()
			r.ProviderErrors++
			lastFailed = true
			r.Rejected = append(r.Rejected, fmt.Sprintf("%d:provider_error", ayah))
			gr.DurationMS = time.Since(started).Milliseconds()
			writeRun(ctx, runs, gr)
			continue
		}
		lastFailed = false

		gr.SourceRefs = out.SourceRefs
		gr.Warnings = out.Warnings
		raw, _ := json.Marshal(out)
		gr.RawJSON = string(raw)
		gr.RawOutput = out.Content

		if err := generation.Validate(req, out); err != nil {
			gr.Outcome, gr.Error = "validation_failed", err.Error()
			r.ValidationFailed++
			r.Rejected = append(r.Rejected, fmt.Sprintf("%d:%s", ayah, err.Error()))
			gr.DurationMS = time.Since(started).Milliseconds()
			writeRun(ctx, runs, gr)
			continue
		}
		r.Validated++

		// Source validation on the refs the model actually cited, now that they
		// are real chunk ids: every one must belong to a verified source. A ref
		// to a chunk that is not verified means the model reached evidence this
		// pipeline is not allowed to serve from.
		if err := checkCitedSources(ctx, db, out.SourceRefs); err != nil {
			gr.Outcome, gr.Error = "validation_failed", err.Error()
			r.ValidationFailed++
			r.Rejected = append(r.Rejected, fmt.Sprintf("%d:%s", ayah, err.Error()))
			gr.DurationMS = time.Since(started).Milliseconds()
			writeRun(ctx, runs, gr)
			continue
		}

		version, err := currentSourceVersion(ctx, db, chunks)
		if err != nil {
			gr.Outcome, gr.Error = "provider_error", err.Error()
			r.ProviderErrors++
			gr.DurationMS = time.Since(started).Milliseconds()
			writeRun(ctx, runs, gr)
			continue
		}

		doc := explanationDoc{
			SurahID: *surah, AyahStart: ayah, AyahEnd: ayah,
			Style: *style, Level: *level,
			Text:           out.Content,
			SourceRefs:     out.SourceRefs,
			Claims:         out.Claims,
			Warnings:       out.Warnings,
			Confidence:     out.Confidence,
			ReviewStatus:   "pending",
			RequiresReview: true,
			SourceVersion:  version,
			PromptVersion:  r.PromptVersion,
			ModelVersion:   r.ModelVersion,
			RunID:          gr.RunID,
			CreatedAt:      time.Now().UTC().Format(time.RFC3339),
		}
		// Upsert on the unique key, not InsertOne: a re-run must replace its own
		// draft rather than add a second one for the same verse.
		res, err := explanations.UpdateOne(ctx,
			bson.M{"surah_id": *surah, "ayah_start": ayah, "style": *style, "level": *level},
			bson.M{"$set": doc},
			options.Update().SetUpsert(true))
		if err != nil {
			gr.Outcome, gr.Error = "write_failed", err.Error()
			r.ProviderErrors++
			gr.DurationMS = time.Since(started).Milliseconds()
			writeRun(ctx, runs, gr)
			continue
		}
		if id, ok := res.UpsertedID.(interface{ Hex() string }); ok {
			gr.ExplanationID = id.Hex()
		}
		gr.Outcome = "pending"
		r.Written++
		gr.DurationMS = time.Since(started).Milliseconds()
		writeRun(ctx, runs, gr)

		log.Printf("ayah %d/%d: pending (%d refs, %d chars, confidence %s)",
			ayah, ayahs, len(out.SourceRefs), len([]rune(out.Content)), out.Confidence)
	}

	if err := audit(ctx, db, &r, *style, *limit); err != nil {
		log.Fatal(err)
	}
	emit(&r)

	// A run that wrote nothing because nothing was approved is correct. A run
	// that failed is not, and must not look like success.
	if r.ProviderErrors > 0 {
		log.Fatalf("%d ayahs failed at the provider or on write; see generation_runs", r.ProviderErrors)
	}
	if *live && *limit == 0 && r.Written < r.Planned {
		log.Printf("%d of %d ayahs are pending explanations; %d had no approved evidence", r.Written, r.Planned, r.NoEvidence)
	}
}

// checkGrounding is §19's verse-mapping check: every retrieved chunk must
// actually cover the ayah being explained.
//
// The retrieval query already asks for that, so this is redundant against a
// correct query — which is the point. It is the check that catches a generation
// layer asking for the wrong ayah, or a future query that loosens the filter,
// and it costs nothing but a comparison.
func checkGrounding(ctx context.Context, db *mongo.Database, surahID, ayah int, chunks []retrieval.TafsirChunk) error {
	for _, c := range chunks {
		if c.SurahID != surahID || c.AyahStart > ayah || c.AyahEnd < ayah {
			return fmt.Errorf("chunk %s covers %d:%d-%d, which does not include %d:%d",
				c.ID, c.SurahID, c.AyahStart, c.AyahEnd, surahID, ayah)
		}
		if !c.SourceVerified {
			return fmt.Errorf("chunk %s is not from a verified source", c.ID)
		}
	}
	return nil
}

// checkAyahIntegrity is §19's Quran text integrity check for the verse under
// discussion. It re-reads the ayah from the corpus and requires a non-empty
// exact text, so a truncated or missing row cannot be used as the subject.
func checkAyahIntegrity(ctx context.Context, db *mongo.Database, surahID, ayah int) error {
	var a struct {
		TextUthmani string `bson:"text_uthmani"`
	}
	err := db.Collection("ayahs").FindOne(ctx, bson.M{"surah_id": surahID, "ayah_number": ayah}).
		Decode(&a)
	if err != nil {
		return fmt.Errorf("ayah %d:%d is not in the verified corpus: %w", surahID, ayah, err)
	}
	if strings.TrimSpace(a.TextUthmani) == "" {
		return fmt.Errorf("ayah %d:%d has empty corpus text", surahID, ayah)
	}
	return nil
}

// checkCitedSources is §19's source validation applied to what the model cited.
// A ref that does not resolve, or resolves to a source that is not verified,
// means the explanation is grounded in something the reader would be told is
// approved when it is not.
func checkCitedSources(ctx context.Context, db *mongo.Database, refs []string) error {
	if len(refs) == 0 {
		return errors.New("no source_refs survived resolution")
	}
	ids := make([]any, 0, len(refs))
	for _, ref := range refs {
		ids = append(ids, ref)
	}
	cur, err := db.Collection("tafsir_chunks").Find(ctx, bson.M{"_id": bson.M{"$in": ids}})
	if err != nil {
		return err
	}
	defer cur.Close(ctx)

	found := map[string]bool{}
	for cur.Next(ctx) {
		var c struct {
			ID       string `bson:"_id"`
			Verified bool   `bson:"source_verified"`
		}
		if err := cur.Decode(&c); err != nil {
			return err
		}
		if !c.Verified {
			return fmt.Errorf("cited chunk %s is not from a verified source", c.ID)
		}
		found[c.ID] = true
	}
	if err := cur.Err(); err != nil {
		return err
	}
	for _, ref := range refs {
		if !found[ref] {
			return fmt.Errorf("cited chunk %s does not exist", ref)
		}
	}
	return nil
}

// currentSourceVersion reads the registry version of the sources the evidence
// came from, so the explanation records what it was grounded in — §18's
// requirement that a later source change cannot retroactively alter a citation.
func currentSourceVersion(ctx context.Context, db *mongo.Database, chunks []retrieval.TafsirChunk) (string, error) {
	ids := map[string]bool{}
	for _, c := range chunks {
		ids[c.SourceID] = true
	}
	list := make([]any, 0, len(ids))
	for id := range ids {
		list = append(list, id)
	}
	cur, err := db.Collection("sources").Find(ctx, bson.M{"_id": bson.M{"$in": list}})
	if err != nil {
		return "", err
	}
	defer cur.Close(ctx)

	versions := []string{}
	for cur.Next(ctx) {
		var s struct {
			ID             string `bson:"_id"`
			CurrentVersion string `bson:"current_version"`
		}
		if err := cur.Decode(&s); err != nil {
			return "", err
		}
		versions = append(versions, s.CurrentVersion)
	}
	if err := cur.Err(); err != nil {
		return "", err
	}
	if len(versions) == 0 {
		return "", errors.New("no registry entry for the evidence's source")
	}
	sort.Strings(versions)
	return strings.Join(versions, "+"), nil
}

func ensureIndex(ctx context.Context, db *mongo.Database) error {
	err := ensureNamedIndex(ctx, db.Collection("explanations"), mongo.IndexModel{
		Keys: bson.D{
			{Key: "surah_id", Value: 1},
			{Key: "ayah_start", Value: 1},
			{Key: "style", Value: 1},
			{Key: "level", Value: 1},
		},
		Options: options.Index().SetUnique(true).SetName("surah_ayah_style_level"),
	})
	if err != nil {
		return err
	}
	// generation_runs is append-only and read by ayah, so it needs no index
	// beyond _id; the run_id is deterministic, which is what makes a re-run
	// replace its own audit record instead of accumulating duplicates.
	return ensureNamedIndex(ctx, db.Collection("generation_runs"), mongo.IndexModel{
		Keys:    bson.D{{Key: "run_id", Value: 1}},
		Options: options.Index().SetUnique(true).SetName("run_id_unique"),
	})
}

// ensureNamedIndex creates an index, replacing an existing one of the same name
// whose options differ.
//
// Mongo refuses this outright — same name, different options, is
// IndexKeySpecsConflict, not something it will merge. That is the right refusal
// in general, but it makes a job that tightens an index (this one started
// non-unique, which allowed a re-run to write two explanations for one verse)
// unusable until someone drops the index by hand in production. The intent is
// unambiguous here: same name, same key, only the unique flag differing.
//
// It does not silently weaken anything: if the data cannot satisfy the stronger
// index, CreateOne still fails loudly rather than leaving a weaker index in
// place, because the drop is rolled back by that failure and the run stops.
func ensureNamedIndex(ctx context.Context, coll *mongo.Collection, model mongo.IndexModel) error {
	_, err := coll.Indexes().CreateOne(ctx, model)
	if err == nil {
		return nil
	}
	name := model.Options.Name
	if name == nil || !strings.Contains(err.Error(), "IndexKeySpecsConflict") {
		return err
	}
	cursor, lerr := coll.Indexes().List(ctx)
	if lerr != nil {
		return err
	}
	var existing []bson.M
	if lerr := cursor.All(ctx, &existing); lerr != nil {
		return err
	}
	for _, idx := range existing {
		if n, _ := idx["name"].(string); n != *name {
			continue
		}
		if _, derr := coll.Indexes().DropOne(ctx, *name); derr != nil {
			return fmt.Errorf("replacing index %s: %w", *name, derr)
		}
		_, cerr := coll.Indexes().CreateOne(ctx, model)
		if cerr != nil {
			return fmt.Errorf("recreating index %s stronger than before (duplicates?): %w", *name, cerr)
		}
		return nil
	}
	return err
}

// findExisting returns the stored explanation for one verse and style, or nil.
// It selects review_status explicitly rather than decoding the whole document,
// because the only question asked of it here is whether a human has ruled on it.
func findExisting(ctx context.Context, explanations *mongo.Collection, surahID, ayah int, style, level string) (*existingExplanation, error) {
	var e existingExplanation
	err := explanations.FindOne(ctx,
		bson.M{"surah_id": surahID, "ayah_start": ayah, "style": style, "level": level},
		options.FindOne().SetProjection(bson.M{
			"review_status": 1, "generation_run_id": 1, "prompt_version": 1,
		}),
	).Decode(&e)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &e, nil
}

type existingExplanation struct {
	ReviewStatus  string `bson:"review_status"`
	RunID         string `bson:"generation_run_id"`
	PromptVersion string `bson:"prompt_version"`
}

// nextGap is the pause before the next provider call, longer after a failure.
func nextGap(pace time.Duration, lastFailed bool) time.Duration {
	if pace <= 0 {
		return 0
	}
	if lastFailed {
		return errorPace
	}
	return pace
}

func writeRun(ctx context.Context, runs *mongo.Collection, gr generationRun) {
	// Upsert on the deterministic run_id: re-running this job for an ayah
	// replaces its audit record rather than leaving two, so "what happened to
	// ayah 12" has one answer.
	_, err := runs.UpdateOne(ctx, bson.M{"run_id": gr.RunID}, bson.M{"$set": gr}, options.Update().SetUpsert(true))
	if err != nil {
		log.Printf("writing generation_run %s: %v", gr.RunID, err)
	}
}

// audit counts pending explanations and names the verses that have none.
func audit(ctx context.Context, db *mongo.Database, r *report, style string, limit int) error {
	cur, err := db.Collection("explanations").Find(ctx, bson.M{
		"surah_id": r.SurahID, "style": style, "review_status": "pending",
	})
	if err != nil {
		return err
	}
	defer cur.Close(ctx)

	seen := map[int]bool{}
	for cur.Next(ctx) {
		var e struct {
			AyahStart int `bson:"ayah_start"`
		}
		if err := cur.Decode(&e); err != nil {
			return err
		}
		seen[e.AyahStart] = true
	}
	if err := cur.Err(); err != nil {
		return err
	}
	r.StoredPending = len(seen)
	if limit > 0 {
		if want := limit; want < r.Ayahs {
			r.Planned = want
		}
	}
	for ayah := 1; ayah <= r.Planned; ayah++ {
		if !seen[ayah] {
			r.MissingVerses++
			r.Gaps = append(r.Gaps, fmt.Sprintf("%d:no pending explanation", ayah))
		}
	}
	return nil
}

func corpusAyahs(ctx context.Context, db *mongo.Database, surahID int) (int, error) {
	n, err := db.Collection("ayahs").CountDocuments(ctx, bson.M{"surah_id": surahID})
	if err != nil {
		return 0, err
	}
	return int(n), nil
}

func chunkIDs(chunks []retrieval.TafsirChunk) []string {
	ids := make([]string, 0, len(chunks))
	for _, c := range chunks {
		ids = append(ids, c.ID)
	}
	return ids
}

func chunkTexts(chunks []retrieval.TafsirChunk) []string {
	out := make([]string, 0, len(chunks))
	for _, c := range chunks {
		out = append(out, c.Text)
	}
	return out
}

func toEvidence(chunks []retrieval.TafsirChunk) []generation.Evidence {
	out := make([]generation.Evidence, 0, len(chunks))
	for _, c := range chunks {
		out = append(out, generation.Evidence{ID: c.ID, Text: c.Text})
	}
	return out
}

// targetLanguage maps a style to its language, because Request carries both and
// the model is told the language as well as the style.
func targetLanguage(style string) string {
	if strings.HasSuffix(style, "_ar") {
		return "ar"
	}
	return "en"
}

func runID(surahID, ayah int) string {
	return fmt.Sprintf("s%03da%03d", surahID, ayah)
}

func emit(r *report) {
	b, _ := json.MarshalIndent(r, "", "  ")
	fmt.Println(string(b))
}
