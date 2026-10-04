package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"tadbor/backend/internal/ingestion"
)

// validRecord is a minimal record that passes, so each test can break exactly
// one field and be sure the failure is that field's fault.
func validRecord() registryRecord {
	return registryRecord{
		Source: ingestion.Source{
			ID:                 "ibn-kathir-ar",
			Title:              "تفسير ابن كثير",
			Author:             "ابن كثير",
			Language:           "ar",
			AuthorityTier:      1,
			LicensingStatus:    ingestion.LicensingCleared,
			VerificationStatus: "unverified",
			Edition:            "quran.com/ar-tafsir-ibn-kathir",
		},
		Version: versionRecord{
			ID:           "ibn-kathir-ar-qurancom-2026-10-04",
			Edition:      "quran.com/ar-tafsir-ibn-kathir",
			RawTextRef:   "corpus/tafsir/x.md",
			VersionLabel: "snapshot",
		},
		TafsirDocument: documentRecord{ID: "ibn-kathir-ar-surah-12", SurahID: 12},
	}
}

func TestValidateAcceptsACompleteRecord(t *testing.T) {
	if err := validate(validRecord()); err != nil {
		t.Fatalf("valid record rejected: %v", err)
	}
}

func TestValidateRejectsIncompleteRecords(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*registryRecord)
		want   string
	}{
		{"no source id", func(r *registryRecord) { r.Source.ID = "" }, "source.id"},
		{"no title", func(r *registryRecord) { r.Source.Title = "" }, "source.title"},
		{"no author", func(r *registryRecord) { r.Source.Author = "" }, "source.author"},
		{"no language", func(r *registryRecord) { r.Source.Language = "" }, "source.language"},
		{"tier out of range", func(r *registryRecord) { r.Source.AuthorityTier = 4 }, "authority_tier"},
		{"tier zero", func(r *registryRecord) { r.Source.AuthorityTier = 0 }, "authority_tier"},
		{"no licensing status", func(r *registryRecord) { r.Source.LicensingStatus = "" }, "licensing_status"},
		// Unrecognised verification states are refused rather than stored: the
		// gate compares against "verified", so an invented value would read as
		// not-verified by accident instead of by decision.
		{"unknown verification status", func(r *registryRecord) { r.Source.VerificationStatus = "checked" }, "verification_status"},
		// ingestion defaults every chunk's source_version from Source.Edition, so
		// an empty edition would produce chunks with no version at all.
		{"no edition", func(r *registryRecord) { r.Source.Edition = "" }, "source.edition"},
		{"no version id", func(r *registryRecord) { r.Version.ID = "" }, "version.id"},
		{"no version edition", func(r *registryRecord) { r.Version.Edition = "" }, "version.edition"},
		{"no raw text ref", func(r *registryRecord) { r.Version.RawTextRef = "" }, "version.raw_text_ref"},
		{"no document id", func(r *registryRecord) { r.TafsirDocument.ID = "" }, "tafsir_document.id"},
		{"bad surah", func(r *registryRecord) { r.TafsirDocument.SurahID = 0 }, "surah_id"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := validRecord()
			tc.mutate(&rec)
			err := validate(rec)
			if err == nil {
				t.Fatal("accepted an invalid record")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %q does not mention %q", err, tc.want)
			}
		})
	}
}

// A source may legitimately be registered before its rights are cleared, but the
// empty string must not stand in for that decision.
func TestValidateAllowsAnUnclearedSourceWithAnExplicitStatus(t *testing.T) {
	rec := validRecord()
	rec.Source.LicensingStatus = "pending rights clearance"
	if err := validate(rec); err != nil {
		t.Fatalf("an explicitly un-cleared source should still register: %v", err)
	}
}

// The stored content_hash must describe the bytes on disk, so a hand-typed or
// stale hash has to be caught.
func TestLoadRecordHashesTheFileItPointsAt(t *testing.T) {
	dir := t.TempDir()
	raw := writeRaw(t, dir, "# tafsir\n\nنص\n")

	path := filepath.Join(dir, "record.json")
	writeRecord(t, path, validRecord())

	_, hash, err := loadRecord(path, dir)
	if err != nil {
		t.Fatalf("loadRecord: %v", err)
	}

	// sha256 of exactly those bytes.
	want := sha256Hex("# tafsir\n\nنص\n")
	if hash != want {
		t.Fatalf("hash = %s, want %s", hash, want)
	}

	// Changing the raw text must change the hash, or drift would go unnoticed.
	if err := os.WriteFile(raw, []byte("# tafsir\n\nنص مختلف\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, changed, err := loadRecord(path, dir)
	if err != nil {
		t.Fatalf("loadRecord: %v", err)
	}
	if changed == hash {
		t.Fatal("editing the raw text did not change the hash")
	}
}

func TestLoadRecordRejectsAMissingRawTextFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "record.json")
	writeRecord(t, path, validRecord())

	_, _, err := loadRecord(path, dir)
	if err == nil {
		t.Fatal("registered a source whose raw text does not exist")
	}
	if !strings.Contains(err.Error(), "raw text") {
		t.Fatalf("error %q does not mention the raw text", err)
	}
}

func TestLoadRecordRejectsAnEmptyRawTextFile(t *testing.T) {
	dir := t.TempDir()
	writeRaw(t, dir, "")
	path := filepath.Join(dir, "record.json")
	writeRecord(t, path, validRecord())

	if _, _, err := loadRecord(path, dir); err == nil {
		t.Fatal("registered a source with an empty raw text file")
	}
}

// resolve has to work both from backend/ and when given an absolute path, since
// the registry stores repo-relative refs but commands run inside backend/.
func TestResolveFindsRepoRelativePathsFromEitherDirectory(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "corpus", "tafsir"), 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dir, "corpus", "tafsir", "x.md")
	if err := os.WriteFile(target, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Running from backend/, where every command in this project is invoked: the
	// repo-relative ref has to resolve against -repo-root.
	got, err := resolve(dir, "corpus/tafsir/x.md")
	if err != nil {
		t.Fatalf("resolve from backend: %v", err)
	}
	if got != target {
		t.Errorf("resolve from backend = %q, want %q", got, target)
	}

	// And from the repo root, where the ref resolves as given rather than
	// through the repo-root prefix.
	restore := chdir(t, dir)
	got, err = resolve(dir, "corpus/tafsir/x.md")
	restore()
	if err != nil {
		t.Fatalf("resolve from repo root: %v", err)
	}
	if got != filepath.Join("corpus", "tafsir", "x.md") {
		t.Errorf("resolve from repo root = %q", got)
	}

	if _, err := resolve(dir, "corpus/tafsir/missing.md"); err == nil {
		t.Error("resolve accepted a path that does not exist")
	}
}

// chdir switches directory for the duration of a subtest and returns a function
// that switches back.
func chdir(t *testing.T, dir string) func() {
	t.Helper()
	prev, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	return func() {
		if err := os.Chdir(prev); err != nil {
			t.Fatal(err)
		}
	}
}

// writeRaw creates the file the record's raw_text_ref points at.
func writeRaw(t *testing.T, dir, body string) string {
	t.Helper()
	p := filepath.Join(dir, "corpus", "tafsir", "x.md")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func writeRecord(t *testing.T, path string, rec registryRecord) {
	t.Helper()
	body, err := jsonMarshal(rec)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatal(err)
	}
}
func jsonMarshal(v any) ([]byte, error) { return json.MarshalIndent(v, "", "  ") }

func sha256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}
