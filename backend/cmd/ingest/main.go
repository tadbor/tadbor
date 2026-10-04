// Offline ingestion runner for the Day 2 pipeline (docs/rag-architecture-part1.md
// §7). It is deliberately a separate binary from the HTTP API: ingestion is an
// internal, offline module (docs/rag-architecture-part2.md §25) that must never
// be reachable from a reader request.
//
// This lives under backend/cmd rather than scripts/ because Go's internal-package
// rule only allows packages inside backend/ to import backend/internal/...
//
// Usage (from backend/):
//
//	go run ./cmd/ingest -list
//	go run ./cmd/ingest -source tabari-yusuf -manifest ../corpus/tabari-yusuf.json
//	go run ./cmd/ingest -source tabari-yusuf -manifest ../corpus/tabari-yusuf.json -live -prune
//	go run ./cmd/ingest -source tabari-yusuf -revalidate
//
// Flags:
//
//	-list        list sources with their ingestion status, then exit
//	-source      source registry id to ingest (required unless -list)
//	-manifest    JSON file holding that source's documents; absent or empty
//	             means there is nothing to ingest yet
//	-live        actually call the embedding provider and write to MongoDB
//	-prune       delete stored chunks that the manifest no longer contains
//	-batch       embedding batch size (default 32)
//	-revalidate  re-apply the registry gate to a source's stored chunks, then exit
//	-allow-unverified
//	             preview a source that has not cleared the registry gate. Issue #5
//	             needs this: a manifest's verse mapping has to be reviewable
//	             before anyone approves the text, and every source starts
//	             unverified. Rejected with -live, so it cannot become a way to
//	             write unverified content.
//
// Everything except -live is a preview: it runs the usability gate, normalization,
// chunking, validation, and resume accounting, then stops before the provider is
// contacted and before anything is written.
//
// The manifest is a JSON array of ingestion documents. Structural detection
// (PDF/OCR/layout) is not part of this pipeline — it consumes already-anchored
// sections, so it can be run and reviewed without any real corpus present:
//
//	[
//	  {
//	    "id": "tabari-yusuf-dar-1410",
//	    "source_version": "dar-1410",
//	    "language": "ar",
//	    "content_type": "tafsir",
//	    "surah_id": 12,
//	    "sections": [
//	      {
//	        "ordinal": 1,
//	        "surah_id": 12,
//	        "ayah_start": 1,
//	        "ayah_end": 1,
//	        "mapping_type": "single_ayah",
//	        "text": "..."
//	      }
//	    ]
//	  }
//	]
//
// Config comes from the environment (EMBEDDING_API_URL, EMBEDDING_API_KEY,
// MONGO_URI), optionally via a .env file. The api key is never logged.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/joho/godotenv"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"tadbor/backend/internal/embedding"
	"tadbor/backend/internal/ingestion"
)

func main() {
	var (
		list            = flag.Bool("list", false, "list sources with their ingestion status, then exit")
		revalidate      = flag.Bool("revalidate", false, "re-apply the registry gate to a source's stored chunks, then exit")
		sourceID        = flag.String("source", "", "source registry id to ingest")
		manifest        = flag.String("manifest", "", "path to the source's JSON document manifest")
		live            = flag.Bool("live", false, "call the embedding provider and write to MongoDB")
		prune           = flag.Bool("prune", false, "delete stored chunks missing from the manifest")
		batch           = flag.Int("batch", embedding.DefaultMaxBatchSize, "embedding batch size")
		allowUnverified = flag.Bool("allow-unverified", false, "preview a source that has not cleared the registry gate; rejected with -live")
	)
	flag.Parse()

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

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	registry := ingestion.NewSourceRegistry(db)

	if *list {
		if err := listSources(ctx, registry); err != nil {
			log.Fatal(err)
		}
		return
	}

	if *sourceID == "" {
		log.Fatal("-source is required (or use -list)")
	}

	// A revalidation needs no provider: it only re-applies the gate.
	if *revalidate {
		updated, err := ingestion.NewService(db, nil).Revalidate(ctx, *sourceID)
		if err != nil {
			log.Fatal(err)
		}
		log.Printf("revalidated %s: %d chunk(s) updated", *sourceID, updated)
		return
	}

	docs, err := loadManifest(*manifest)
	if err != nil {
		log.Fatal(err)
	}
	if len(docs) == 0 {
		log.Printf("no documents supplied — running the gate and index checks only")
	}

	// The embedder is only built for a live run, so a preview needs no provider
	// credentials at all.
	var embedder embedding.Embedder
	if *live {
		// The client's batch size is aligned with the service's so that one
		// Embed call is exactly one provider request, keeping the reported call
		// count honest.
		client, err := embedding.NewClientWithOptions(
			os.Getenv("EMBEDDING_API_URL"),
			os.Getenv("EMBEDDING_API_KEY"),
			embedding.Options{MaxBatchSize: *batch},
		)
		if err != nil {
			log.Fatal(err)
		}
		embedder = client
	}

	// -allow-unverified exists so a verse mapping can be reviewed before the text
	// is approved, which is the state every source starts in. It must never be a
	// way to write unverified content, so combining it with -live is refused here
	// rather than quietly ignored deeper in the service.
	if *allowUnverified && *live {
		log.Fatal("-allow-unverified is a preview-only flag; it cannot be combined with -live")
	}

	report, err := ingestion.NewService(db, embedder).IngestSource(ctx, *sourceID, docs, ingestion.IngestOptions{
		BatchSize:         *batch,
		PruneStale:        *prune,
		DryRun:            !*live,
		PreviewUnverified: *allowUnverified,
	})
	if err != nil {
		log.Printf("ingest failed: %v", err)
		printReport(report)
		os.Exit(1)
	}
	printReport(report)
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

// loadManifest reads the document manifest. A missing file is treated as an empty
// manifest rather than an error: before a corpus exists, "nothing to ingest" is
// the expected state.
func loadManifest(path string) ([]ingestion.Document, error) {
	if path == "" {
		return nil, nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read manifest: %w", err)
	}
	if len(raw) == 0 {
		return nil, nil
	}

	var docs []ingestion.Document
	if err := json.Unmarshal(raw, &docs); err != nil {
		return nil, fmt.Errorf("parse manifest %s: %w", path, err)
	}
	return docs, nil
}

// listSources prints the registry alongside each source's gate verdict, which is
// the first thing to check before running an ingest.
func listSources(ctx context.Context, registry *ingestion.SourceRegistry) error {
	sources, err := registry.List(ctx)
	if err != nil {
		return err
	}
	if len(sources) == 0 {
		log.Print("no sources registered yet")
		return nil
	}
	for _, s := range sources {
		usable, err := registry.IsUsable(ctx, s.ID)
		if err != nil {
			return err
		}
		status := s.IngestionStatus
		if status == "" {
			status = ingestion.StatusNotStarted
		}
		log.Printf("%-24s tier=%d verified=%-9v licensing=%-8s ingestion=%-11s %s",
			s.ID, s.AuthorityTier, s.VerificationStatus, s.LicensingStatus, status, usableWord(usable))
	}
	return nil
}

func usableWord(usable bool) string {
	if usable {
		return "USABLE"
	}
	return "not usable"
}

// printReport emits the run's audit record as JSON so it can be diffed between
// runs or piped into another tool.
func printReport(report ingestion.IngestReport) {
	out, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		log.Printf("report: %+v", report)
		return
	}
	log.Printf("report: %s", out)
}
