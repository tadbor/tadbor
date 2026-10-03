// Tadbor retrieval validation harness.
//
// Sweeps every ayah of a surah through the running retrieval endpoint and writes
// a written record of what came back, per issue #7. Its job is to produce an
// honest verdict per ayah — including "this is a gap" and "this needs a human to
// read it" — so the record can be trusted as evidence rather than as a green
// tick.
//
// The harness deliberately does not decide whether a chunk is *semantically*
// about the verse. Only exact verse mapping is mechanically checkable here;
// anything served from thematic fallback is marked NEEDS_REVIEW, because
// confirming relevance means reading Arabic commentary, which a script cannot do.
//
// Usage:
//
//	go run ./cmd/validate_retrieval -surah 12 -out docs/retrieval-validation.md
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// Verdict is the per-ayah outcome recorded in the report.
type Verdict string

const (
	// VerdictExact: evidence mapped to exactly this ayah was returned, so the
	// mapping is mechanically confirmed correct.
	VerdictExact Verdict = "PASS_EXACT"
	// VerdictThematic: served from thematic/semantic fallback. Reachable, but
	// relevance is unproven and a human must read it.
	VerdictThematic Verdict = "NEEDS_REVIEW_THEMATIC"
	// VerdictInsufficient: retrieval correctly refused. This is a known gap
	// needing more Tafsir chunking, never a pass.
	VerdictInsufficient Verdict = "GAP_INSUFFICIENT_EVIDENCE"
	// VerdictError: the request itself failed (bad gateway, provider outage).
	VerdictError Verdict = "ERROR"
)

// chunk mirrors the fields of retrieval.TafsirChunk that the harness judges on.
// Embeddings and source_verified are json:"-" upstream and so never arrive.
type chunk struct {
	ID          string `json:"id"`
	Text        string `json:"text"`
	SurahID     int    `json:"surah_id"`
	AyahStart   int    `json:"ayah_start"`
	AyahEnd     int    `json:"ayah_end"`
	MappingType string `json:"mapping_type"`
}

// result is one ayah's row in the report.
type result struct {
	Ayah    int
	Verdict Verdict
	Chunks  []chunk
	Detail  string
}

// covers reports whether a chunk's ayah range includes the requested ayah.
// Thematic and surah-level chunks use 0/0 and so never cover a specific ayah —
// that is what keeps them out of exact-verse answers.
func (c chunk) covers(ayah int) bool {
	if c.AyahStart == 0 && c.AyahEnd == 0 {
		return false
	}
	return c.AyahStart <= ayah && ayah <= c.AyahEnd
}

// classify turns a 200 response into a verdict. Any chunk actually mapped to
// the requested ayah is an exact hit; otherwise the ayah was answered from
// thematic fallback and needs a human.
func classify(ayah int, chunks []chunk) (Verdict, string) {
	if len(chunks) == 0 {
		return VerdictInsufficient, "200 OK with an empty evidence array"
	}
	var exact, thematic int
	ranges := make([]string, 0, len(chunks))
	for _, c := range chunks {
		if c.covers(ayah) {
			exact++
		} else {
			thematic++
		}
		ranges = append(ranges, fmt.Sprintf("%s %d:%d", c.MappingType, c.AyahStart, c.AyahEnd))
	}
	if exact > 0 {
		return VerdictExact, fmt.Sprintf("%d chunk(s) mapped to this ayah [%s]", exact, strings.Join(ranges, ", "))
	}
	return VerdictThematic, fmt.Sprintf("no exact mapping; %d thematic chunk(s) [%s]", thematic, strings.Join(ranges, ", "))
}

// fetch calls the retrieval endpoint for one ayah.
func fetch(client *http.Client, baseURL string, surah, ayah int) (result, error) {
	url := fmt.Sprintf("%s/retrieval/%d/%d", strings.TrimRight(baseURL, "/"), surah, ayah)
	resp, err := client.Get(url)
	if err != nil {
		return result{Ayah: ayah, Verdict: VerdictError, Detail: err.Error()}, nil
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return result{Ayah: ayah, Verdict: VerdictError, Detail: "read response: " + err.Error()}, nil
	}

	// 422 INSUFFICIENT_EVIDENCE is the documented, correct refusal — the whole
	// point of the fix in internal/retrieval: it is a gap, not a failure.
	if resp.StatusCode == http.StatusUnprocessableEntity {
		return result{Ayah: ayah, Verdict: VerdictInsufficient, Detail: http.StatusText(resp.StatusCode)}, nil
	}
	if resp.StatusCode != http.StatusOK {
		return result{Ayah: ayah, Verdict: VerdictError, Detail: fmt.Sprintf("HTTP %d: %s", resp.StatusCode, truncate(string(body), 200))}, nil
	}

	var chunks []chunk
	if err := json.NewDecoder(bytes.NewReader(body)).Decode(&chunks); err != nil {
		return result{Ayah: ayah, Verdict: VerdictError, Detail: "decode: " + err.Error()}, nil
	}

	verdict, detail := classify(ayah, chunks)
	return result{Ayah: ayah, Verdict: verdict, Chunks: chunks, Detail: detail}, nil
}

func truncate(s string, n int) string {
	s = strings.TrimSpace(strings.ReplaceAll(s, "\n", " "))
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// report renders the written record for issue #7.
func report(surah int, from, to int, results []result, generated time.Time) string {
	var b strings.Builder

	counts := map[Verdict]int{}
	for _, r := range results {
		counts[r.Verdict]++
	}

	fmt.Fprintf(&b, "# Retrieval validation — Surah %d, ayahs %d–%d\n\n", surah, from, to)
	fmt.Fprintf(&b, "Generated %s by `backend/cmd/validate_retrieval` (issue #7).\n\n", generated.Format("2006-01-02 15:04 MST"))
	b.WriteString("Each ayah was requested from the live retrieval endpoint and the returned\n")
	b.WriteString("chunk mappings were checked mechanically: a chunk counts as an exact hit only\n")
	b.WriteString("when its ayah range actually contains the requested ayah.\n\n")

	fmt.Fprintf(&b, "## Summary\n\n| Verdict | Ayahs |\n|---|---:|\n")
	for _, v := range []Verdict{VerdictExact, VerdictThematic, VerdictInsufficient, VerdictError} {
		fmt.Fprintf(&b, "| %s | %d |\n", v, counts[v])
	}
	fmt.Fprintf(&b, "| **Total** | **%d** |\n\n", len(results))

	// Never let an empty corpus read as a pass. If literally nothing resolved,
	// the honest conclusion is that validation could not be performed at all.
	if counts[VerdictExact]+counts[VerdictThematic] == 0 {
		b.WriteString("> **VALIDATION BLOCKED — this is not a pass.**\n>\n")
		b.WriteString("> No ayah returned any evidence. That means the corpus retrieval is reading\n")
		b.WriteString("> from is empty or unapproved, so there is nothing to have validated. The\n")
		b.WriteString("> unblocking work is upstream: a Quran corpus (issue #2) and a verified\n")
		b.WriteString("> Tafsir source with ingested chunks (issue #3).\n\n")
	}
	if counts[VerdictExact] == 0 && counts[VerdictThematic] > 0 {
		b.WriteString("> **No ayah had exact verse mapping.** Every answer came from thematic\n")
		b.WriteString("> fallback, so verse-level attribution is unproven across the surah.\n>\n")
	}

	b.WriteString("## Per-ayah results\n\n| Ayah | Verdict | Chunks | Detail |\n|---:|---|---:|---|\n")
	for _, r := range results {
		fmt.Fprintf(&b, "| %d | %s | %d | %s |\n", r.Ayah, r.Verdict, len(r.Chunks), r.Detail)
	}

	b.WriteString("\n## Gaps to resolve\n\n")
	var gaps []string
	for _, r := range results {
		if r.Verdict == VerdictInsufficient {
			gaps = append(gaps, fmt.Sprintf("- %d:%d — no approved evidence; needs more Tafsir chunking or a further source", surah, r.Ayah))
		}
	}
	if len(gaps) == 0 {
		b.WriteString("None: every ayah returned evidence.\n")
	} else {
		for _, g := range gaps {
			b.WriteString(g)
			b.WriteString("\n")
		}
	}

	needsReview := counts[VerdictThematic]
	if needsReview > 0 {
		fmt.Fprintf(&b, "\n## Awaiting human review (%d)\n\n", needsReview)
		b.WriteString("These ayahs were answered from thematic fallback. A person must read the\n")
		b.WriteString("returned commentary and confirm it is genuinely about that verse; the\n")
		b.WriteString("harness cannot establish relevance, only reachability.\n\n")
		for _, r := range results {
			if r.Verdict == VerdictThematic {
				fmt.Fprintf(&b, "### %d:%d\n\n", surah, r.Ayah)
				for _, c := range r.Chunks {
					fmt.Fprintf(&b, "- `%s` (%s, %d:%d) %s\n", c.ID, c.MappingType, c.AyahStart, c.AyahEnd, truncate(c.Text, 200))
				}
				b.WriteString("\n")
			}
		}
	}

	return b.String()
}

// preflight confirms the target really is the tadbor API before sweeping.
//
// This matters more than it looks: a dev server for the SPA answers *every*
// unknown path with 200 and an HTML shell, so without a preflight the sweep
// reports 111 identical JSON decode errors and looks like a retrieval outage
// rather than a wrong port.
func preflight(client *http.Client, baseURL string) error {
	resp, err := client.Get(strings.TrimRight(baseURL, "/") + "/health")
	if err != nil {
		return fmt.Errorf("cannot reach %s: %w", baseURL, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s/health returned HTTP %d, expected 200 — is the API running?", baseURL, resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "application/json") {
		return fmt.Errorf("%s is not the tadbor API: GET /health returned Content-Type %q, expected JSON. "+
			"This looks like a different service (the SPA dev server answers every path with an HTML shell)", baseURL, ct)
	}
	var health struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(body, &health); err != nil {
		return fmt.Errorf("%s/health did not return the expected JSON: %w", baseURL, err)
	}
	if health.Status != "ok" {
		return fmt.Errorf("%s/health reported status %q, expected \"ok\"", baseURL, health.Status)
	}
	return nil
}

func main() {
	surah := flag.Int("surah", 12, "surah id to validate")
	from := flag.Int("from", 1, "first ayah")
	to := flag.Int("to", 111, "last ayah")
	baseURL := flag.String("base-url", envOr("API_BASE_URL", "http://localhost:8080"), "base URL of the running API")
	out := flag.String("out", "", "write the markdown record here (default: stdout only)")
	timeout := flag.Duration("timeout", 30*time.Second, "per-request timeout")
	flag.Parse()

	client := &http.Client{Timeout: *timeout}
	if err := preflight(client, *baseURL); err != nil {
		fmt.Fprintf(os.Stderr, "preflight failed: %v\n", err)
		os.Exit(1)
	}

	results := make([]result, 0, *to-*from+1)
	for ayah := *from; ayah <= *to; ayah++ {
		r, _ := fetch(client, *baseURL, *surah, ayah)
		results = append(results, r)
		fmt.Fprintf(os.Stderr, "%d:%d %s\n", *surah, r.Ayah, r.Verdict)
	}

	md := report(*surah, *from, *to, results, time.Now())
	if *out == "" {
		fmt.Print(md)
		return
	}
	if err := os.WriteFile(*out, []byte(md), 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "write %s: %v\n", *out, err)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "wrote %s\n", *out)
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
