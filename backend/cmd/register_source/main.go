// Tadbor source-registry writer for issue #3.
//
// Applies a reviewed source record to MongoDB: one `sources` document, one
// `source_versions` document, and one `tafsir_documents` row (Part 2 §24).
//
// Usage (from backend/):
//
//	go run ./cmd/register_source -file ../corpus/sources/ibn-kathir-ar.json
//	go run ./cmd/register_source -file ../corpus/sources/ibn-kathir-ar.json -check
//	go run ./cmd/register_source -list
//
// Flags:
//
//	-file   registry record to apply (required unless -list)
//	-check  report what is stored and whether it still matches; write nothing
//	-list   list registered sources with their state, then exit
//
// # Why the record is a file rather than flags
//
// authority_tier, licensing_status, and verification_status are human judgements
// about a specific published work, not derivable from anything. Keeping them in a
// reviewed JSON file under corpus/ puts them in version control, where a change
// to a licensing claim is visible as a diff, instead of buried in a command
// invocation that leaves no trace.
//
// # Why the content hash is computed here
//
// `source_versions.content_hash` records which text this edition actually is.
// Typing it by hand would defeat the purpose: a hash nobody computed is a claim,
// not a check. It is derived from the raw text file, so re-running after upstream
// text changes reports a different hash and the change is visible.
//
// # This does not verify anything
//
// Writing a registry record makes a source *known*, not *usable*. IsUsable still
// requires verification_status == "verified" alongside licensing_status ==
// "cleared", and issue #3 deliberately registers the source as unverified. Until
// a reviewer flips that, retrieval excludes every chunk from it — which is the
// intended state, not a bug to work around.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"tadbor/backend/internal/ingestion"
)

type registryRecord struct {
	Source         ingestion.Source `json:"source"`
	Version        versionRecord    `json:"version"`
	TafsirDocument documentRecord   `json:"tafsir_document"`
}

// versionRecord mirrors `source_versions` from Part 2 §24.
type versionRecord struct {
	ID           string `json:"id"`
	Edition      string `json:"edition"`
	VersionLabel string `json:"version_label"`
	RawTextRef   string `json:"raw_text_ref"`
}

// documentRecord mirrors `tafsir_documents`, which points at the raw text a
// given source version was digitised from.
type documentRecord struct {
	ID          string `json:"id"`
	ContentType string `json:"content_type"`
	SurahID     int    `json:"surah_id"`
	AyahCount   int    `json:"ayah_count"`
}

func main() {
	var (
		file  = flag.String("file", "", "registry record to apply")
		check = flag.Bool("check", false, "report stored state without writing")
		list  = flag.Bool("list", false, "list registered sources, then exit")
		root  = flag.String("repo-root", "..", "repo root, for resolving repo-relative paths")
	)
	flag.Parse()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	client, err := mongo.Connect(ctx, options.Client().ApplyURI(mongoURI()))
	if err != nil {
		log.Fatalf("connect: %v", err)
	}
	defer client.Disconnect(context.Background())
	db := client.Database(dbName())

	if *list {
		if err := listSources(ctx, db); err != nil {
			log.Fatalf("list: %v", err)
		}
		return
	}
	if *file == "" {
		log.Fatal("-file is required (or use -list)")
	}

	rec, rawHash, err := loadRecord(*file, *root)
	if err != nil {
		log.Fatalf("%s: %v", *file, err)
	}

	if *check {
		report(ctx, db, rec, rawHash)
		return
	}
	if err := apply(ctx, db, rec, rawHash); err != nil {
		log.Fatalf("apply: %v", err)
	}
}

func mongoURI() string {
	if v := os.Getenv("MONGO_URI"); v != "" {
		return v
	}
	log.Fatal("MONGO_URI is not set")
	return ""
}

func dbName() string {
	if v := os.Getenv("MONGO_DB_NAME"); v != "" {
		return v
	}
	return "tadbor"
}

// loadRecord reads the registry record and hashes the raw text it points at.
// Hashing here rather than at write time means the stored hash always describes
// the bytes that exist on disk.
func loadRecord(path, root string) (registryRecord, string, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return registryRecord{}, "", err
	}
	var rec registryRecord
	if err := json.Unmarshal(body, &rec); err != nil {
		return registryRecord{}, "", fmt.Errorf("decode: %w", err)
	}
	if err := validate(rec); err != nil {
		return registryRecord{}, "", err
	}

	rawPath, err := resolve(root, rec.Version.RawTextRef)
	if err != nil {
		return registryRecord{}, "", fmt.Errorf("raw text %s: %w", rec.Version.RawTextRef, err)
	}
	raw, err := os.ReadFile(rawPath)
	if err != nil {
		return registryRecord{}, "", fmt.Errorf("raw text %s: %w", rec.Version.RawTextRef, err)
	}
	if len(raw) == 0 {
		return registryRecord{}, "", fmt.Errorf("raw text %s is empty", rec.Version.RawTextRef)
	}
	sum := sha256.Sum256(raw)
	return rec, hex.EncodeToString(sum[:]), nil
}

func validate(rec registryRecord) error {
	if rec.Source.ID == "" {
		return fmt.Errorf("source.id is required")
	}
	if rec.Source.Title == "" {
		return fmt.Errorf("source.title is required")
	}
	if rec.Source.Author == "" {
		return fmt.Errorf("source.author is required")
	}
	if rec.Source.Language == "" {
		return fmt.Errorf("source.language is required")
	}
	switch rec.Source.AuthorityTier {
	case 1, 2, 3:
	default:
		return fmt.Errorf("source.authority_tier must be 1, 2, or 3, got %d", rec.Source.AuthorityTier)
	}
	switch rec.Source.LicensingStatus {
	case ingestion.LicensingCleared:
	default:
		// A source that is not rights-cleared may be registered — it just cannot
		// be used — but the record has to say so deliberately.
		if rec.Source.LicensingStatus == "" {
			return fmt.Errorf("source.licensing_status is required (the gate compares against %q)", ingestion.LicensingCleared)
		}
	}
	switch rec.Source.VerificationStatus {
	case ingestion.VerificationVerified, "unverified":
	default:
		return fmt.Errorf("source.verification_status must be %q or %q, got %q",
			ingestion.VerificationVerified, "unverified", rec.Source.VerificationStatus)
	}
	if rec.Source.Edition == "" {
		return fmt.Errorf("source.edition is required: ingestion defaults every chunk's source_version from it")
	}
	if rec.Version.ID == "" {
		return fmt.Errorf("version.id is required")
	}
	if rec.Version.Edition == "" {
		return fmt.Errorf("version.edition is required")
	}
	if rec.Version.RawTextRef == "" {
		return fmt.Errorf("version.raw_text_ref is required")
	}
	if rec.TafsirDocument.ID == "" {
		return fmt.Errorf("tafsir_document.id is required")
	}
	if rec.TafsirDocument.SurahID < 1 {
		return fmt.Errorf("tafsir_document.surah_id must be >= 1, got %d", rec.TafsirDocument.SurahID)
	}
	return nil
}

// resolve turns a repo-relative path into one readable from the working
// directory, which is backend/ for every command in this project.
func resolve(root, ref string) (string, error) {
	if filepath.IsAbs(ref) {
		return ref, nil
	}
	if _, err := os.Stat(ref); err == nil {
		return ref, nil
	}
	joined := filepath.Join(root, ref)
	if _, err := os.Stat(joined); err != nil {
		return "", fmt.Errorf("cannot find %s (tried %s and %s)", ref, ref, joined)
	}
	return joined, nil
}

func apply(ctx context.Context, db *mongo.Database, rec registryRecord, rawHash string) error {
	now := time.Now().UTC().Format(time.RFC3339)

	src := rec.Source
	src.IngestionStatus = ingestion.StatusNotStarted
	if src.RawTextRef == "" {
		src.RawTextRef = rec.Version.RawTextRef
	}
	if _, err := db.Collection("sources").ReplaceOne(ctx, bson.M{"_id": src.ID}, src,
		options.Replace().SetUpsert(true)); err != nil {
		return fmt.Errorf("sources: %w", err)
	}

	version := bson.M{
		"_id":           rec.Version.ID,
		"source_id":     src.ID,
		"edition":       rec.Version.Edition,
		"version_label": rec.Version.VersionLabel,
		"content_hash":  rawHash,
		"created_at":    now,
	}
	if _, err := db.Collection("source_versions").ReplaceOne(ctx,
		bson.M{"_id": rec.Version.ID}, version,
		options.Replace().SetUpsert(true)); err != nil {
		return fmt.Errorf("source_versions: %w", err)
	}

	doc := bson.M{
		"_id":               rec.TafsirDocument.ID,
		"source_version_id": rec.Version.ID,
		"raw_text_ref":      rec.Version.RawTextRef,
		"ingestion_status":  ingestion.StatusNotStarted,
		"content_type":      rec.TafsirDocument.ContentType,
		"surah_id":          rec.TafsirDocument.SurahID,
		"ayah_count":        rec.TafsirDocument.AyahCount,
		"created_at":        now,
	}
	if _, err := db.Collection("tafsir_documents").ReplaceOne(ctx,
		bson.M{"_id": rec.TafsirDocument.ID}, doc,
		options.Replace().SetUpsert(true)); err != nil {
		return fmt.Errorf("tafsir_documents: %w", err)
	}

	fmt.Printf("registered %s\n", src.ID)
	fmt.Printf("  sources            %s (%s, tier %d)\n", src.Title, src.VerificationStatus, src.AuthorityTier)
	fmt.Printf("  source_versions    %s  edition=%s\n", rec.Version.ID, rec.Version.Edition)
	fmt.Printf("  raw text           %s\n", rec.Version.RawTextRef)
	fmt.Printf("  sha256             %s\n", rawHash)
	fmt.Printf("  tafsir_documents   %s\n", rec.TafsirDocument.ID)

	usable, err := ingestion.NewSourceRegistry(db).IsUsable(ctx, src.ID)
	if err != nil {
		return fmt.Errorf("gate check: %w", err)
	}
	if usable {
		fmt.Printf("\n  the gate reports this source as USABLE\n")
	} else {
		fmt.Printf("\n  the gate reports this source as not usable, which is the intended state\n")
		fmt.Printf("  for issue #3: retrieval will exclude its chunks until a reviewer sets\n")
		fmt.Printf("  verification_status to %q.\n", ingestion.VerificationVerified)
	}
	return nil
}

func report(ctx context.Context, db *mongo.Database, rec registryRecord, rawHash string) {
	var src ingestion.Source
	if err := db.Collection("sources").FindOne(ctx, bson.M{"_id": rec.Source.ID}).Decode(&src); err != nil {
		if err == mongo.ErrNoDocuments {
			log.Fatalf("FAIL: no sources document with _id %q", rec.Source.ID)
		}
		log.Fatalf("sources: %v", err)
	}

	var version bson.M
	err := db.Collection("source_versions").FindOne(ctx, bson.M{"_id": rec.Version.ID}).Decode(&version)
	if err != nil {
		log.Fatalf("FAIL: no source_versions document with _id %q: %v", rec.Version.ID, err)
	}

	fmt.Printf("%s\n", rec.Source.ID)
	fmt.Printf("  title              %s\n", src.Title)
	fmt.Printf("  authority_tier     %d\n", src.AuthorityTier)
	fmt.Printf("  licensing_status   %s\n", src.LicensingStatus)
	fmt.Printf("  verification_status %s\n", src.VerificationStatus)
	fmt.Printf("  ingestion_status   %s\n", src.IngestionStatus)
	fmt.Printf("  edition            %s\n", src.Edition)
	fmt.Printf("  raw_text_ref       %s\n", src.RawTextRef)
	fmt.Printf("  stored hash        %v\n", version["content_hash"])

	storedHash, _ := version["content_hash"].(string)
	if storedHash != rawHash {
		log.Fatalf("FAIL: stored content_hash does not match %s\n  stored:  %s\n  on disk: %s",
			rec.Version.RawTextRef, storedHash, rawHash)
	}

	var doc bson.M
	if err := db.Collection("tafsir_documents").FindOne(ctx,
		bson.M{"_id": rec.TafsirDocument.ID}).Decode(&doc); err != nil {
		if err == mongo.ErrNoDocuments {
			log.Fatalf("FAIL: no tafsir_documents document with _id %q", rec.TafsirDocument.ID)
		}
		log.Fatalf("tafsir_documents: %v", err)
	}
	fmt.Printf("  tafsir_document    %s\n", rec.TafsirDocument.ID)
	fmt.Printf("\n  PASS: the registry matches the file on disk.\n")
}

func listSources(ctx context.Context, db *mongo.Database) error {
	cur, err := db.Collection("sources").Find(ctx, bson.M{},
		options.Find().SetSort(bson.D{{Key: "_id", Value: 1}}))
	if err != nil {
		return err
	}
	defer cur.Close(ctx)

	var out []bson.M
	if err := cur.All(ctx, &out); err != nil {
		return err
	}
	if len(out) == 0 {
		fmt.Println("no sources registered")
		return nil
	}
	fmt.Printf("%d registered source(s):\n", len(out))
	for _, s := range out {
		fmt.Printf("  %-22s tier %v  %-11s %-11s ingestion=%v\n",
			fmt.Sprint(s["_id"]), s["authority_tier"], s["licensing_status"],
			s["verification_status"], s["ingestion_status"])
	}
	return nil
}
