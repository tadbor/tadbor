package main

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestCleanHTML(t *testing.T) {
	t.Run("drops tags but keeps their text", func(t *testing.T) {
		got := cleanHTML(`<p>روى الثعلبي <span class="blue">ويقال :</span> سليم</p>`)
		if got != "روى الثعلبي ويقال : سليم" {
			t.Fatalf("got %q", got)
		}
	})

	// Part 1 §7: the spans carry citations and quotations, so their text must
	// survive even though the markup does not. Losing these would silently drop
	// Ibn Kathir's references from the corpus.
	t.Run("keeps citation and quotation text from every span class", func(t *testing.T) {
		for _, in := range []string{
			`<span class="reference brown"> الزمر : 23 </span>`,
			`<span class="arabic qpc-hafs"> تلك آيات الكتاب </span>`,
			`<span class="red"> " الدلائل " </span>`,
			`<span class="blue"> عن ابن عباس قال : </span>`,
			`<span class="ar "> نص </span>`,
		} {
			got := cleanHTML(in)
			if got == "" || strings.ContainsAny(got, "<>") {
				t.Errorf("cleanHTML(%q) = %q — text or markup lost", in, got)
			}
			if strings.TrimSpace(got) == "" {
				t.Errorf("cleanHTML(%q) produced nothing", in)
			}
		}
	})

	t.Run("turns block tags into paragraph breaks", func(t *testing.T) {
		got := cleanHTML(`<p>الفقرة الأولى</p><p>الفقرة الثانية</p>`)
		if !strings.Contains(got, "\n\n") {
			t.Fatalf("no paragraph break in %q", got)
		}
		if strings.Contains(got, "<p>") || strings.Contains(got, "</p>") {
			t.Fatalf("paragraph tags survived: %q", got)
		}
	})

	t.Run("handles nested and self-closing tags", func(t *testing.T) {
		got := cleanHTML(`<p>قبل <b>After<b> ثم<br>سطر</p>`)
		if strings.ContainsAny(got, "<>") {
			t.Fatalf("markup survived: %q", got)
		}
		if !strings.Contains(got, "ثم") || !strings.Contains(got, "سطر") {
			t.Fatalf("text lost: %q", got)
		}
	})

	t.Run("unescapes entities after removing tags", func(t *testing.T) {
		// Unescaping has to happen last: an entity that expands to something
		// tag-shaped must not be re-read as markup.
		got := cleanHTML(`<p>حديث &lt;ك&gt; &amp; نص</p>`)
		if strings.Contains(got, "&lt;") || strings.Contains(got, "&amp;") {
			t.Fatalf("entities left encoded: %q", got)
		}
		if !strings.Contains(got, "<ك>") {
			t.Fatalf("entity not expanded: %q", got)
		}
	})

	t.Run("collapses whitespace and trims", func(t *testing.T) {
		got := cleanHTML("  <p>  نص   طويل  \n\n\n\n  بعد </p>  ")
		if strings.HasPrefix(got, " ") || strings.HasSuffix(got, " ") {
			t.Fatalf("not trimmed: %q", got)
		}
		if strings.Contains(got, "\n\n\n") {
			t.Fatalf("blank runs survived: %q", got)
		}
		if strings.Contains(got, "  ") {
			t.Fatalf("double spaces survived: %q", got)
		}
	})

	t.Run("is idempotent", func(t *testing.T) {
		once := cleanHTML(`<p>تفسير <span class="reference brown">[ وهي مكية ]</span></p>`)
		twice := cleanHTML(once)
		if once != twice {
			t.Fatalf("not idempotent:\n once: %q\ntwice: %q", once, twice)
		}
	})

	t.Run("never returns empty for real text", func(t *testing.T) {
		if got := cleanHTML(`<p>نص</p>`); got != "نص" {
			t.Fatalf("got %q", got)
		}
	})
}

// The heading is the verse mapping. If cleaning or rendering ever mangled it,
// issue #5 would attribute commentary to the wrong ayah without any error.
func TestRenderKeepsOneHeadingPerAyahInOrder(t *testing.T) {
	entries := map[int]string{1: "الأولى", 2: "الثانية", 3: "الثالثة"}
	out, err := render(context.Background(), 14, 12, entries)
	if err != nil {
		t.Fatalf("render: %v", err)
	}

	var seen []int
	for _, line := range strings.Split(out, "\n") {
		if !strings.HasPrefix(line, "## ") {
			continue
		}
		key := strings.TrimSpace(strings.TrimPrefix(line, "## "))
		surah, ayah, ok := strings.Cut(key, ":")
		if !ok {
			t.Fatalf("malformed heading %q", line)
		}
		if surah != "12" {
			t.Fatalf("heading %q is not for surah 12", line)
		}
		n := 0
		for _, c := range ayah {
			if c < '0' || c > '9' {
				t.Fatalf("heading %q has a non-numeric ayah", line)
			}
			n = n*10 + int(c-'0')
		}
		seen = append(seen, n)
	}

	want := sortedKeys(entries)
	if len(seen) != len(want) {
		t.Fatalf("got %d headings, want %d", len(seen), len(want))
	}
	for i := range want {
		if seen[i] != want[i] {
			t.Fatalf("heading %d is %d, want %d", i, seen[i], want[i])
		}
	}

	for ayah, text := range entries {
		if !strings.Contains(out, text) {
			t.Errorf("ayah %d text missing from the rendered file", ayah)
		}
	}
}

// fetch refuses to produce a file from an incomplete or misordered response.
// This drives fetch's validation directly through a stub transport.
func TestFetchRejectsIncompleteResponses(t *testing.T) {
	cases := []struct {
		name string
		body string
		want string
	}{
		{
			name: "missing an ayah",
			body: envelope(allVerses(2)),
			want: "partial file",
		},
		{
			// Complete, but ayah 2 and 3 are swapped. The count check passes, so
			// this can only be caught by comparing each key against its position.
			name: "out of order",
			body: envelope(allVersesSwapped(2, 3)),
			want: "not in surah order",
		},
		{
			// A full-length response where one ayah is repeated, which means a
			// different ayah is absent. The length is right, so only a
			// position-aware check can catch it.
			name: "duplicate ayah standing in for a missing one",
			body: envelope(allVersesWithDuplicate(111, 1, 2)),
			want: "not in surah order",
		},
		{
			name: "empty commentary",
			body: envelope(allVersesWithText(111, 50, "<p>   </p>")),
			want: "has no commentary text",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			entries, err := fetchWithStub(t, tc.body)
			if err == nil {
				t.Fatalf("expected an error, got %d entries", len(entries))
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %q does not mention %q", err, tc.want)
			}
		})
	}
}

func TestFetchAcceptsACompleteResponse(t *testing.T) {
	entries, err := fetchWithStub(t, `{"tafsirs":`+allVerses(111)+`}`)
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if len(entries) != 111 {
		t.Fatalf("got %d entries, want 111", len(entries))
	}
	if entries[1] != "نص" {
		t.Fatalf("ayah 1 = %q, want the cleaned text", entries[1])
	}
}

// envelope wraps a tafsirs array the way the API returns it.
func envelope(arr string) string { return `{"tafsirs":` + arr + `}` }

// allVerses builds a well-formed response body for n ayahs of surah 12.
func allVerses(n int) string {
	var b strings.Builder
	b.WriteString("[")
	for i := 1; i <= n; i++ {
		if i > 1 {
			b.WriteString(",")
		}
		b.WriteString(verseEntry(i, "<p>نص</p>"))
	}
	b.WriteString("]")
	return b.String()
}

func verseEntry(ayah int, text string) string {
	return `{"verse_key":"12:` + itoa(ayah) + `","text":"` + text + `"}`
}

// allVersesSwapped builds a complete response with two ayahs transposed.
func allVersesSwapped(a, c int) string {
	var b strings.Builder
	b.WriteString("[")
	for i := 1; i <= 111; i++ {
		if i > 1 {
			b.WriteString(",")
		}
		key := i
		if i == a {
			key = c
		} else if i == c {
			key = a
		}
		b.WriteString(verseEntry(key, "<p>نص</p>"))
	}
	b.WriteString("]")
	return b.String()
}

// allVersesWithDuplicate builds a complete-length response in which the ayah at
// position at is replaced by a second copy of the ayah at dup.
func allVersesWithDuplicate(n, at, dup int) string {
	var b strings.Builder
	b.WriteString("[")
	for i := 1; i <= n; i++ {
		if i > 1 {
			b.WriteString(",")
		}
		key := i
		if i == at {
			key = dup
		}
		b.WriteString(verseEntry(key, "<p>نص</p>"))
	}
	b.WriteString("]")
	return b.String()
}

// allVersesWithText builds a complete response with one ayah's text replaced.
func allVersesWithText(n, ayah int, text string) string {
	var b strings.Builder
	b.WriteString("[")
	for i := 1; i <= n; i++ {
		if i > 1 {
			b.WriteString(",")
		}
		t := "<p>نص</p>"
		if i == ayah {
			t = text
		}
		b.WriteString(verseEntry(i, t))
	}
	b.WriteString("]")
	return b.String()
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var digits []byte
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}

// fetchWithStub points the shared client at a canned body for every request.
func fetchWithStub(t *testing.T, body string) (map[int]string, error) {
	t.Helper()
	old := httpClient
	t.Cleanup(func() { httpClient = old })
	httpClient = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(body)),
			Header:     http.Header{},
		}, nil
	})}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return fetch(ctx, 14, 12)
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// -check must compare the commentary, not the whole file. The header records the
// retrieval time, so a byte-for-byte file comparison reports a difference on
// every run against a file this command wrote itself, which would make the check
// useless.
func TestSplitHeaderIgnoresTheRetrievalTimestamp(t *testing.T) {
	entries := map[int]string{1: "نص"}
	first, err := render(context.Background(), 14, 12, entries)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	time.Sleep(1100 * time.Millisecond)
	second, err := render(context.Background(), 14, 12, entries)
	if err != nil {
		t.Fatalf("render: %v", err)
	}

	if first == second {
		t.Fatal("two renders a second apart came out identical — the timestamp is not being written")
	}

	_, bodyA, ok := splitHeader(first)
	if !ok {
		t.Fatal("rendered file has no header separator")
	}
	_, bodyB, ok := splitHeader(second)
	if !ok {
		t.Fatal("rendered file has no header separator")
	}
	if bodyA != bodyB {
		t.Fatalf("bodies differ:\n%q\n%q", bodyA, bodyB)
	}
}

func TestSplitHeaderRejectsAFileWithoutTheSeparator(t *testing.T) {
	if _, _, ok := splitHeader("# hand-written\n\nno separator here"); ok {
		t.Fatal("accepted a file that this command did not write")
	}
}

// A real change in the commentary must still be detected, or -check would be
// worthless in the other direction.
func TestSplitHeaderStillSeesAnEditedBody(t *testing.T) {
	entries := map[int]string{1: "نص"}
	file, err := render(context.Background(), 14, 12, entries)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	_, stored, _ := splitHeader(file)

	edited := map[int]string{1: "نص مختلف"}
	other, err := render(context.Background(), 14, 12, edited)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	_, changed, _ := splitHeader(other)

	if stored == changed {
		t.Fatal("an edited body compared equal")
	}
}
