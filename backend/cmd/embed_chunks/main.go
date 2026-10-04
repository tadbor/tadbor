// Batch embedding job for issue #6 — the second half of Day 2, after
// cmd/ingest has put chunks in tafsir_chunks.
//
// # What this is for
//
// A chunk is only reachable through the thematic fallback in
// docs/rag-architecture-part1.md §11 if it carries a vector. Issue #5 produced 454
// chunks for Surah Yusuf with no vectors at all, so the fallback path has nothing to
// rank and every query falls through to "no evidence". This job fills them in.
//
// # Why it is not just `cmd/ingest -live`
//
// cmd/ingest creates chunks and their vectors in a single pass, because it refuses to
// persist a chunk it has not embedded. That is the right shape for the ingest, and
// the wrong shape for a repair job, which needs to:
//
//   - attach a vector to a chunk that is already indexed, without re-chunking;
//   - survive interruption. A 454-chunk run against a free-tier provider is several
//     minutes of network calls, and a run that dies at chunk 400 must not have to
//     start over. Each window is written as it completes, so a re-run picks up the
//     chunks that are still empty;
//   - spend nothing on chunks that already have a vector.
//
// # The three questions -check answers
//
// The acceptance criterion is one line — every chunk has a non-null, non-empty
// embedding array — and -check is how it is verified rather than asserted:
//
//  1. Coverage: does every stored chunk carry a vector?
//  2. Provenance: is the vector the one this pipeline would have written — the
//     current model, the expected width, marked normalized?
//  3. Integrity: does embedding_hash still match the vector stored beside it? This is
//     the only one that needs no provider and no trust in earlier runs: it
//     recomputes the fingerprint from the vector itself, so a truncated or
//     hand-edited vector is caught here rather than silently degrading cosine
//     similarity at query time.
//
// -check reads only the database, so it needs no provider credentials, and exits 1 if
// any chunk fails any of the three. That makes it usable as a gate in front of a
// deployment, not just as a report to read once.
//
// # Why there is no registry gate here
//
// cmd/ingest refuses to write anything for a source that has not cleared
// SourceRegistry.IsUsable, and that gate stays exactly where it is: this job writes
// vectors, not content. internal/retrieval filters on the source_verified flag
// denormalized onto each chunk, and nothing here touches it, so an unverified source
// cannot become servable by being embedded. Embedding is arithmetic over text that is
// already stored, and the point of doing it before approval is that approval should
// not be blocked on a job that can be re-run in a minute.
//
// # Rate limits
//
// The free tier this runs on (docs/rag-architecture-part2.md §26) is credit-metered,
// not request-metered, and the provider answers an over-eager run with 429. Two things
// handle that: embedding.Client already retries a throttled request with exponential
// backoff, and -pace spaces the requests out so the run does not discover the limit by
// hitting it. One surah is 454 chunks — 15 requests at the default batch size — which
// is a few seconds of pacing.
//
// Usage (from backend/):
//
//	go run ./cmd/embed_chunks -check
//	go run ./cmd/embed_chunks -check -source ibn-kathir-ar
//	go run ./cmd/embed_chunks -source ibn-kathir-ar
//	go run ./cmd/embed_chunks -source ibn-kathir-ar -live
//	go run ./cmd/embed_chunks -source ibn-kathir-ar -live -limit 32 -pace 2s
//
// Flags:
//
//	-check    audit stored vectors, write nothing, contact no provider; exit 1 if
//	          any chunk is missing a usable vector
//	-source   restrict to one source registry id (default: every stored chunk)
//	-live     call the embedding provider and write the vectors (default: preview)
//	-batch    inputs per provider request (default 32)
//	-pace     minimum gap between provider requests (default 1s; 0 disables)
//	-limit    stop after this many chunks, to bound a run and prove it resumes
//
// Everything except -live and -check is a preview: it reports what it found and how
// many provider calls it would make, then stops before any request. The api key is
// never logged.
//
// Config comes from the environment (EMBEDDING_API_URL, EMBEDDING_API_KEY,
// MONGO_URI), optionally via a .env file.
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
	"time"

	"github.com/joho/godotenv"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"tadbor/backend/internal/embedding"
	"tadbor/backend/internal/ingestion"
)

// runTimeout bounds a whole run. A single surah needs well under a minute; the
// ceiling exists so a wedged provider fails the job instead of hanging it.
const runTimeout = 30 * time.Minute

// run is everything one invocation decided, printed as JSON at the end so two runs
// can be diffed — the audit record convention cmd/ingest already uses.
type run struct {
	Source     string `json:"source,omitempty"`
	Model      string `json:"model"`
	Dimensions int    `json:"dimensions"`

	Audited          int `json:"audited"`
	WithVector       int `json:"with_vector"`
	NeedingEmbedding int `json:"needing_embedding"`
	// Defects tallies why, so a corpus stuck on one provider quirk is legible
	// without re-running.
	Defects map[string]int `json:"defects,omitempty"`

	Selected      int  `json:"selected"`
	Truncated     bool `json:"truncated"`
	PlannedCalls  int  `json:"planned_calls"`
	ProviderCalls int  `json:"provider_calls"`
	Embedded      int  `json:"embedded"`
	Written       int  `json:"written"`
}

func main() {
	var (
		check  = flag.Bool("check", false, "audit stored vectors and write nothing; exit 1 if any chunk needs embedding")
		source = flag.String("source", "", "source registry id to limit the run to (default: every stored chunk)")
		live   = flag.Bool("live", false, "call the embedding provider and write vectors")
		batch  = flag.Int("batch", embedding.DefaultMaxBatchSize, "inputs per provider request")
		pace   = flag.Duration("pace", time.Second, "minimum gap between provider requests; 0 disables pacing")
		limit  = flag.Int("limit", 0, "stop after embedding this many chunks (0 = no limit)")
	)
	flag.Parse()

	if *check && *live {
		log.Fatal("-check writes nothing, so it cannot be combined with -live")
	}
	if *batch < 1 {
		log.Fatalf("-batch must be at least 1, got %d", *batch)
	}
	if *limit < 0 {
		log.Fatalf("-limit cannot be negative, got %d", *limit)
	}
	if *pace < 0 {
		log.Fatalf("-pace cannot be negative, got %s", *pace)
	}

	// .env is optional — docker-compose and the Makefile pass values directly.
	if err := godotenv.Load(); err != nil && !os.IsNotExist(err) {
		log.Printf("godotenv: %v", err)
	}

	db, err := connect()
	if err != nil {
		log.Fatal(err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = db.Client().Disconnect(ctx)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), runTimeout)
	defer cancel()

	store := ingestion.NewStore(db)

	audits, err := store.AuditEmbeddings(ctx, *source)
	if err != nil {
		log.Fatalf("audit stored chunks: %v", err)
	}

	r := run{Source: *source, Model: embedding.ModelID, Dimensions: embedding.ExpectedDimensions}
	r.Audited = len(audits)
	for _, a := range audits {
		if a.HasVector {
			r.WithVector++
		}
		if a.NeedsEmbedding() {
			r.NeedingEmbedding++
			for _, d := range a.Defects {
				if r.Defects == nil {
					r.Defects = map[string]int{}
				}
				r.Defects[d]++
			}
		}
	}
	log.Printf("audited %d chunk(s)%s: %d carry a vector, %d need embedding",
		r.Audited, sourceSuffix(r.Source), r.WithVector, r.NeedingEmbedding)
	for defect, n := range r.Defects {
		log.Printf("  %-52s %d", defect, n)
	}

	if *check {
		report(r)
		ok, msg := verdict(r)
		if !ok {
			log.Fatal(msg)
		}
		log.Print(msg)
		return
	}

	pending := selectPending(audits, *limit)
	r.Selected = len(pending)
	r.Truncated = r.Selected < r.NeedingEmbedding
	r.PlannedCalls = callsFor(r.Selected, *batch)
	if len(pending) == 0 {
		log.Print("nothing to embed")
		report(r)
		return
	}

	if !*live {
		log.Printf("preview: %d chunk(s) in %d provider call(s) at batch %d, paced %s apart. Re-run with -live to write.",
			r.Selected, r.PlannedCalls, *batch, *pace)
		report(r)
		return
	}

	client, err := embedding.NewClientWithOptions(
		os.Getenv("EMBEDDING_API_URL"),
		os.Getenv("EMBEDDING_API_KEY"),
		embedding.Options{MaxBatchSize: *batch},
	)
	if err != nil {
		log.Fatal(err)
	}

	log.Printf("embedding %d chunk(s) as %s", len(pending), embedding.ModelID)
	for start := 0; start < len(pending); start += *batch {
		if err := ctx.Err(); err != nil {
			log.Printf("run interrupted: %v", err)
			break
		}
		window := pending[start:min(start+*batch, len(pending))]

		// Pace before the request rather than after it: the first call pays no
		// delay, and every later one is spaced by at least -pace.
		if start > 0 && *pace > 0 {
			if err := sleep(ctx, *pace); err != nil {
				break
			}
		}

		texts := make([]string, len(window))
		for i, a := range window {
			// Text, never TextNormalized: diacritics are meaning-bearing and are
			// what the ingest pipeline embeds (Part 1 §7).
			texts[i] = a.Text
		}
		vectors, err := client.Embed(ctx, texts)
		r.ProviderCalls++
		if err != nil {
			// Windows before this one are already stored, so re-running resumes
			// here instead of paying for them twice.
			log.Printf("embedding failed after %d chunk(s): %v", start, err)
			log.Printf("re-run to resume; %d chunk(s) are already stored", r.Written)
			report(r)
			os.Exit(1)
		}

		writes := make([]ingestion.EmbeddedChunk, len(window))
		for i, a := range window {
			writes[i] = ingestion.EmbeddedChunk{ID: a.ID, Vector: vectors[i]}
		}
		res, err := store.SetEmbeddings(ctx, embedding.ModelID, writes)
		if err != nil {
			log.Printf("storing vectors failed after %d chunk(s): %v", start, err)
			report(r)
			os.Exit(1)
		}
		r.Embedded += len(vectors)
		r.Written += int(res.MatchedCount)

		log.Printf("  %d/%d chunks embedded (%d provider call(s) so far)",
			min(start+len(window), len(pending)), len(pending), r.ProviderCalls)
	}

	report(r)
	if r.Selected < r.NeedingEmbedding {
		log.Printf("%d chunk(s) still have no vector — re-run to continue", r.NeedingEmbedding-r.Selected)
	}
}

// selectPending reduces an audit to the chunks this run should embed, in a stable
// order so that an interrupted run and its continuation agree on which window comes
// next. A limit of 0 means no limit; a truncated selection is reported rather than
// silently accepted, because "everything is embedded" would otherwise be printed
// after a partial run.
func selectPending(audits []ingestion.EmbeddingAudit, limit int) []ingestion.EmbeddingAudit {
	var pending []ingestion.EmbeddingAudit
	for _, a := range audits {
		if a.NeedsEmbedding() {
			pending = append(pending, a)
		}
	}
	sort.Slice(pending, func(i, j int) bool { return pending[i].ID < pending[j].ID })
	if limit > 0 && len(pending) > limit {
		pending = pending[:limit]
	}
	return pending
}

// callsFor is the provider request count a selection will cost at a given batch
// size. Reported before the run so the free-tier spend is visible up front.
func callsFor(selected, batch int) int {
	if selected == 0 || batch < 1 {
		return 0
	}
	return (selected + batch - 1) / batch
}

// verdict is what -check concludes, and the line it says so. The acceptance
// criterion is "every chunk has a non-null, non-empty embedding array", which an
// empty collection satisfies vacuously — so an empty corpus is reported as the
// failure it is, rather than as a green light for a pipeline that has not run.
func verdict(r run) (bool, string) {
	switch {
	case r.Audited == 0:
		return false, "FAIL: tafsir_chunks holds no chunks. Ingestion has not run, so there is no vector to verify — this is not a pass."
	case r.NeedingEmbedding > 0:
		return false, fmt.Sprintf("FAIL: %d of %d chunk(s) do not carry a usable %s vector.\n"+
			"  run without -check to see the plan, then with -live to embed them",
			r.NeedingEmbedding, r.Audited, r.Model)
	default:
		return true, fmt.Sprintf("PASS: every one of %d chunk(s) carries a usable %s vector (%d dims, hash verified)",
			r.Audited, r.Model, r.Dimensions)
	}
}

// sleep waits, but stays interruptible: a cancelled context must end the run rather
// than sit out the remaining pacing delay.
func sleep(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func sourceSuffix(sourceID string) string {
	if sourceID == "" {
		return ""
	}
	return " for source " + sourceID
}

// report emits the run's audit record as JSON so it can be diffed between runs or
// piped into another tool.
func report(r run) {
	out, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		log.Printf("report: %+v", r)
		return
	}
	log.Printf("report: %s", out)
}

// connect opens the shared MongoDB handle, honouring MONGO_DB_NAME the same way
// internal/platform does.
func connect() (*mongo.Database, error) {
	uri := os.Getenv("MONGO_URI")
	if uri == "" {
		return nil, errors.New("MONGO_URI is not set")
	}
	dbName := os.Getenv("MONGO_DB_NAME")
	if dbName == "" {
		dbName = "tadbor"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client, err := mongo.Connect(ctx, options.Client().ApplyURI(uri))
	if err != nil {
		return nil, err
	}
	if err := client.Ping(ctx, nil); err != nil {
		return nil, err
	}
	return client.Database(dbName), nil
}
