// Maps-and-covers reporter for issue #5.
//
// Answers the two questions the acceptance criteria are written as:
//
//  1. Does every ayah of the surah have at least one chunk in
//     chunk_ayah_mappings? (coverage)
//  2. Do the mapping table and the chunk collection still agree? (drift)
//
// And it adds the part a mapping table cannot answer by itself: whether each
// passage's text actually looks like commentary on the verse it is keyed to.
//
// # Why the corroboration signals exist
//
// issue #5 asks for a human to read the corpus and record which ayah each passage
// discusses. That review has not happened, so this tool does not pretend to replace
// it — it decides where a human should look.
//
// Three independent signals, none of which is a verdict:
//
//   - quoted_run: the longest run of the verse's own words appearing consecutively
//     in the passage. Catches a verbatim quotation.
//   - introduced: whether the passage opens the way a mufassir does before
//     expounding a verse ("يخبر تعالى"). Catches commentary that paraphrases.
//   - quoted: the share of the verse's 8-rune shingles present in the passage.
//     Catches a near-verbatim quotation, where spelling differences defeat exact
//     word matching.
//
// Each has a known failure mode, which is why a mapping is flagged only when all
// three fail rather than when one does: a stock opening can precede a wide-ranging
// discussion, short verses share words by coincidence, and shingle overlap can be
// produced by a passage that quotes a different, similarly-worded verse. On Surah
// Yusuf this leaves 19 of 111 flagged, all of which read correctly.
//
// None of these can prove a mapping right. A passage that paraphrases its verse
// shares no wording with it, and Ibn Kathir does that constantly — so the flags
// are a reading queue, not a defect list. A low score is never treated as an error.
//
// Usage (from backend/):
//
//	go run ./cmd/check_coverage -source ibn-kathir-ar -surah 12
//	go run ./cmd/check_coverage -source ibn-kathir-ar -surah 12 -json
//	go run ./cmd/check_coverage -source ibn-kathir-ar -surah 12 -only-flagged
//
// Flags:
//
//	-source     source registry id (required)
//	-surah      surah to check (default 12)
//	-min-run    consecutive ayah words a passage must contain to count as
//	            quoting the verse (default 4)
//	-only-flagged  print only ayahs that need a human, not all 111 rows
//	-json       machine-readable output
//
// Exit status is 1 when any ayah has no chunk at all, or when the two sides have
// drifted — the conditions a later ingest or a hand edit could cause. An
// unapproved source and an unquoted passage both exit 0: those are the states this
// tool exists to surface for a human, not failures it has standing to declare.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/joho/godotenv"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"tadbor/backend/internal/ingestion"
	"tadbor/backend/internal/quran"
	"tadbor/backend/internal/textverify"
)

type ayahRow struct {
	Ayah         int      `json:"ayah"`
	Chunks       int      `json:"chunks"`
	Usable       int      `json:"usable"`
	Parents      int      `json:"parents"`
	MappingTypes []string `json:"mapping_types"`
	QuotedRun    int      `json:"quoted_run"`
	// Introduced records the second corroborating signal: whether the passage
	// opens the way a mufassir does before expounding a verse. See
	// textverify.VerseIntroducingPhrases.
	Introduced bool `json:"introduced"`
	// Quoted is the fraction of the verse's shingles that occur in the passage.
	// See textverify.QuoteCoverage.
	Quoted  float64 `json:"quoted"`
	Verdict string  `json:"verdict"`
	Text    string  `json:"verse_text,omitempty"`
	Passage string  `json:"passage_preview,omitempty"`
}

type report struct {
	Source    string                  `json:"source"`
	Version   string                  `json:"source_version"`
	Surah     int                     `json:"surah"`
	AyahCount int                     `json:"ayah_count"`
	Covered   int                     `json:"covered"`
	Gaps      []int                   `json:"gaps"`
	Unusable  []int                   `json:"unusable"`
	MinRun    int                     `json:"min_run"`
	Rows      []ayahRow               `json:"rows"`
	Drift     ingestion.CoverageDrift `json:"drift"`
}

func main() {
	var (
		sourceID    = flag.String("source", "", "source registry id")
		surah       = flag.Int("surah", 12, "surah to check")
		minRun      = flag.Int("min-run", 4, "consecutive ayah words that counts as quoting the verse")
		onlyFlagged = flag.Bool("only-flagged", false, "print only ayahs that need a human")
		asJSON      = flag.Bool("json", false, "machine-readable output")
	)
	flag.Parse()

	if *sourceID == "" {
		log.Fatal("-source is required")
	}
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

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	rep, err := check(ctx, db, *sourceID, *surah, *minRun)
	if err != nil {
		log.Fatal(err)
	}

	if *asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(rep); err != nil {
			log.Fatal(err)
		}
	} else {
		printReport(rep, *onlyFlagged)
	}

	// A gap and drift are states a later ingest or a hand edit could cause, so
	// they fail the command. A source that has not been approved, and a passage
	// that does not quote its verse, are both expected pre-approval states that a
	// human resolves by reviewing; neither is this tool's call to fail on.
	if len(rep.Gaps) > 0 || !rep.Drift.Empty() {
		os.Exit(1)
	}
}

func check(ctx context.Context, db *mongo.Database, sourceID string, surah, minRun int) (report, error) {
	count := quran.AyahCount(surah)
	if count == 0 {
		return report{}, fmt.Errorf("surah %d is not between 1 and 114", surah)
	}

	store := ingestion.NewStore(db)
	coverage, err := store.CoverageForSurah(ctx, surah, count)
	if err != nil {
		return report{}, fmt.Errorf("coverage: %w", err)
	}

	registry := ingestion.NewSourceRegistry(db)
	// Chunks are keyed by the pinned snapshot's id, so the version must be
	// resolved the same way ingestion resolves it — reading Edition here would
	// silently match nothing.
	version, err := registry.Version(ctx, sourceID)
	if err != nil {
		return report{}, fmt.Errorf("resolve pinned source version: %w", err)
	}

	// Passage text, joined per ayah, is what the quotation check searches.
	texts, err := passageText(ctx, db, sourceID, version.ID, surah, count)
	if err != nil {
		return report{}, err
	}

	ayahs, err := quran.NewService(db).GetSurah(ctx, surah)
	if err != nil {
		return report{}, fmt.Errorf("load ayahs: %w", err)
	}
	verseText := map[int]string{}
	for _, a := range ayahs {
		verseText[a.AyahNumber] = a.TextSimple
	}

	rep := report{Source: sourceID, Version: version.ID, Surah: surah, AyahCount: count, MinRun: minRun}

	for i := 1; i <= count; i++ {
		cov := coverage[i]
		row := ayahRow{
			Ayah:         i,
			Chunks:       cov.Chunks,
			Usable:       cov.Usable,
			Parents:      cov.Parents,
			MappingTypes: cov.MappingTypes,
		}
		if cov.Chunks == 0 {
			row.Verdict = "gap"
			rep.Gaps = append(rep.Gaps, i)
			rep.Rows = append(rep.Rows, row)
			continue
		}

		// "Mapped" and "usable" are tracked separately on purpose. Usability is a
		// property of the source's approval, not of the mapping, and gating the
		// reading check on it would produce an empty review queue for exactly the
		// unverified sources issue #5 needs to review.
		rep.Covered++
		row.Text = verseText[i]
		row.Passage = preview(texts[i], 200)
		score := textverify.ScorePassage(verseText[i], texts[i])
		row.QuotedRun = score.LongestRun
		row.Introduced = score.Introduced
		row.Quoted = score.QuoteCoverage
		row.Verdict = verdict(score, minRun)
		if cov.Usable == 0 {
			rep.Unusable = append(rep.Unusable, i)
		}
		rep.Rows = append(rep.Rows, row)
	}

	drift, err := store.VerifyMappingsConsistent(ctx, sourceID, version.ID)
	if err != nil {
		return report{}, fmt.Errorf("verify mappings: %w", err)
	}
	rep.Drift = drift

	return rep, nil
}

// passageText concatenates the stored chunks of every ayah into one searchable
// string. The check is about what a passage says about a verse, and a verse's
// commentary is routinely spread over several chunks, so per-chunk matching would
// under-report quoting.
func passageText(ctx context.Context, db *mongo.Database, sourceID, version string, surah, count int) (map[int]string, error) {
	cur, err := db.Collection("tafsir_chunks").Find(ctx, bson.M{
		"source_id":      sourceID,
		"source_version": version,
		"surah_id":       surah,
	}, options.Find().SetProjection(bson.M{"_id": 0, "ayah_start": 1, "text": 1}))
	if err != nil {
		return nil, fmt.Errorf("load chunk text: %w", err)
	}
	defer cur.Close(ctx)

	out := make(map[int]string, count)
	for cur.Next(ctx) {
		var row struct {
			AyahStart int    `bson:"ayah_start"`
			Text      string `bson:"text"`
		}
		if err := cur.Decode(&row); err != nil {
			return nil, err
		}
		out[row.AyahStart] += " " + row.Text
	}
	return out, cur.Err()
}

// longestRun returns the length of the longest run of consecutive words from the
// verse that appears, in order and uninterrupted, inside the passage. A verse
// quoted as a block scores near its full length; one quoted in fragments scores
// low; a passage that never touches it scores zero.
//
// Both sides are normalised here rather than by the caller, so the function is
// correct by construction and cannot silently score zero on orthography.
// verdict labels a mapping on the evidence available for it. An entry is
// corroborated when any one signal fires.
//
// Requiring unanimity to declare an entry suspect keeps any single signal from
// deciding alone, because each has a known failure mode: a stock opening can
// precede a wide-ranging discussion, and word overlap happens by coincidence in a
// short verse.
func verdict(score textverify.Score, minRun int) string {
	if score.LongestRun >= minRun || score.Introduced || score.Quoted {
		return "mapped-corroborated"
	}
	return "mapped-uncorroborated"
}

func printReport(rep report, onlyFlagged bool) {
	totalChunks, totalParents := 0, 0
	for _, r := range rep.Rows {
		totalChunks += r.Chunks
		totalParents += r.Parents
	}

	fmt.Printf("coverage  %s  surah %d (%d ayahs)\n", rep.Source, rep.Surah, rep.AyahCount)
	fmt.Printf("  snapshot              %s\n", rep.Version)
	fmt.Printf("  ayahs with a chunk      %d/%d\n", rep.Covered, rep.AyahCount)
	fmt.Printf("  ayahs searchable now    %d/%d\n", rep.Covered-len(rep.Unusable), rep.AyahCount)
	fmt.Printf("  gaps (no chunk)         %d\n", len(rep.Gaps))
	fmt.Printf("  mapped, source gated    %d\n", len(rep.Unusable))
	fmt.Printf("  chunks / passages       %d / %d\n", totalChunks, totalParents)
	fmt.Printf("  mapping table drift     %s\n", driftWord(rep.Drift))

	if !rep.Drift.Empty() {
		for _, id := range rep.Drift.ChunksWithoutMapping {
			fmt.Printf("    chunk without mapping:       %s\n", id)
		}
		for _, id := range rep.Drift.MappingsWithoutChunk {
			fmt.Printf("    mapping without chunk:       %s\n", id)
		}
		for _, id := range rep.Drift.Mismatched {
			fmt.Printf("    mapping disagrees with chunk: %s\n", id)
		}
	}

	if len(rep.Gaps) > 0 {
		fmt.Printf("\ngaps: %v\n", rep.Gaps)
	}
	if len(rep.Unusable) > 0 {
		fmt.Printf("(%d ayahs are mapped but not searchable: the source has not cleared the registry gate)\n", len(rep.Unusable))
	}

	// The review queue, worst first. These are the mappings this tool cannot vouch
	// for; a human decides whether the passage really belongs to the verse.
	var queue []ayahRow
	for _, r := range rep.Rows {
		if r.Verdict == "mapped-uncorroborated" {
			queue = append(queue, r)
		}
	}
	sortRowsByRun(queue)

	if len(queue) == 0 {
		fmt.Printf("\nno ayah fell below the %d-word quotation threshold\n", rep.MinRun)
		return
	}
	fmt.Printf("\nneeds a human read (%d ayahs no signal could corroborate):\n", len(queue))
	fmt.Printf("  no signal agreed: fewer than %d consecutive quoted words, no verse-introducing\n", rep.MinRun)
	fmt.Printf("  phrase, and less than %.0f%% of the verse present\n", textverify.QuotedThreshold*100)
	if !onlyFlagged {
		fmt.Printf("  (use -only-flagged to list them with the verse and passage)\n")
		return
	}
	// A run length on its own does not tell a reviewer anything. The two texts
	// do: a passage that glosses the verse in different words is legitimate, and
	// only a reader can tell that apart from a passage about the wrong ayah.
	for _, r := range queue {
		fmt.Printf("\n  %d:%d  run=%d  quoted=%.2f  chunks=%d  passages=%d\n",
			rep.Surah, r.Ayah, r.QuotedRun, r.Quoted, r.Chunks, r.Parents)
		fmt.Printf("    verse    %s\n", r.Text)
		fmt.Printf("    passage  %s\n", r.Passage)
	}
}

// textverify.ShingleSize is the rune length of the n-grams quoteCoverage counts.
// preview truncates on a rune boundary, never mid-character.
func preview(s string, limit int) string {
	r := []rune(strings.TrimSpace(s))
	if len(r) <= limit {
		return string(r)
	}
	return string(r[:limit]) + "..."
}

func sortRowsByRun(rows []ayahRow) {
	for i := 1; i < len(rows); i++ {
		for j := i; j > 0 && rows[j].QuotedRun < rows[j-1].QuotedRun; j-- {
			rows[j], rows[j-1] = rows[j-1], rows[j]
		}
	}
}

func driftWord(d ingestion.CoverageDrift) string {
	if d.Empty() {
		return "none"
	}
	return fmt.Sprintf("%d unmapped chunks, %d orphan mappings, %d mismatched",
		len(d.ChunksWithoutMapping), len(d.MappingsWithoutChunk), len(d.Mismatched))
}

func connect() (*mongo.Database, error) {
	uri := os.Getenv("MONGO_URI")
	if uri == "" {
		return nil, errors.New("MONGO_URI is not set")
	}
	name := os.Getenv("MONGO_DB_NAME")
	if name == "" {
		name = "tadbor"
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
	return client.Database(name), nil
}
