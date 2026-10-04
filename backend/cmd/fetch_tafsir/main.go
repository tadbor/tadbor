// Tadbor Tafsir fetcher for issue #3.
//
// Downloads the Arabic commentary on one surah from quran.com's per-ayah tafsir
// API and writes it to a plain-text file that is checked into the repository, so
// the raw text Day 2 chunks from is reviewable by a human without running any
// code and is not re-downloaded at build or ingest time.
//
// Usage (from backend/):
//
//	go run ./cmd/fetch_tafsir -surah 12 -tafsir-id 14
//	go run ./cmd/fetch_tafsir -surah 12 -tafsir-id 14 -check
//	go run ./cmd/fetch_tafsir -list
//
// Flags:
//
//	-list       list the Arabic tafsir editions the API offers, then exit
//	-surah      surah to fetch (default 12)
//	-tafsir-id  tafsir edition id (default 14, Tafsir Ibn Kathir)
//	-out        output path (default ../corpus/tafsir/<slug>-surah-<n>.md)
//	-check      re-fetch and compare against the file on disk; write nothing
//
// # Why the per-ayah endpoint
//
// The alternative sources for classical tafsir are continuous texts that need
// ayah markers parsed out of them, and a marker that is missed or misread
// silently attributes commentary to the wrong verse — which Part 1 §8 calls the
// single most important correctness property in the system. This API keys each
// passage to its verse server-side, so the mapping is a fact rather than an
// inference. Verse-level mapping is Day 2's job (issue #5); this command only
// preserves the keys.
//
// # Provenance
//
// The file carries a header naming the edition, the exact URL, the retrieval
// time, and the count. The text itself is not verified scripture and must not
// be treated as such: it is commentary. A source record (issue #3) is what
// licenses its use, and it starts out unverified.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"html"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"tadbor/backend/internal/quran"
)

const (
	defaultTafsirID = 14 // Tafsir Ibn Kathir
	apiBase         = "https://api.quran.com/api/v4"
	maxDownload     = 32 << 20
)

var httpClient = &http.Client{Timeout: 90 * time.Second}

func main() {
	var (
		list     = flag.Bool("list", false, "list the Arabic tafsir editions, then exit")
		surah    = flag.Int("surah", 12, "surah to fetch")
		tafsirID = flag.Int("tafsir-id", defaultTafsirID, "tafsir edition id")
		out      = flag.String("out", "", "output path (default ../corpus/tafsir/<slug>-surah-<n>.md)")
		check    = flag.Bool("check", false, "re-fetch and compare against the file on disk; write nothing")
	)
	flag.Parse()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	if *list {
		if err := listEditions(ctx); err != nil {
			log.Fatalf("list: %v", err)
		}
		return
	}

	if quran.AyahCount(*surah) == 0 {
		log.Fatalf("surah %d is not between 1 and 114", *surah)
	}

	entries, err := fetch(ctx, *tafsirID, *surah)
	if err != nil {
		log.Fatalf("fetch: %v", err)
	}

	path := *out
	if path == "" {
		path = defaultPath(ctx, *tafsirID, *surah)
	}

	rendered, err := render(ctx, *tafsirID, *surah, entries)
	if err != nil {
		log.Fatalf("render: %v", err)
	}

	if *check {
		existing, err := os.ReadFile(path)
		if err != nil {
			log.Fatalf("check: %v", err)
		}
		// Only the commentary body is compared. The header holds a retrieval
		// timestamp, so comparing the whole file would always fail.
		_, storedBody, ok := splitHeader(string(existing))
		if !ok {
			log.Fatalf("FAIL: %s has no %q separator, so it was not written by this command", path, strings.TrimSpace(headerSeparator))
		}
		_, freshBody, _ := splitHeader(rendered)
		if storedBody == freshBody {
			fmt.Printf("PASS: %s matches tafsir %d for surah %d (%d ayahs, %d bytes)\n",
				path, *tafsirID, *surah, len(entries), len(rendered))
			return
		}
		log.Fatalf("FAIL: the commentary in %s differs from what tafsir %d serves for surah %d now.\n"+
			"  The stored text is a snapshot; either the upstream text changed or the\n"+
			"  file was edited by hand. Re-run without -check to accept the new text, and\n"+
			"  record the change in the source_versions content_hash.", path, *tafsirID, *surah)
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		log.Fatalf("create %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(rendered), 0o644); err != nil {
		log.Fatalf("write %s: %v", path, err)
	}
	fmt.Printf("wrote %s: %d ayahs, %d bytes\n", path, len(entries), len(rendered))
}

// ---------------------------------------------------------------------------
// API
// ---------------------------------------------------------------------------

// edition is one commentary entry, as served per ayah.
type edition struct {
	ID         int    `json:"id"`
	ResourceID int    `json:"resource_id"`
	VerseKey   string `json:"verse_key"`
	LanguageID int    `json:"language_id"`
	Slug       string `json:"slug"`
	Text       string `json:"text"`
}

type editionList struct {
	ID         int    `json:"id"`
	Name       string `json:"name"`
	AuthorName string `json:"author_name"`
	Slug       string `json:"slug"`
}

func get(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	// Cloudflare answers Python's default urllib agent with a 403, and the
	// endpoint is public with no key, so identifying the client is enough.
	req.Header.Set("User-Agent", "tadbor-corpus-fetch/1.0")
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: HTTP %d", url, resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, maxDownload))
}

// fetch returns the commentary keyed to ayah number, having checked that every
// ayah of the surah is present exactly once and in order. A partial or
// out-of-order response is an error rather than something to write out: the
// whole point of the per-ayah endpoint is that the keys are trustworthy, and a
// silently short file would be indistinguishable from a surah with less
// commentary.
func fetch(ctx context.Context, tafsirID, surah int) (map[int]string, error) {
	url := fmt.Sprintf("%s/tafsirs/%d/by_chapter/%d?per_page=%d", apiBase, tafsirID, surah, quran.AyahCount(surah))
	body, err := get(ctx, url)
	if err != nil {
		return nil, err
	}
	var payload struct {
		Tafsirs    []edition `json:"tafsirs"`
		Pagination struct {
			TotalRecords int `json:"total_records"`
			TotalPages   int `json:"total_pages"`
		} `json:"pagination"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("decode tafsir response: %w", err)
	}

	want := quran.AyahCount(surah)
	if payload.Pagination.TotalRecords != 0 && payload.Pagination.TotalRecords != want {
		return nil, fmt.Errorf("the API reports %d records for surah %d, expected %d",
			payload.Pagination.TotalRecords, surah, want)
	}
	if len(payload.Tafsirs) != want {
		return nil, fmt.Errorf("got %d entries for surah %d, expected %d — refusing to write a partial file",
			len(payload.Tafsirs), surah, want)
	}

	out := make(map[int]string, want)
	for i, e := range payload.Tafsirs {
		// The key must match the entry's position, not merely be well formed.
		// Checking the key against itself proves nothing; checking it against
		// its index catches a transposition, a repeated ayah standing in for a
		// missing one, and an out-of-range key in one comparison.
		ayah := i + 1
		expected := fmt.Sprintf("%d:%d", surah, ayah)
		if e.VerseKey != expected {
			return nil, fmt.Errorf("entry %d has verse_key %q, expected %q — the response is not in surah order", i, e.VerseKey, expected)
		}
		cleaned := cleanHTML(e.Text)
		if cleaned == "" {
			return nil, fmt.Errorf("verse %s has no commentary text", e.VerseKey)
		}
		out[ayah] = cleaned
	}
	return out, nil
}

func listEditions(ctx context.Context) error {
	body, err := get(ctx, apiBase+"/resources/tafsirs?language=ar")
	if err != nil {
		return err
	}
	var payload struct {
		Tafsirs []editionList `json:"tafsirs"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return fmt.Errorf("decode edition list: %w", err)
	}
	if len(payload.Tafsirs) == 0 {
		return fmt.Errorf("the API listed no Arabic tafsir editions")
	}
	fmt.Printf("%d Arabic tafsir editions:\n", len(payload.Tafsirs))
	for _, t := range payload.Tafsirs {
		fmt.Printf("  %4d  %-28s  %s\n", t.ID, t.Name, t.AuthorName)
	}
	return nil
}

func fetchEditionMeta(ctx context.Context, tafsirID int) (editionList, error) {
	body, err := get(ctx, apiBase+"/resources/tafsirs")
	if err != nil {
		return editionList{}, err
	}
	var payload struct {
		Tafsirs []editionList `json:"tafsirs"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return editionList{}, fmt.Errorf("decode edition list: %w", err)
	}
	for _, t := range payload.Tafsirs {
		if t.ID == tafsirID {
			return t, nil
		}
	}
	return editionList{}, fmt.Errorf("tafsir id %d is not in the API's edition list", tafsirID)
}

// headerSeparator divides the provenance header from the text body. It exists so
// -check can compare the commentary itself: the header carries a retrieval
// timestamp and the upstream URL, both of which change on every run, and
// comparing them would make -check fail against an unmodified file.
const headerSeparator = "\n---\n"

// splitHeader separates the provenance header from the commentary body.
func splitHeader(file string) (header, body string, ok bool) {
	i := strings.Index(file, headerSeparator)
	if i < 0 {
		return "", file, false
	}
	return file[:i+len(headerSeparator)], file[i+len(headerSeparator):], true
}

// ---------------------------------------------------------------------------
// Cleaning
// ---------------------------------------------------------------------------

var (
	// Paragraph and line breaks become real newlines so the chunker can split on
	// them; every other tag is dropped but its text is kept.
	blockTag = regexp.MustCompile(`(?i)</?(p|div|br|li|tr|h[1-6])\b[^>]*>`)
	anyTag   = regexp.MustCompile(`(?s)<[^>]*>`)
	// quran.com pads some class attributes with trailing spaces ("ar ", "fa ").
	classAttr = regexp.MustCompile(`class\s*=\s*"[^"]*"\s*`)
	spaceRun  = regexp.MustCompile(`[ \t ]+`)
	blankRun  = regexp.MustCompile(`\n{3,}`)
)

// cleanHTML turns the API's HTML into plain text.
//
// Tags are removed but their contents are kept. That is deliberate and follows
// Part 1 §7: the spans in this corpus are not decoration, they carry meaning.
// `<span class="reference brown">` holds bracketed citations such as
// "[ الزمر : 23 ]", `arabic qpc-hafs` holds the Quranic quotations Ibn Kathir
// is explaining, and the blue/red spans hold hadith and inline asides. Dropping
// the text along with the tag would delete the citations; keeping the tags would
// push markup into the embedded text. So only the markup goes.
func cleanHTML(s string) string {
	s = blockTag.ReplaceAllStringFunc(s, func(m string) string {
		if strings.HasPrefix(strings.ToLower(m), "</") {
			return "\n\n"
		}
		return "\n"
	})
	s = anyTag.ReplaceAllString(s, "")
	// Unescape last, so an entity that produced a literal tag-looking string is
	// not re-interpreted as markup.
	s = html.UnescapeString(s)

	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	s = spaceRun.ReplaceAllString(s, " ")
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		lines[i] = strings.TrimSpace(line)
	}
	return strings.TrimSpace(blankRun.ReplaceAllString(strings.Join(lines, "\n"), "\n\n"))
}

// ---------------------------------------------------------------------------
// Output
// ---------------------------------------------------------------------------

func defaultPath(ctx context.Context, tafsirID, surah int) string {
	slug := "tafsir-" + strconv.Itoa(tafsirID)
	if meta, err := fetchEditionMeta(ctx, tafsirID); err == nil && meta.Slug != "" {
		slug = meta.Slug
	}
	return fmt.Sprintf("../corpus/tafsir/%s-surah-%d.md", slug, surah)
}

// render builds the whole file, header included.
func render(ctx context.Context, tafsirID, surah int, entries map[int]string) (string, error) {
	meta, err := fetchEditionMeta(ctx, tafsirID)
	if err != nil {
		// The header should still be honest about what it does not know.
		log.Printf("warning: could not read edition metadata: %v", err)
	}

	url := fmt.Sprintf("%s/tafsirs/%d/by_chapter/%d?per_page=%d", apiBase, tafsirID, surah, quran.AyahCount(surah))

	var b strings.Builder
	fmt.Fprintf(&b, "# Tafsir edition %d — Surah %d\n\n", tafsirID, surah)
	if meta.Name != "" {
		fmt.Fprintf(&b, "**Work:** %s\n", meta.Name)
	}
	if meta.AuthorName != "" {
		fmt.Fprintf(&b, "**Author:** %s\n", meta.AuthorName)
	}
	fmt.Fprintf(&b, "**Edition slug:** %s\n", meta.Slug)
	fmt.Fprintf(&b, "**Language:** ar\n")
	fmt.Fprintf(&b, "**Retrieved from:** %s\n", url)
	fmt.Fprintf(&b, "**Retrieved at:** %s\n", time.Now().UTC().Format(time.RFC3339))
	fmt.Fprintf(&b, "**Ayahs:** %d\n", len(entries))
	fmt.Fprintf(&b, "\nThis file is a snapshot of a third-party API response, not scripture. It is\n")
	fmt.Fprintf(&b, "commentary: the copyright and verification state of the work is recorded in the\n")
	fmt.Fprintf(&b, "`sources` and `source_versions` collections, not here.\n\n")
	fmt.Fprintf(&b, "Each `## surah:ayah` heading below is a key supplied by the API, so the verse a\n")
	fmt.Fprintf(&b, "passage belongs to is a fact rather than something parsed out of the text.\n\n")

	var text strings.Builder
	for ayah := 1; ayah <= len(entries); ayah++ {
		fmt.Fprintf(&text, "\n## %d:%d\n\n%s\n", surah, ayah, entries[ayah])
	}
	return b.String() + headerSeparator + text.String(), nil
}

func sortedKeys(m map[int]string) []int {
	out := make([]int, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Ints(out)
	return out
}
