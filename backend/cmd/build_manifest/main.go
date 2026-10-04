// Tadbor manifest builder for issue #5.
//
// Turns a fetched Tafsir corpus file into the ingestion manifest that carries
// the verse mapping, so chunking has structure to consume instead of guessing it.
//
// Usage (from backend/):
//
//	go run ./cmd/build_manifest -source ibn-kathir-ar -surah 12
//	go run ./cmd/build_manifest -source ibn-kathir-ar -surah 12 -check
//
// Flags:
//
//	-source     source registry id (required)
//	-surah      surah the corpus file covers (default 12)
//	-registry   registry file (default ../corpus/sources/<source>.json)
//	-corpus     corpus file (default: the registry's raw_text_ref)
//	-out        manifest path (default ../corpus/manifests/<source>-surah-<n>.json)
//	-check      verify the manifest matches the corpus file; write nothing
//
// # Where the mapping comes from
//
// Part 1 §9 is explicit that chunk boundaries follow the Tafsir's own structure and
// that the chunker must never infer a verse mapping. So the mapping here is read
// off the `## surah:ayah` headings that fetch_tafsir wrote, which are themselves
// keys the upstream API supplied. Nothing is pattern-matched out of the Arabic and
// nothing is guessed from position: an entry's heading is the claim, and this
// command only carries it through.
//
// That makes the mapping as trustworthy as the source's own keying. It is not a
// human reading each passage and deciding what it discusses, which is what issue
// #5 asks for. check_coverage is the stand-in for that: it reports which ayahs
// have commentary that does and does not quote the verse it is keyed to, so the
// passages worth a human's attention are the ones flagged rather than all 111.
//
// # Mapping type
//
// Everything this file produces is single_ayah, because the source keys each
// passage to exactly one verse. Two kinds of passage would argue otherwise and are
// recorded in the source's metadata rather than silently reclassified here:
//
//   - 12:1 opens with the surah preamble and a hadith about teaching Surat Yusuf.
//     It is genuinely keyed to 12:1 by the source, so it is labelled single_ayah.
//     Calling it surah_level would be this tool overruling the source.
//   - Passages that quote several verses together stay single_ayah. Promoting them
//     to multi_ayah needs a human decision, and getting it wrong widens what a
//     verse lookup will return as exact evidence.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"tadbor/backend/internal/ingestion"
	"tadbor/backend/internal/quran"
)

// heading matches the ayah headings fetch_tafsir writes. Anchored, so a line of
// Arabic that happens to contain "12:1" cannot be mistaken for a heading.
var heading = regexp.MustCompile(`(?m)^##\s+(\d+):(\d+)\s*$`)

type options struct {
	sourceID string
	surah    int
	corpus   string
	out      string
}

func main() {
	var (
		sourceID = flag.String("source", "", "source registry id")
		surah    = flag.Int("surah", 12, "surah the corpus file covers")
		registry = flag.String("registry", "", "registry file (default ../corpus/sources/<source>.json)")
		corpus   = flag.String("corpus", "", "corpus file (default: the registry's raw_text_ref)")
		out      = flag.String("out", "", "manifest path (default ../corpus/manifests/<source>-surah-<n>.json)")
		check    = flag.Bool("check", false, "verify the manifest matches the corpus; write nothing")
	)
	flag.Parse()

	if *sourceID == "" {
		log.Fatal("-source is required")
	}
	if quran.AyahCount(*surah) == 0 {
		log.Fatalf("surah %d is not between 1 and 114", *surah)
	}

	opts := options{sourceID: *sourceID, surah: *surah}
	registryPath := *registry
	if registryPath == "" {
		registryPath = fmt.Sprintf("../corpus/sources/%s.json", *sourceID)
	}
	opts.corpus = *corpus
	if opts.corpus == "" {
		ref, err := rawTextRef(registryPath)
		if err != nil {
			log.Fatalf("%v", err)
		}
		// raw_text_ref is repository-root relative ("corpus/tafsir/..."). The
		// default registry sits at <root>/corpus/sources/<source>.json, so the
		// root is three directories up. This tool runs from backend/, hence the
		// relative paths.
		root := filepath.Dir(filepath.Dir(filepath.Dir(registryPath)))
		opts.corpus = filepath.Join(root, ref)
	}
	opts.out = *out
	if opts.out == "" {
		opts.out = fmt.Sprintf("../corpus/manifests/%s-surah-%d.json", *sourceID, *surah)
	}

	sections, err := parseCorpus(opts.corpus, opts.surah)
	if err != nil {
		log.Fatalf("%s: %v", opts.corpus, err)
	}

	doc := ingestion.Document{
		ID:          fmt.Sprintf("%s-surah-%d", opts.sourceID, opts.surah),
		Language:    "ar",
		ContentType: ingestion.ContentTypeTafsir,
		SurahID:     opts.surah,
		Sections:    sections,
	}
	manifest, err := json.MarshalIndent([]ingestion.Document{doc}, "", "  ")
	if err != nil {
		log.Fatal(err)
	}
	manifest = append(manifest, '\n')

	if *check {
		existing, err := os.ReadFile(opts.out)
		if err != nil {
			log.Fatalf("%v\n  the manifest does not exist yet — run without -check to write it", err)
		}
		if string(existing) != string(manifest) {
			log.Fatalf("FAIL: %s does not match %s.\n"+
				"  Either the corpus text changed or the manifest was edited by hand.\n"+
				"  Re-run without -check to accept the new mapping, and check the diff:\n"+
				"  a changed mapping means commentary now points at a different verse.",
				opts.out, opts.corpus)
		}
		fmt.Printf("PASS: %s matches %s (%d sections)\n", opts.out, opts.corpus, len(sections))
		return
	}

	if err := os.MkdirAll(filepath.Dir(opts.out), 0o755); err != nil {
		log.Fatal(err)
	}
	if err := os.WriteFile(opts.out, manifest, 0o644); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("wrote %s: %d sections, %d bytes\n", opts.out, len(sections), len(manifest))
	report(sections)
}

// rawTextRef reads the corpus path out of the reviewed registry file, so the
// manifest is built from the text the registry says this source actually is
// rather than from a filename convention that would drift from it.
func rawTextRef(registryPath string) (string, error) {
	raw, err := os.ReadFile(registryPath)
	if err != nil {
		return "", fmt.Errorf("%v (pass -corpus to build without a registry file)", err)
	}
	var rec struct {
		Source struct {
			ID         string `json:"id"`
			RawTextRef string `json:"raw_text_ref"`
		} `json:"source"`
	}
	if err := json.Unmarshal(raw, &rec); err != nil {
		return "", fmt.Errorf("parse %s: %w", registryPath, err)
	}
	if rec.Source.RawTextRef == "" {
		return "", fmt.Errorf("%s has no source.raw_text_ref", registryPath)
	}
	return rec.Source.RawTextRef, nil
}

// parseCorpus reads the ayah headings and their text back out of the corpus file.
//
// Every ayah of the surah must be present exactly once. A missing heading would
// leave a verse with no commentary while still reporting success, and a duplicate
// would give one verse two contradictory attributions.
func parseCorpus(path string, surah int) ([]ingestion.Section, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	matches := heading.FindAllStringSubmatchIndex(string(body), -1)
	if len(matches) == 0 {
		return nil, fmt.Errorf("no '## surah:ayah' headings found — is this a file fetch_tafsir wrote?")
	}

	var (
		sections = make([]ingestion.Section, 0, len(matches))
		seen     = map[int]bool{}
	)
	for i, m := range matches {
		key, err := strconv.Atoi(string(body[m[2]:m[3]]))
		if err != nil {
			return nil, fmt.Errorf("bad surah in heading %q", string(body[m[0]:m[1]]))
		}
		ayah, err := strconv.Atoi(string(body[m[4]:m[5]]))
		if err != nil {
			return nil, fmt.Errorf("bad ayah in heading %q", string(body[m[0]:m[1]]))
		}
		if key != surah {
			return nil, fmt.Errorf("heading %q is not for surah %d", string(body[m[0]:m[1]]), surah)
		}
		if ayah < 1 || ayah > quran.AyahCount(surah) {
			return nil, fmt.Errorf("heading %q is outside surah %d (1-%d)", string(body[m[0]:m[1]]), surah, quran.AyahCount(surah))
		}
		if seen[ayah] {
			return nil, fmt.Errorf("ayah %d:%d appears more than once in the corpus file", surah, ayah)
		}
		seen[ayah] = true

		// Text runs from just after this heading to the start of the next.
		textStart := m[1]
		textEnd := len(body)
		if i+1 < len(matches) {
			textEnd = matches[i+1][0]
		}
		text := strings.TrimSpace(string(body[textStart:textEnd]))
		if text == "" {
			return nil, fmt.Errorf("ayah %d:%d has no commentary text", surah, ayah)
		}

		sections = append(sections, ingestion.Section{
			Ordinal:     i,
			SurahID:     surah,
			AyahStart:   ayah,
			AyahEnd:     ayah,
			MappingType: ingestion.MappingSingleAyah,
			Text:        text,
		})
	}

	for ayah := 1; ayah <= quran.AyahCount(surah); ayah++ {
		if !seen[ayah] {
			return nil, fmt.Errorf("ayah %d:%d has no section — refusing to build a manifest with a silent gap", surah, ayah)
		}
	}

	sort.Slice(sections, func(i, j int) bool { return sections[i].AyahStart < sections[j].AyahStart })
	return sections, nil
}

// report prints the shape of what was written. A manifest that is mostly one-chunk
// sections hides the passages that will explode into many, and those are the ones
// that decide whether the LLM context budget holds.
func report(sections []ingestion.Section) {
	byPiece := map[int]int{}
	longest := ""
	for _, s := range sections {
		pieces := 0
		for _, para := range strings.Split(s.Text, "\n\n") {
			pieces += (len([]rune(para)) + ingestion.DefaultMaxChunkRunes - 1) / ingestion.DefaultMaxChunkRunes
		}
		byPiece[pieces]++
		if longest == "" || len([]rune(s.Text)) > len([]rune(longest)) {
			longest = s.Text
		}
	}
	multi := 0
	for _, n := range byPiece {
		if n > 1 {
			multi++
		}
	}
	fmt.Printf("  sections                %d\n", len(sections))
	fmt.Printf("  split into >1 chunk     %d sections\n", multi)
	fmt.Printf("  longest section         %d runes (ayah %d)\n", len([]rune(longest)), longestAyah(sections, longest))
	fmt.Printf("  est. chunks             ~%d\n", estimateChunks(sections))
}

func longestAyah(sections []ingestion.Section, text string) int {
	for _, s := range sections {
		if s.Text == text {
			return s.AyahStart
		}
	}
	return 0
}

func estimateChunks(sections []ingestion.Section) int {
	total := 0
	for _, s := range sections {
		for _, para := range strings.Split(s.Text, "\n\n") {
			runes := len([]rune(para))
			if runes == 0 {
				continue
			}
			total += (runes + ingestion.DefaultMaxChunkRunes - 1) / ingestion.DefaultMaxChunkRunes
		}
	}
	return total
}
