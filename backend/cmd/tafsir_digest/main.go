// Command tafsir_digest builds a reviewable proof sheet for verifying a Tafsir
// source against a printed edition.
//
// It exists because source verification is a human judgement. A Tafsir's licence
// and edition can be checked by a machine; whether its text faithfully reproduces
// the edition cannot. So this tool's job is narrow and deliberately dull: put the
// exact bytes that would be ingested next to the exact verse they are keyed to,
// with provenance a reviewer can check, and get out of the way.
//
// It reads the committed manifest rather than the chunk collection, because a
// source must be verifiable before it is ingested. That also means the digest can
// reproduce the review queue that ingestion would later see, without asking anyone
// to trust a dropped preview database.
//
// Nothing here decides whether the text is correct. A low corroboration score
// means "a human should read this one", never "this one is wrong" — paraphrase is
// a legitimate and common feature of Tafsir. See internal/textverify.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"tadbor/backend/internal/ingestion"
	"tadbor/backend/internal/quran"
	"tadbor/backend/internal/textverify"
)

func main() {
	var (
		sourceID   = flag.String("source", "", "source registry id, e.g. ibn-kathir-ar")
		manifest   = flag.String("manifest", "", "path to the ingestion manifest to review")
		outPath    = flag.String("out", "", "write Markdown here instead of stdout")
		ayahList   = flag.String("ayahs", "", "comma-separated ayah numbers to include, e.g. 1,34,43. Ranges like 12-20 are allowed")
		uncorrob   = flag.Bool("only-uncorroborated", false, "include only entries no signal corroborates, i.e. the human review queue")
		minRun     = flag.Int("min-run", 4, "consecutive ayah words that counts as quoting the verse")
		allEntries = flag.Bool("all", false, "include every entry in the manifest, not just the review queue")
		verifyOnly = flag.Bool("verify", false, "print only the provenance header and counts, then exit")
	)
	flag.Parse()

	if *sourceID == "" {
		fatal(errors.New("-source is required"))
	}
	if *manifest == "" {
		fatal(errors.New("-manifest is required"))
	}
	if *uncorrob && *allEntries {
		fatal(errors.New("-only-uncorroborated and -all contradict each other"))
	}

	db, err := connect()
	if err != nil {
		fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	docs, err := readManifest(*manifest)
	if err != nil {
		fatal(err)
	}
	if len(docs) != 1 {
		fatal(fmt.Errorf("digest covers a single document per run; %s holds %d", *manifest, len(docs)))
	}
	doc := docs[0]

	registry := ingestion.NewSourceRegistry(db)
	src, err := registry.Get(ctx, *sourceID)
	if err != nil {
		fatal(fmt.Errorf("load source: %w", err))
	}
	version, err := registry.Version(ctx, *sourceID)
	if err != nil {
		fatal(fmt.Errorf("resolve pinned source version: %w", err))
	}
	if doc.SourceVersion != "" && doc.SourceVersion != version.ID {
		fatal(fmt.Errorf("manifest pins source_version %q but the registry's current version for %s is %q",
			doc.SourceVersion, *sourceID, version.ID))
	}

	// The verse text must be the same verified corpus the chunker will compare
	// against, so its version and hash go in the header for the record.
	verses, corpus, err := readVerses(ctx, db, doc.SurahID)
	if err != nil {
		fatal(err)
	}

	entries := make([]entry, 0, len(doc.Sections))
	for _, sec := range doc.Sections {
		verse, ok := verses[sec.AyahStart]
		if !ok {
			fatal(fmt.Errorf("section %d maps to %d:%d but that ayah is not in the verse corpus",
				sec.Ordinal, sec.SurahID, sec.AyahStart))
		}
		score := textverify.ScorePassage(verse.simple, sec.Text)
		entries = append(entries, entry{
			section: sec,
			verse:   verse,
			score:   score,
			verdict: verdict(score, *minRun),
		})
	}

	var queue []entry
	for _, e := range entries {
		if e.verdict == "mapped-uncorroborated" {
			queue = append(queue, e)
		}
	}

	selected, err := selectEntries(entries, queue, *ayahList, *uncorrob, *allEntries)
	if err != nil {
		fatal(err)
	}

	md := render(header{
		sourceID:     *sourceID,
		source:       src,
		version:      version,
		surah:        doc.SurahID,
		manifestPath: *manifest,
		corpus:       corpus,
		minRun:       *minRun,
		total:        len(entries),
		queue:        len(queue),
		selected:     len(selected),
		mode:         modeName(*uncorrob, *allEntries, *ayahList),
	}, selected)

	if *verifyOnly {
		fmt.Print(headerOnly(md))
		return
	}
	if *outPath == "" {
		fmt.Print(md)
		return
	}
	if err := os.WriteFile(*outPath, []byte(md), 0o644); err != nil {
		fatal(fmt.Errorf("write %s: %w", *outPath, err))
	}
	fmt.Fprintf(os.Stderr, "wrote %s: %d entries (%d of %d uncorroborated)\n", *outPath, len(selected), len(queue), len(entries))
}

type verseText struct {
	number  int
	uthmani string
	simple  string
}

type entry struct {
	section ingestion.Section
	verse   verseText
	score   textverify.Score
	verdict string
}

// verdict mirrors check_coverage so both tools agree on what needs a human. It
// lives in two packages because each is a command; if they ever disagree the
// digest would under-report the queue, which is the failure that matters.
func verdict(score textverify.Score, minRun int) string {
	if score.LongestRun >= minRun || score.Introduced || score.Quoted {
		return "mapped-corroborated"
	}
	return "mapped-uncorroborated"
}

func modeName(uncorrob, all bool, ayahs string) string {
	switch {
	case all:
		return "every entry"
	case ayahs != "":
		return "an explicit ayah list"
	case uncorrob:
		return "the uncorroborated review queue"
	default:
		return "the review queue"
	}
}

// selectEntries decides which entries the sheet contains. The default is the
// review queue, which is the whole point: these are the entries no signal could
// vouch for, so they are the ones a printed edition has to settle.
//
// 12:1 belongs in that queue rather than in a special case. Its commentary opens
// with the surah preamble before reaching the verse, so it scores 0.00 shingle
// coverage on its own ayah — the sheet falls out of the evidence instead of
// needing an exception, and an exception would have quietly duplicated it.
func selectEntries(entries, queue []entry, ayahs string, uncorrob, all bool) ([]entry, error) {
	switch {
	case all:
		return entries, nil

	case ayahs != "":
		want, err := parseAyahList(ayahs)
		if err != nil {
			return nil, err
		}
		byNumber := map[int]entry{}
		for _, e := range entries {
			byNumber[e.verse.number] = e
		}
		var picked []entry
		for _, n := range want {
			e, ok := byNumber[n]
			if !ok {
				return nil, fmt.Errorf("ayah %d is not in the manifest", n)
			}
			picked = append(picked, e)
		}
		return picked, nil

	default:
		return queue, nil
	}
}

func parseAyahList(s string) ([]int, error) {
	var out []int
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if lo, hi, ok := strings.Cut(part, "-"); ok {
			a, err := strconv.Atoi(strings.TrimSpace(lo))
			if err != nil {
				return nil, fmt.Errorf("bad ayah range %q", part)
			}
			b, err := strconv.Atoi(strings.TrimSpace(hi))
			if err != nil {
				return nil, fmt.Errorf("bad ayah range %q", part)
			}
			for i := a; i <= b; i++ {
				out = append(out, i)
			}
			continue
		}
		n, err := strconv.Atoi(part)
		if err != nil {
			return nil, fmt.Errorf("bad ayah %q", part)
		}
		out = append(out, n)
	}
	sort.Ints(out)
	return out, nil
}

type header struct {
	sourceID     string
	source       *ingestion.Source
	version      *ingestion.SourceVersion
	surah        int
	manifestPath string
	corpus       provenance
	minRun       int
	total        int
	queue        int
	selected     int
	mode         string
}

// stringMeta reads one descriptive field off the source record. The registry keeps
// these as free-form metadata (licensing basis, digitisation, upstream ids) and
// they are worth showing: the digitisation note in particular decides what a
// reviewer can honestly conclude.
func stringMeta(src *ingestion.Source, key string) string {
	if src == nil || src.Metadata == nil {
		return ""
	}
	return fmt.Sprint(src.Metadata[key])
}

type provenance struct {
	version string
	hash    string
}

func (h header) render() string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Verification sheet: %s, Surah %d\n\n", h.source.Title, h.surah)

	fmt.Fprintf(&b, "Generated by `cmd/tafsir_digest` on %s. This file is evidence for a\n",
		time.Now().UTC().Format("2006-01-02"))
	b.WriteString("human decision. No part of it asserts the text is correct.\n\n")

	b.WriteString("## What is being checked\n\n")
	b.WriteString("Registration already established that the licence is clear and that the\n")
	b.WriteString("corpus is complete. Neither of those depends on a human reading Arabic.\n")
	b.WriteString("What is left is whether this Arabic is faithful Ibn Kathir, and that\n")
	b.WriteString("takes a reader.\n\n")
	b.WriteString("- **Reviewer**: read each passage and decide whether it is genuine Ibn Kathir\n")
	b.WriteString("  expounding the ayah it is keyed to.\n")
	b.WriteString("- **Not in scope**: mapping correctness (covered separately by\n")
	b.WriteString("  `cmd/check_coverage`) and chunk boundaries.\n\n")

	b.WriteString("### What is already established, and by what\n\n")
	b.WriteString("These are machine-checkable and already pass, so they are not the\n")
	b.WriteString("reviewer's job. They are listed because a verification is only meaningful\n")
	b.WriteString("if it is clear what was taken on trust.\n\n")
	b.WriteString("| established | check |\n|---|---|\n")
	b.WriteString("| the committed file is byte-identical to what quran.com serves | `cmd/fetch_tafsir -check` |\n")
	b.WriteString("| the manifest is a faithful parse of that file | `cmd/build_manifest -check` |\n")
	b.WriteString("| the registry's hash pins those exact bytes | `cmd/register_source -check` |\n")
	b.WriteString("| every ayah matches its hash and three independent sources | `cmd/seed_quran -check` |\n")
	b.WriteString("| all 111 ayahs of the surah are covered | `cmd/check_coverage` |\n\n")

	b.WriteString("### What this sheet cannot establish\n\n")
	if d := stringMeta(h.source, "digitisation"); d != "" {
		fmt.Fprintf(&b, "The registry records this text's origin as: *%s*.\n\n", d)
	}
	fmt.Fprintf(&b, "So the reviewer's comparison is **not** against a printing called\n")
	fmt.Fprintf(&b, "`%s` — that is the name of a website and its API resource, not a\n", h.version.Edition)
	b.WriteString("printed edition. Nothing in this repository ties quran.com's Arabic to any\n")
	b.WriteString("specific printing, and a verification that does not record which printing it\n")
	b.WriteString("compared against cannot be repeated by anyone else.\n\n")
	b.WriteString("That makes two separate judgements, and the sheet asks for both:\n\n")
	b.WriteString("1. **Fidelity** — is this Arabic Ibn Kathir, and not a paraphrase, an\n")
	b.WriteString("   abridgement, or another mufassir? Compare against any print edition of\n")
	b.WriteString("   Tafsir Ibn Kathir you hold, and record which one below.\n")
	b.WriteString("2. **Attribution** — is each passage about the ayah beside it? That is\n")
	b.WriteString("   `cmd/check_coverage`'s question, and it is a mapping judgement, not a\n")
	b.WriteString("   transcription one.\n\n")
	fmt.Fprintf(&b, "The print edition used for this review: ______________________________\n\n")
	fmt.Fprintf(&b, "Reviewer: ____________________  Date: ____________\n\n")

	b.WriteString("## Provenance\n\n")
	b.WriteString("| field | value |\n|---|---|\n")
	fmt.Fprintf(&b, "| source id | `%s` |\n", h.sourceID)
	fmt.Fprintf(&b, "| snapshot id | `%s` |\n", h.version.ID)
	fmt.Fprintf(&b, "| digitisation (edition id) | `%s` |\n", h.version.Edition)
	if h.version.VersionLabel != "" {
		fmt.Fprintf(&b, "| version label | %s |\n", h.version.VersionLabel)
	}
	fmt.Fprintf(&b, "| raw text | `%s` |\n", h.version.RawTextRef)
	fmt.Fprintf(&b, "| **source sha256** | `%s` |\n", h.version.ContentHash)
	fmt.Fprintf(&b, "| manifest | `%s` |\n", h.manifestPath)
	fmt.Fprintf(&b, "| verse corpus | `%s` |\n", h.corpus.version)
	fmt.Fprintf(&b, "| verse corpus sha256 (computed over this surah) | `%s` |\n", h.corpus.hash)
	fmt.Fprintf(&b, "| entries in manifest | %d |\n", h.total)
	fmt.Fprintf(&b, "| entries needing a human | %d |\n", h.queue)
	fmt.Fprintf(&b, "| entries in this sheet | %d (%s) |\n\n", h.selected, h.mode)

	fmt.Fprintf(&b, "Re-verify every claim above before trusting this sheet:\n\n```\ncd backend\n")
	if rid := stringMeta(h.source, "upstream_resource_id"); rid != "" {
		fmt.Fprintf(&b, "go run ./cmd/fetch_tafsir -tafsir-id %s -surah %d -check\n", rid, h.surah)
	}
	fmt.Fprintf(&b, "go run ./cmd/build_manifest -source %s -surah %d -check\n", h.sourceID, h.surah)
	fmt.Fprintf(&b, "go run ./cmd/register_source -file ../corpus/sources/%s.json -check\n", h.sourceID)
	b.WriteString("```\n\n")

	b.WriteString("## How corroboration is scored\n\n")
	fmt.Fprintf(&b, "Three independent signals decide which entries appear here. They measure\n")
	b.WriteString("*evidence that a passage really is about its ayah*, nothing more:\n\n")
	b.WriteString("| signal | reading |\n|---|---|\n")
	fmt.Fprintf(&b, "| quoted run | %d consecutive words of the ayah also occur in the passage, after orthography folding |\n", h.minRun)
	fmt.Fprintf(&b, "| shingle coverage | share of the ayah's %d-rune windows occurring in the passage; %.2f or more counts as quoted |\n",
		textverify.ShingleSize, textverify.QuotedThreshold)
	fmt.Fprintf(&b, "| verse introduction | passage opens with a stock formula such as `%s` |\n",
		strings.Join(textverify.VerseIntroducingPhrases, "`, `"))
	b.WriteString("\nA low score means *read it*, never *it is wrong*: paraphrase is a legitimate\n")
	b.WriteString("way to write Tafsir. It is also why this sheet can be short and still be\n")
	b.WriteString("complete — the entries below are the ones no signal could vouch for.\n\n")

	b.WriteString("## Marking\n\n")
	b.WriteString("For each entry, tick one:\n\n")
	b.WriteString("- `[x] faithful` — this is Ibn Kathir, and it is expounding the ayah above.\n")
	b.WriteString("- `[ ] suspect` — not his text, an abridgement, another author, or about a\n")
	b.WriteString("  different ayah. Say which, below.\n")
	b.WriteString("- `[ ] untranslated / modern paraphrase` — flag it; that is a real finding,\n")
	b.WriteString("  not a failure.\n\n")
	b.WriteString("An entry marked `suspect` blocks verification of the whole source until it is\n")
	b.WriteString("resolved, because a mixed source cannot be attributed to Ibn Kathir at all.\n\n")

	return b.String()
}

func headerOnly(md string) string {
	if i := strings.Index(md, "\n---\n\n"); i >= 0 {
		return md[:i]
	}
	return md
}

func render(h header, entries []entry) string {
	var b strings.Builder
	b.WriteString(h.render())

	b.WriteString("---\n\n")

	if len(entries) == 0 {
		b.WriteString("No entries selected.\n")
		return b.String()
	}

	fmt.Fprintf(&b, "# Entries (%d)\n\n", len(entries))
	for _, e := range entries {
		renderEntry(&b, h.surah, e)
	}

	b.WriteString("---\n\n# Reviewer's notes\n\n")
	b.WriteString("_Record any divergence from the printed edition here: surah, ayah, printed page,\n")
	b.WriteString("and what differs. An entry marked `differs` blocks verification until resolved._\n\n")

	return b.String()
}

func renderEntry(b *strings.Builder, surah int, e entry) {
	fmt.Fprintf(b, "## %d:%d\n\n", surah, e.verse.number)

	fmt.Fprintf(b, "- section: `%d`, mapping `%s`", e.section.Ordinal, e.section.MappingType)
	if e.section.AyahEnd != e.section.AyahStart {
		fmt.Fprintf(b, " (spans %d-%d)", e.section.AyahStart, e.section.AyahEnd)
	}
	fmt.Fprintf(b, "\n- evidence: %d-word quoted run, %.2f shingle coverage", e.score.LongestRun, e.score.QuoteCoverage)
	switch {
	case e.score.Introduced:
		b.WriteString(", introduces the verse explicitly")
	case e.score.Quoted:
		b.WriteString(", quotes the verse")
	default:
		b.WriteString(" — **no signal fired, so this entry needs a human**")
	}
	b.WriteString("\n\n")

	b.WriteString("### The ayah\n\n")
	b.WriteString("Uthmani (as printed in the Quran corpus):\n\n")
	fmt.Fprintf(b, "```\n%s\n```\n\n", e.verse.uthmani)
	if e.verse.simple != e.verse.uthmani {
		b.WriteString("Simplified (search only, never quote this as the Quran):\n\n")
		fmt.Fprintf(b, "```\n%s\n```\n\n", e.verse.simple)
	}

	fmt.Fprintf(b, "### The commentary (%d runes)\n\n", len([]rune(e.section.Text)))
	b.WriteString("```\n")
	b.WriteString(e.section.Text)
	b.WriteString("\n```\n\n")

	b.WriteString("- [ ] faithful\n")
	b.WriteString("- [ ] suspect: ______________________\n\n")
}

func readManifest(path string) ([]ingestion.Document, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read manifest: %w", err)
	}
	var docs []ingestion.Document
	if err := json.Unmarshal(raw, &docs); err != nil {
		return nil, fmt.Errorf("parse manifest: %w", err)
	}
	return docs, nil
}

func readVerses(ctx context.Context, db *mongo.Database, surah int) (map[int]verseText, provenance, error) {
	cur, err := db.Collection("ayahs").Find(ctx, bson.M{"surah_id": surah})
	if err != nil {
		return nil, provenance{}, fmt.Errorf("read ayahs: %w", err)
	}
	defer cur.Close(ctx)

	var rows []quran.Ayah
	if err := cur.All(ctx, &rows); err != nil {
		return nil, provenance{}, fmt.Errorf("read ayahs: %w", err)
	}
	if len(rows) == 0 {
		return nil, provenance{}, fmt.Errorf("no ayahs for surah %d in the verse corpus", surah)
	}

	out := make(map[int]verseText, len(rows))
	// One number that identifies the exact verse set this sheet was built
	// against. Individual ayahs carry their own content_hash, but quoting any one
	// of them would pin only one ayah, not the surah.
	sum := sha256.New()
	var prov provenance
	versions := map[string]bool{}
	for _, a := range rows {
		out[a.AyahNumber] = verseText{
			number:  a.AyahNumber,
			uthmani: a.TextUthmani,
			simple:  a.TextSimple,
		}
		versions[a.CorpusVersion] = true
		fmt.Fprintf(sum, "%d\t%s\n", a.AyahNumber, a.ContentHash)
	}
	if len(versions) != 1 {
		return nil, provenance{}, fmt.Errorf("surah %d mixes verse corpora: %v", surah, versions)
	}
	for v := range versions {
		prov.version = v
	}
	prov.hash = hex.EncodeToString(sum.Sum(nil))
	return out, prov, nil
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

func fatal(err error) {
	fmt.Fprintf(os.Stderr, "tafsir_digest: %v\n", err)
	os.Exit(1)
}
