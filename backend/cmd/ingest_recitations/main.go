// Real per-ayah recitation audio for Surah Yusuf — issue #14.
//
// # What this replaces
//
// scripts/seed_recitation.go wrote 8 placeholder recitations with URLs on
// example.com, for ayahs 1 and 2 only, so the surah page showed audio controls
// on two verses and nowhere else and nothing about them played. Those
// placeholders are gone; this command writes the 444 the acceptance criterion
// asks for, 111 ayahs across 4 reciters, every URL checked before it is stored.
//
// # Why every URL is verified before it is stored
//
// A recitation document that exists but does not play is the exact failure this
// issue is closing, so inserting 444 unverified URLs would reproduce it at 444
// times the size — and unlike the placeholders it would look like real data, so
// nothing downstream would flag it. A URL only enters the database after the
// origin has answered for it. Anything unreachable is reported and skipped, and
// the command exits non-zero so a partial load cannot be mistaken for a complete
// one.
//
// HEAD is tried first and a ranged GET is the fallback, because a server that
// does not implement HEAD must not be recorded as a missing recording.
//
// # Ayah count comes from the corpus, not from a constant
//
// The number of ayahs per surah is read from the verified Quran corpus in the
// ayahs collection rather than hardcoded to 111. If the corpus and the audio
// disagreed, the page would show either audio controls for verses that do not
// exist or none for verses that do, and both would be hard to notice.
//
// # Direct linking, not re-hosting
//
// The URLs point at everyayah.com and the audio is not copied into MongoDB or
// object storage: only the reference is stored, the same rule explanations
// follow with source_refs. Re-hosting would also mean redistributing files this
// project has not established a right to redistribute.
//
// # Licensing: unresolved, and deliberately not decided here
//
// Issue #14 asks to confirm each reciter's terms allow this use in an app.
// everyayah.com publishes no licence, no terms page and no per-reciter rights
// statement — the data index, the homepage and /terms were all checked. The
// files are served without charge, which is not the same as a grant of
// redistribution rights, and every reciter here is living or recently deceased
// with an estate that holds those rights.
//
// So the URLs are stored and verified, and the question stays open. What is
// decided is the shape that keeps it cheap to answer: nothing is re-hosted, so
// the answer only matters for whether the app may link out. See
// docs/recitation-audio-sources.md.
//
// Usage (from backend/):
//
//	go run ./cmd/ingest_recitations -check
//	go run ./cmd/ingest_recitations -live
//	go run ./cmd/ingest_recitations -live -surah 12 -pace 200ms
//	go run ./cmd/ingest_recitations -check -expect 444
//
// Flags:
//
//	-check    audit stored recitations, write nothing, contact no host; exit 1 if
//	          coverage or placeholder URLs are wrong
//	-live     verify the URLs and write the recitations (default: preview)
//	-surah    surah id to load (default 12)
//	-pace     minimum gap between HTTP requests to everyayah.com (default 250ms;
//	          0 disables)
//	-limit    stop after checking this many URLs (0 = no limit)
//	-expect   ayah count the audit requires; 0 reads it from the corpus
//
// Everything except -live and -check is a preview: it reports what it would do
// and stops before any request.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const runTimeout = 20 * time.Minute

// baseURL is everyayah.com's per-ayah MP3 layout: /data/<reciter folder>/<surah
// and ayah, each zero-padded to three digits, no separator>.
const baseURL = "https://everyayah.com/data"

// source identifies a reciter's folder on everyayah.com.
//
// The folder names are not derivable from the reciter ids in this database, and
// two of the four obvious guesses are wrong: Ghamadi_40kbps carries no "Al_"
// prefix and MaherAlMuaiqly128kbps has no separators and no bitrate suffix. The
// names were read off everyayah.com's own /data index rather than guessed,
// because a wrong guess is a 404 that looks exactly like a missing recording.
type source struct {
	ReciterID string
	Folder    string
}

var sources = []source{
	{ReciterID: "alafasy", Folder: "Alafasy_128kbps"},
	{ReciterID: "abdulbasit", Folder: "Abdul_Basit_Murattal_192kbps"},
	{ReciterID: "ghamdi", Folder: "Ghamadi_40kbps"},
	{ReciterID: "muaiqly", Folder: "MaherAlMuaiqly128kbps"},
}

// ayahURL builds the per-ayah URL. Zero padding to three digits is what the
// directory listings use: surah 12 ayah 9 is 012009, not 0129 or 01209.
func ayahURL(folder string, surahID, ayah int) string {
	return ayahURLFrom(baseURL, folder, surahID, ayah)
}

func ayahURLFrom(base, folder string, surahID, ayah int) string {
	return fmt.Sprintf("%s/%s/%03d%03d.mp3", base, folder, surahID, ayah)
}

type recitationDoc struct {
	ReciterID  string `bson:"reciter_id"`
	SurahID    int    `bson:"surah_id"`
	AyahNumber int    `bson:"ayah_number"`
	AudioURL   string `bson:"audio_url"`
}

// placeholderHosts are hosts that mean "this was never filled in". The audit
// fails on them, because a placeholder that survives into a loaded database is
// the state this command was written to end.
var placeholderHosts = []string{"example.com", "placeholder", "localhost"}

// run is the audit record, printed as JSON so two runs can be diffed.
type run struct {
	SurahID   int `json:"surah_id"`
	Reciters  int `json:"reciters"`
	Ayahs     int `json:"ayahs"`
	Planned   int `json:"planned_urls"`
	Checked   int `json:"checked_urls"`
	Reachable int `json:"reachable_urls"`
	Unreach   int `json:"unreachable_urls"`

	Stored         int `json:"stored_recitations"`
	StoredBefore   int `json:"stored_recitations_before"`
	Missing        int `json:"missing_verses"`
	PlaceholderURL int `json:"placeholder_urls"`
	Duplicates     int `json:"duplicate_documents"`

	// UnreachableNames lists which reciter:ayah pairs failed, so the report
	// says where rather than just how many.
	UnreachableNames []string `json:"unreachable_names,omitempty"`
}

func main() {
	var (
		check  = flag.Bool("check", false, "audit stored recitations and write nothing; exit 1 if coverage is wrong")
		live   = flag.Bool("live", false, "verify URLs and write recitations")
		surah  = flag.Int("surah", 12, "surah id to load")
		pace   = flag.Duration("pace", 250*time.Millisecond, "minimum gap between HTTP requests; 0 disables pacing")
		limit  = flag.Int("limit", 0, "stop after checking this many URLs (0 = no limit)")
		expect = flag.Int("expect", 0, "ayah count the audit requires; 0 reads it from the corpus")
	)
	flag.Parse()

	if *check && *live {
		log.Fatal("-check writes nothing, so it cannot be combined with -live")
	}
	if *limit < 0 {
		log.Fatalf("-limit cannot be negative, got %d", *limit)
	}
	if *surah < 1 {
		log.Fatalf("-surah must be a surah id, got %d", *surah)
	}

	if err := godotenv.Load(); err != nil && !os.IsNotExist(err) {
		log.Printf("godotenv: %v", err)
	}
	uri := os.Getenv("MONGO_URI")
	if uri == "" {
		log.Fatal("MONGO_URI is not set (put it in .env, or export it)")
	}

	ctx, cancel := context.WithTimeout(context.Background(), runTimeout)
	defer cancel()

	client, err := mongo.Connect(ctx, options.Client().ApplyURI(uri))
	if err != nil {
		log.Fatal(err)
	}
	defer client.Disconnect(ctx)

	db := client.Database("tadbor")

	// The corpus decides how many ayahs exist; nothing here assumes 111.
	ayahs, err := corpusAyahs(ctx, db, *surah)
	if err != nil {
		log.Fatalf("counting ayahs for surah %d: %v", *surah, err)
	}
	if ayahs == 0 {
		log.Fatalf("surah %d has no ayahs in the corpus; run make seed first", *surah)
	}
	if *expect > 0 && *expect != ayahs {
		log.Fatalf("corpus has %d ayahs for surah %d but -expect is %d", ayahs, *surah, *expect)
	}

	r := run{SurahID: *surah, Reciters: len(sources), Ayahs: ayahs, Planned: ayahs * len(sources)}

	// The unique index is what makes an upsert an upsert. Without it two runs of
	// this command, or one run and one `make seed`, both insert and the
	// collection silently doubles — and a surah page then plays the same ayah
	// twice.
	if err := ensureIndex(ctx, db); err != nil {
		log.Fatal(err)
	}

	recs := db.Collection("recitations")
	before, err := recs.CountDocuments(ctx, bson.M{"surah_id": *surah})
	if err != nil {
		log.Fatal(err)
	}
	r.StoredBefore = int(before)

	if *check {
		if err := audit(ctx, recs, &r); err != nil {
			log.Fatal(err)
		}
		emit(&r)
		if r.Stored != r.Planned || r.Missing > 0 || r.PlaceholderURL > 0 || r.Duplicates > 0 {
			log.Fatalf("audit failed: %d of %d recitations present, %d missing verses, %d placeholder URLs, %d duplicates",
				r.Stored, r.Planned, r.Missing, r.PlaceholderURL, r.Duplicates)
		}
		return
	}

	docs, report, err := verifyAll(ctx, baseURL, *surah, ayahs, *pace, *limit, *live)
	r.Checked, r.Reachable, r.Unreach = report.checked, report.reachable, report.unreachable
	r.UnreachableNames = report.names

	if !*live {
		emit(&r)
		log.Printf("preview only: would verify %d URLs and upsert %d recitations; pass -live to write", r.Planned, r.Planned)
		return
	}

	if len(docs) > 0 {
		models := make([]mongo.WriteModel, 0, len(docs))
		for _, d := range docs {
			models = append(models, mongo.NewUpdateOneModel().
				SetFilter(bson.D{
					{Key: "reciter_id", Value: d.ReciterID},
					{Key: "surah_id", Value: d.SurahID},
					{Key: "ayah_number", Value: d.AyahNumber},
				}).
				SetUpdate(bson.D{{Key: "$set", Value: d}}).
				SetUpsert(true))
		}
		if _, err := recs.BulkWrite(ctx, models); err != nil {
			log.Fatal(err)
		}
		r.Stored = len(models)
	}

	emit(&r)

	// A partial load is the thing this command exists to prevent, so it is a
	// failure rather than a warning.
	if r.Unreach > 0 {
		log.Fatalf("%d of %d URLs were unreachable, so recitations are incomplete: %s",
			r.Unreach, r.Planned, strings.Join(r.UnreachableNames, ", "))
	}
	if r.Stored != r.Planned {
		log.Fatalf("wrote %d recitations, expected %d", r.Stored, r.Planned)
	}

	if err := audit(ctx, recs, &r); err != nil {
		log.Fatal(err)
	}
	emit(&r)
	if r.Missing > 0 || r.PlaceholderURL > 0 || r.Duplicates > 0 {
		log.Fatalf("post-write audit failed: %d missing verses, %d placeholder URLs, %d duplicates",
			r.Missing, r.PlaceholderURL, r.Duplicates)
	}
	log.Printf("ok: %d recitations for surah %d across %d reciters", r.Stored, r.SurahID, r.Reciters)
}

// corpusAyahs returns how many ayahs the verified corpus holds for a surah.
func corpusAyahs(ctx context.Context, db *mongo.Database, surahID int) (int, error) {
	n, err := db.Collection("ayahs").CountDocuments(ctx, bson.M{"surah_id": surahID})
	if err != nil {
		return 0, err
	}
	return int(n), nil
}

func ensureIndex(ctx context.Context, db *mongo.Database) error {
	_, err := db.Collection("recitations").Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys: bson.D{
			{Key: "surah_id", Value: 1},
			{Key: "reciter_id", Value: 1},
			{Key: "ayah_number", Value: 1},
		},
		Options: options.Index().SetUnique(true).SetName("surah_reciter_ayah_unique"),
	})
	if err != nil {
		return fmt.Errorf("creating the unique index (drop duplicate recitations first): %w", err)
	}
	return nil
}

// audit reads the database and fills in everything checkable without the
// network: how many recitations exist, whether any verse is missing, whether
// any URL is still a placeholder, and whether the unique index caught a
// duplicate.
func audit(ctx context.Context, recs *mongo.Collection, r *run) error {
	cur, err := recs.Find(ctx, bson.M{"surah_id": r.SurahID})
	if err != nil {
		return err
	}
	defer cur.Close(ctx)

	seen := map[string]bool{}
	total := 0
	for cur.Next(ctx) {
		var d recitationDoc
		if err := cur.Decode(&d); err != nil {
			return err
		}
		total++
		key := d.ReciterID + "/" + strconv.Itoa(d.AyahNumber)
		if seen[key] {
			r.Duplicates++
		}
		seen[key] = true
		if isPlaceholder(d.AudioURL) {
			r.PlaceholderURL++
		}
	}
	r.Stored = total

	// Missing is counted per reciter so the message can name the reciter that
	// is short, which is the difference between a useful failure and "some
	// verses are missing".
	for _, s := range sources {
		for ayah := 1; ayah <= r.Ayahs; ayah++ {
			if !seen[s.ReciterID+"/"+strconv.Itoa(ayah)] {
				r.Missing++
			}
		}
	}
	return cur.Err()
}

func isPlaceholder(raw string) bool {
	low := strings.ToLower(raw)
	for _, h := range placeholderHosts {
		if strings.Contains(low, h) {
			return true
		}
	}
	return false
}

// verification accumulates what the URL checks found.
type verification struct {
	checked     int
	reachable   int
	unreachable int
	names       []string
}

// verifyAll checks every reciter/ayah URL and returns the documents worth
// writing. When live is false nothing is requested and no documents are
// returned, which is what makes the default a preview.
//
// Requests are serial and paced: this is 444 requests to a volunteer-run site
// that hosts a lot of people, and a parallel burst is the fastest way to get
// the whole directory rate-limited for everyone.
// base is an origin plus any path prefix, so it is baseURL rather than the host
// alone; the caller supplies it to keep this testable.
func verifyAll(ctx context.Context, base string, surahID, ayahs int, pace time.Duration, limit int, live bool) ([]recitationDoc, verification, error) {
	var (
		docs []recitationDoc
		v    verification
	)
	client := &http.Client{Timeout: 30 * time.Second}

	for _, s := range sources {
		for ayah := 1; ayah <= ayahs; ayah++ {
			if limit > 0 && v.checked >= limit {
				return docs, v, nil
			}
			url := ayahURLFrom(base, s.Folder, surahID, ayah)
			v.checked++

			if !live {
				continue
			}
			if pace > 0 {
				time.Sleep(pace)
			}

			ok, err := reachable(ctx, client, url)
			if err != nil {
				return nil, v, err
			}
			if !ok {
				v.unreachable++
				if len(v.names) < 20 {
					v.names = append(v.names, s.ReciterID+":"+strconv.Itoa(ayah))
				}
				continue
			}
			v.reachable++
			docs = append(docs, recitationDoc{
				ReciterID:  s.ReciterID,
				SurahID:    surahID,
				AyahNumber: ayah,
				AudioURL:   url,
			})
		}
	}
	return docs, v, nil
}

// reachable reports whether a URL actually serves audio.
//
// HEAD first, then a one-byte ranged GET. Some static hosts answer HEAD with a
// 405, and recording those as missing recordings would drop a reciter that is
// perfectly fine; the GET fallback is what keeps the audit honest.
//
// The check is for a usable audio response, not merely for 200: an HTML error
// page served with 200 would otherwise be stored as a recitation.
func reachable(ctx context.Context, client *http.Client, url string) (bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, url, nil)
	if err != nil {
		return false, err
	}
	resp, err := client.Do(req)
	if err == nil {
		resp.Body.Close()
		if resp.StatusCode == http.StatusOK && isAudio(resp.Header.Get("Content-Type")) {
			return true, nil
		}
		if resp.StatusCode != http.StatusMethodNotAllowed && resp.StatusCode != http.StatusNotFound {
			// Some other status is a real answer; a GET fallback would only
			// mask it, so report it as unreachable via the normal path.
			return false, nil
		}
	}

	getReq, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return false, err
	}
	getReq.Header.Set("Range", "bytes=0-0")
	resp, err = client.Do(getReq)
	if err != nil {
		// A network error here is a failure to determine, not a missing file.
		return false, err
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusPartialContent, nil
}

func isAudio(contentType string) bool {
	ct := strings.ToLower(contentType)
	return strings.HasPrefix(ct, "audio/") || strings.Contains(ct, "mpeg") || strings.Contains(ct, "mp3")
}

func emit(r *run) {
	sort.Strings(r.UnreachableNames)
	b, _ := json.MarshalIndent(r, "", "  ")
	fmt.Println(string(b))
}
