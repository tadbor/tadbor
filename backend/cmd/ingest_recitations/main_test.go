package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// The zero padding is the whole trick: everyayah.com's directory listing uses
// three digits for the surah and three for the ayah with no separator, so
// surah 12 ayah 9 is 012009. Getting this wrong yields a 404 that is
// indistinguishable from a recording that does not exist — which is how the two
// wrong folder guesses in sources would have gone unnoticed.
func TestAyahURL(t *testing.T) {
	cases := []struct {
		folder string
		surah  int
		ayah   int
		want   string
	}{
		{"Alafasy_128kbps", 12, 1, "https://everyayah.com/data/Alafasy_128kbps/012001.mp3"},
		{"Alafasy_128kbps", 12, 9, "https://everyayah.com/data/Alafasy_128kbps/012009.mp3"},
		{"Alafasy_128kbps", 12, 10, "https://everyayah.com/data/Alafasy_128kbps/012010.mp3"},
		{"Alafasy_128kbps", 12, 111, "https://everyayah.com/data/Alafasy_128kbps/012111.mp3"},
		{"MaherAlMuaiqly128kbps", 12, 111, "https://everyayah.com/data/MaherAlMuaiqly128kbps/012111.mp3"},
		{"Ghamadi_40kbps", 112, 1, "https://everyayah.com/data/Ghamadi_40kbps/112001.mp3"},
	}
	for _, tc := range cases {
		if got := ayahURL(tc.folder, tc.surah, tc.ayah); got != tc.want {
			t.Errorf("ayahURL(%q, %d, %d) = %q, want %q", tc.folder, tc.surah, tc.ayah, got, tc.want)
		}
	}
}

// Every ayah in the surah must produce a distinct URL. A padding bug that
// collided 9 and 09, or 12 and 112, would silently overwrite one verse's audio
// with another's while the row count still looked correct.
func TestAyahURLsAreDistinctPerAyah(t *testing.T) {
	seen := map[string]int{}
	for ayah := 1; ayah <= 111; ayah++ {
		u := ayahURL("Alafasy_128kbps", 12, ayah)
		if prev, dup := seen[u]; dup {
			t.Fatalf("ayah %d and ayah %d both produce %s", prev, ayah, u)
		}
		seen[u] = ayah
	}
	if len(seen) != 111 {
		t.Errorf("produced %d distinct URLs for 111 ayahs", len(seen))
	}
}

// The four reciter ids are fixed by the seed script and by rows already in the
// database; the folder names are fixed by everyayah.com. A folder name that
// stops existing turns into 444 unreachable URLs, so the mapping is pinned.
func TestSourceFoldersAreEveryayahNames(t *testing.T) {
	want := map[string]string{
		"alafasy":    "Alafasy_128kbps",
		"abdulbasit": "Abdul_Basit_Murattal_192kbps",
		"ghamdi":     "Ghamadi_40kbps",
		"muaiqly":    "MaherAlMuaiqly128kbps",
	}
	if len(sources) != len(want) {
		t.Fatalf("sources has %d entries, want %d", len(sources), len(want))
	}
	for _, s := range sources {
		if got, ok := want[s.ReciterID]; !ok {
			t.Errorf("unexpected reciter id %q", s.ReciterID)
		} else if s.Folder != got {
			t.Errorf("reciter %q maps to folder %q, want %q", s.ReciterID, s.Folder, got)
		}
	}
}

// A placeholder URL is the failure this whole command exists to end, and the
// audit is the only thing that notices after the fact.
func TestIsPlaceholder(t *testing.T) {
	yes := []string{
		"https://example.com/placeholder/alafasy/012001.mp3",
		"http://localhost:8000/audio/1.mp3",
		"https://cdn.example.com/x.mp3",
		"https://EXAMPLE.COM/placeholder.mp3",
		"/placeholder/012001.mp3",
	}
	for _, u := range yes {
		if !isPlaceholder(u) {
			t.Errorf("isPlaceholder(%q) = false, want true", u)
		}
	}
	no := []string{
		"https://everyayah.com/data/Alafasy_128kbps/012001.mp3",
		"https://cdn.tadbor.app/recitations/012001.mp3",
	}
	for _, u := range no {
		if isPlaceholder(u) {
			t.Errorf("isPlaceholder(%q) = true, want false", u)
		}
	}
}

func TestIsAudio(t *testing.T) {
	for _, ct := range []string{"audio/mpeg", "audio/mp3", "AUDIO/MPEG", "application/mpeg"} {
		if !isAudio(ct) {
			t.Errorf("isAudio(%q) = false, want true", ct)
		}
	}
	for _, ct := range []string{"text/html", "application/json", ""} {
		if isAudio(ct) {
			t.Errorf("isAudio(%q) = true, want false — an HTML error page is not a recording", ct)
		}
	}
}

// A host that answers 200 with an HTML page is not a working recording, and
// storing it would leave a reciter that looks loaded and plays nothing.
func TestReachableRejectsHTMLWithA200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte("<html>404</html>"))
	}))
	defer srv.Close()

	ok, err := reachable(context.Background(), &http.Client{Timeout: 5 * time.Second}, srv.URL+"/012001.mp3")
	if err != nil {
		t.Fatalf("reachable: %v", err)
	}
	if ok {
		t.Error("reachable accepted an HTML page served with 200")
	}
}

func TestReachableAcceptsAudio(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "audio/mpeg")
		w.Write([]byte("ID3"))
	}))
	defer srv.Close()

	ok, err := reachable(context.Background(), &http.Client{Timeout: 5 * time.Second}, srv.URL+"/012001.mp3")
	if err != nil {
		t.Fatalf("reachable: %v", err)
	}
	if !ok {
		t.Error("reachable rejected an audio response")
	}
}

// Some static hosts do not implement HEAD. Treating a 405 as "missing recording"
// would drop a reciter that is entirely fine, which is why the GET fallback
// exists.
func TestReachableFallsBackWhenHeadIsNotAllowed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "audio/mpeg")
		w.Write([]byte("ID3"))
	}))
	defer srv.Close()

	ok, err := reachable(context.Background(), &http.Client{Timeout: 5 * time.Second}, srv.URL+"/012001.mp3")
	if err != nil {
		t.Fatalf("reachable: %v", err)
	}
	if !ok {
		t.Error("a host that 405s HEAD but serves audio was recorded as a missing recording")
	}
}

func TestReachableReports404(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	ok, err := reachable(context.Background(), &http.Client{Timeout: 5 * time.Second}, srv.URL+"/012999.mp3")
	if err != nil {
		t.Fatalf("reachable: %v", err)
	}
	if ok {
		t.Error("reachable accepted a 404")
	}
}

// Preview mode must contact nothing and write nothing, which is the property
// that makes the default safe to run against a real database.
func TestVerifyAllPreviewMakesNoRequests(t *testing.T) {
	docs, v, err := verifyAll(context.Background(), baseURL, 12, 111, 0, 0, false)
	if err != nil {
		t.Fatalf("verifyAll: %v", err)
	}
	if len(docs) != 0 {
		t.Errorf("preview returned %d documents, want none", len(docs))
	}
	if v.checked != 444 {
		t.Errorf("preview checked %d URLs, want 444", v.checked)
	}
	if v.reachable != 0 || v.unreachable != 0 {
		t.Errorf("preview recorded reachability without making requests: %+v", v)
	}
}

// -limit exists so a run can be bounded and proven resumable, so it must stop
// at exactly the limit rather than at the end of a reciter.
func TestVerifyAllHonoursLimit(t *testing.T) {
	_, v, err := verifyAll(context.Background(), baseURL, 12, 111, 0, 7, false)
	if err != nil {
		t.Fatalf("verifyAll: %v", err)
	}
	if v.checked != 7 {
		t.Errorf("checked %d URLs with -limit 7, want 7", v.checked)
	}
}

// An unreachable URL must be named in the failure, not merely counted: "444
// planned, 441 reachable" sends you hunting, "muaiqly:37" does not.
func TestVerifyAllNamesUnreachableVerses(t *testing.T) {
	// ayah 37 of Alafasy is missing; everything else serves audio.
	const gone = "/data/Alafasy_128kbps/012037.mp3"
	var muaiqly37 bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == gone || muaiqly37 && r.URL.Path == "/data/MaherAlMuaiqly128kbps/012037.mp3" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "audio/mpeg")
	}))
	defer srv.Close()

	docs, v, err := verifyAll(context.Background(), srv.URL+"/data", 12, 37, 0, 0, true)
	if err != nil {
		t.Fatalf("verifyAll: %v", err)
	}
	if v.checked != 37*len(sources) {
		t.Errorf("checked %d URLs, want %d", v.checked, 37*len(sources))
	}
	if v.unreachable != 1 {
		t.Errorf("unreachable = %d, want 1", v.unreachable)
	}
	if len(v.names) != 1 || v.names[0] != "alafasy:37" {
		t.Errorf("names = %v, want [alafasy:37]", v.names)
	}
	if v.reachable != 37*len(sources)-1 {
		t.Errorf("reachable = %d, want %d", v.reachable, 37*len(sources)-1)
	}
	// The missing ayah is skipped rather than written, so a 404 never becomes a
	// stored recitation.
	for _, d := range docs {
		if d.ReciterID == "alafasy" && d.AyahNumber == 37 {
			t.Error("a document was returned for a URL that returned 404")
		}
	}
	if len(docs) != 37*len(sources)-1 {
		t.Errorf("returned %d documents, want %d", len(docs), 37*len(sources)-1)
	}
}

// A URL that answers 200 with an HTML error page is not a recording, and must
// not be stored as one.
func TestVerifyAllSkipsNonAudioResponses(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte("<html>not found</html>"))
	}))
	defer srv.Close()

	_, v, err := verifyAll(context.Background(), srv.URL+"/data", 12, 2, 0, 0, true)
	if err != nil {
		t.Fatalf("verifyAll: %v", err)
	}
	if v.reachable != 0 {
		t.Errorf("reachable = %d against a host serving HTML everywhere, want 0", v.reachable)
	}
	if v.unreachable != 8 {
		t.Errorf("unreachable = %d, want 8", v.unreachable)
	}
}
