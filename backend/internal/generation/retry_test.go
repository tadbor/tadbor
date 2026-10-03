package generation

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeProvider returns a scripted sequence of responses, repeating the last
// entry once the script runs out, and records what was sent on each call.
type fakeProvider struct {
	srv *httptest.Server

	mu       sync.Mutex
	calls    int
	statuses []int
	bodies   []string
	sent     [][]byte
}

func newFakeProvider(t *testing.T, statuses []int, bodies []string) *fakeProvider {
	t.Helper()
	f := &fakeProvider{statuses: statuses, bodies: bodies}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sent, _ := io.ReadAll(r.Body)

		f.mu.Lock()
		i := f.calls
		f.calls++
		f.sent = append(f.sent, sent)
		status, body := f.statuses[len(f.statuses)-1], f.bodies[len(f.bodies)-1]
		if i < len(f.statuses) {
			status = f.statuses[i]
		}
		if i < len(f.bodies) {
			body = f.bodies[i]
		}
		f.mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeProvider) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

func (f *fakeProvider) sentBodies() [][]byte {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([][]byte, len(f.sent))
	copy(out, f.sent)
	return out
}

func okBody(t *testing.T, modelOutput string) string {
	t.Helper()
	return `{"candidates":[{"content":{"parts":[{"text":` + mustJSON(t, modelOutput) + `}]},"finishReason":"STOP"}]}`
}

const draftJSON = `{"content":"c","source_refs":["s1"],"claims":["cl"],"warnings":[],"confidence":"medium"}`

func TestGenerateSucceedsWithoutRetrying(t *testing.T) {
	f := newFakeProvider(t, []int{http.StatusOK}, []string{okBody(t, draftJSON)})

	if _, err := newTestService(f.srv.URL, "k").Generate(testRequest()); err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if got := f.callCount(); got != 1 {
		t.Errorf("provider called %d times, want 1 — a success must not retry", got)
	}
}

func TestGenerateRetriesTransientStatuses(t *testing.T) {
	for _, status := range []int{http.StatusTooManyRequests, http.StatusServiceUnavailable} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			// Fail twice, then succeed — the retry must recover the call.
			f := newFakeProvider(t,
				[]int{status, status, http.StatusOK},
				[]string{`{"error":{"message":"try later"}}`, `{"error":{"message":"try later"}}`, okBody(t, draftJSON)},
			)

			out, err := newTestService(f.srv.URL, "k").Generate(testRequest())
			if err != nil {
				t.Fatalf("Generate should have recovered after retries: %v", err)
			}
			if out.Content != "c" {
				t.Errorf("Content = %q, want %q", out.Content, "c")
			}
			if !out.RequiresReview {
				t.Error("RequiresReview = false after a retried call — invariant not preserved")
			}
			if got := f.callCount(); got != 3 {
				t.Errorf("provider called %d times, want 3", got)
			}
		})
	}
}

func TestGenerateStopsAtMaxRetries(t *testing.T) {
	f := newFakeProvider(t, []int{http.StatusServiceUnavailable}, []string{`{"error":{"message":"high demand"}}`})

	out, err := newTestService(f.srv.URL, "k").Generate(testRequest())
	if err == nil {
		t.Fatalf("expected an error once retries were exhausted, got %+v", out)
	}
	if want := defaultMaxRetries + 1; f.callCount() != want {
		t.Errorf("provider called %d times, want %d (1 initial + %d retries)", f.callCount(), want, defaultMaxRetries)
	}
	if !strings.Contains(err.Error(), "503") {
		t.Errorf("error %q does not name the status", err)
	}
	if !strings.Contains(err.Error(), "giving up after 3 retries") {
		t.Errorf("error %q does not report that retries were exhausted", err)
	}
}

func TestGenerateDoesNotRetryNonTransientStatuses(t *testing.T) {
	// Each of these fails identically on every attempt, so retrying would only
	// delay the real error.
	for _, status := range []int{
		http.StatusBadRequest,
		http.StatusUnauthorized,
		http.StatusForbidden,
		http.StatusInternalServerError,
		http.StatusNotFound,
	} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			f := newFakeProvider(t, []int{status}, []string{`{"error":{"message":"nope"}}`})

			if _, err := newTestService(f.srv.URL, "k").Generate(testRequest()); err == nil {
				t.Fatal("expected an error")
			}
			if got := f.callCount(); got != 1 {
				t.Errorf("provider called %d times, want 1 — %d must not be retried", got, status)
			}
		})
	}
}

// A retry must resend the same payload. This guards the request being rebuilt
// from a fresh reader each attempt rather than a consumed one.
func TestRetryResendsIdenticalBody(t *testing.T) {
	f := newFakeProvider(t,
		[]int{http.StatusServiceUnavailable, http.StatusOK},
		[]string{`{"error":{"message":"high demand"}}`, okBody(t, draftJSON)},
	)

	if _, err := newTestService(f.srv.URL, "k").Generate(testRequest()); err != nil {
		t.Fatalf("Generate: %v", err)
	}

	sent := f.sentBodies()
	if len(sent) != 2 {
		t.Fatalf("provider called %d times, want 2", len(sent))
	}
	if len(sent[0]) == 0 {
		t.Fatal("first request body was empty")
	}
	if string(sent[0]) != string(sent[1]) {
		t.Errorf("retry sent a different body:\n first: %s\nsecond: %s", sent[0], sent[1])
	}
}

func TestIsRetryableStatus(t *testing.T) {
	retryable := map[int]bool{
		http.StatusTooManyRequests:    true,
		http.StatusServiceUnavailable: true,
	}
	for _, status := range []int{
		http.StatusOK, http.StatusBadRequest, http.StatusUnauthorized,
		http.StatusForbidden, http.StatusNotFound, http.StatusInternalServerError,
		http.StatusBadGateway, http.StatusGatewayTimeout,
	} {
		if got := isRetryableStatus(status); got != retryable[status] {
			t.Errorf("isRetryableStatus(%d) = %v, want %v", status, got, retryable[status])
		}
	}
}

func TestRetryDelayIsExponential(t *testing.T) {
	base := 500 * time.Millisecond
	want := []time.Duration{base, 2 * base, 4 * base, 8 * base}
	for i, w := range want {
		if got := retryDelay(i+1, base); got != w {
			t.Errorf("retryDelay(%d) = %v, want %v", i+1, got, w)
		}
	}
	if got := retryDelay(0, base); got != base {
		t.Errorf("retryDelay(0) = %v, want %v — must not shift or go negative", got, base)
	}
}

// The production default must stay small: this is a ~333-call offline batch.
func TestDefaultRetryBudgetIsBounded(t *testing.T) {
	if defaultMaxRetries != 3 {
		t.Errorf("defaultMaxRetries = %d, want 3", defaultMaxRetries)
	}
	svc := NewService()
	if svc.maxRetries != defaultMaxRetries {
		t.Errorf("NewService maxRetries = %d, want %d", svc.maxRetries, defaultMaxRetries)
	}
	total := time.Duration(0)
	for i := 1; i <= defaultMaxRetries; i++ {
		total += retryDelay(i, svc.baseBackoff)
	}
	if total > 10*time.Second {
		t.Errorf("worst-case added latency %v is too high for a batch of hundreds of calls", total)
	}
}

// A Service built as a struct literal (no sleep func) must not panic.
func TestSleepForToleratesNilFunc(t *testing.T) {
	s := &Service{}
	s.sleepFor(time.Nanosecond)
}
