package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/joho/godotenv"

	"tadbor/backend/internal/generation"
	"tadbor/backend/internal/introduction"
	"tadbor/backend/internal/platform"
)

const runTimeout = 10 * time.Minute

func main() {
	live := flag.Bool("live", false, "call the configured model and persist a pending draft")
	surahID := flag.Int("surah", introduction.PilotSurahID, "pilot surah (only 12 is supported)")
	style := flag.String("style", "simplified_ar", "introduction output style")
	level := flag.String("level", "beginner", "reader level")
	flag.Parse()

	if *surahID != introduction.PilotSurahID {
		log.Fatalf("only surah %d is supported", introduction.PilotSurahID)
	}
	if err := godotenv.Load(); err != nil && !os.IsNotExist(err) {
		log.Printf("godotenv: %v", err)
	}
	if os.Getenv("MONGO_URI") == "" {
		log.Fatal("MONGO_URI is not set")
	}
	db, err := platform.NewMongoConnection()
	if err != nil {
		log.Fatalf("mongo: %v", err)
	}
	defer platform.CloseMongoConnection(db)

	ctx, cancel := context.WithTimeout(context.Background(), runTimeout)
	defer cancel()
	evidenceSource := introduction.NewMongoEvidenceSource(db)
	evidence, err := evidenceSource.LoadEvidence(ctx, *surahID)
	if err != nil {
		log.Fatalf("verified evidence unavailable: %v", err)
	}
	if len(evidence) == 0 {
		log.Fatal(introduction.ErrInsufficientEvidence)
	}
	if !*live {
		fmt.Printf("preview: surah=%d style=%s level=%s verified_chunks=%d; no model call or writes; pass -live to persist a pending draft\n",
			*surahID, *style, *level, len(evidence))
		return
	}

	store := introduction.NewMongoDraftStore(db)
	if err := store.EnsureIndexes(ctx); err != nil {
		log.Fatalf("introduction index: %v", err)
	}
	pipeline := introduction.NewPipeline(evidenceSource, generation.NewService(), store)
	draft, err := pipeline.Run(ctx, *surahID, *style, *level)
	if err != nil {
		log.Fatalf("introduction generation failed: %v", err)
	}
	fmt.Printf("pending review: id=%s surah=%d style=%s sections=%d source_refs=%d\n",
		draft.ID, draft.SurahID, draft.Style, len(draft.Sections), len(draft.SourceRefs))
}
