package embedding

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// testVector returns a deterministic unit-length vector. Distinct seeds give
// distinct vectors so order-preservation is actually observable.
func testVector(seed float64) []float64 {
	const dims = ExpectedDimensions
	v := make([]float64, dims)
	for i := range v {
		v[i] = seed + float64(i)/float64(dims)/1000
	}
	return normalizeInPlace(v)
}

func normalizeInPlace(v []float64) []float64 {
	var sum float64
	for _, f := range v {
		sum += f * f
	}
	norm := math.Sqrt(sum)
	for i := range v {
		v[i] /= norm
	}
	return v
}

// recordingServer captures every request it serves so tests can assert on the
// exact wire format, batching, and call counts.
type recordingServer struct {
	*httptest.Server
	calls    atomic.Int32
	mu       sync.Mutex
	requests []embedRequest
	bodies   []string
	auth     []string
}

func newRecordingServer(t *testing.T, handler func(w http.ResponseWriter, reqBody []byte)) *recordingServer {
	t.Helper()
	rec := &recordingServer{}
	rec.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw := readAll(r)

		var req embedRequest
		_ = json.Unmarshal(raw, &req)

		rec.mu.Lock()
		if err := json.Unmarshal(raw, &req); err == nil {
			rec.requests = append(rec.requests, req)
		}
		rec.bodies = append(rec.bodies, string(raw))
		rec.auth = append(rec.auth, r.Header.Get("Authorization"))
		rec.mu.Unlock()

		rec.calls.Add(1)
		handler(w, raw)
	}))
	t.Cleanup(rec.Close)
	return rec
}

func (s *recordingServer) snapshot() (requests []embedRequest, bodies []string, auth []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]embedRequest(nil), s.requests...), append([]string(nil), s.bodies...), append([]string(nil), s.auth...)
}

func readAll(r *http.Request) []byte {
	defer r.Body.Close()
	buf := make([]byte, 0, 4096)
	tmp := make([]byte, 4096)
	for {
		n, err := r.Body.Read(tmp)
		buf = append(buf, tmp[:n]...)
		if err != nil {
			return buf
		}
	}
}

// respondVectors echoes one unit vector per input, in order. Each vector is
// seeded from the input's length and position, so a reordering or misalignment
// bug changes the returned values.
func respondVectors(w http.ResponseWriter, raw []byte) {
	var req embedRequest
	_ = json.Unmarshal(raw, &req)
	writeVectorsFor(w, req.Inputs)
}

func writeVectorsFor(w http.ResponseWriter, inputs []string) {
	out := make([][]float64, len(inputs))
	for i, in := range inputs {
		out[i] = testVector(seedOf(in))
	}
	_ = json.NewEncoder(w).Encode(out)
}

// seedOf derives a vector seed from the input's content alone. No position is
// involved, so a reordered or misaligned response cannot accidentally produce the
// values the test expects.
func seedOf(in string) float64 { return 1 + float64(len(in))*0.25 }

func newClient(t *testing.T, srv *recordingServer, maxBatch int) *Client {
	t.Helper()
	c, err := NewClientWithOptions(srv.URL, "test-key", Options{
		MaxBatchSize: maxBatch,
		HTTPClient:   srv.Client(),
		Backoff:      time.Millisecond,
	})
	if err != nil {
		t.Fatalf("NewClientWithOptions: %v", err)
	}
	return c
}

func inputsOf(raw []byte) []string {
	var req embedRequest
	_ = json.Unmarshal(raw, &req)
	return req.Inputs
}

func inputs(n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = fmt.Sprintf("chunk-%02d-%s", i, strings.Repeat("ط", i+1))
	}
	return out
}

func TestNewClientRequiresConfig(t *testing.T) {
	if _, err := NewClient("", "k"); !errors.Is(err, ErrMissingConfig) {
		t.Fatalf("empty endpoint: got %v, want ErrMissingConfig", err)
	}
	if _, err := NewClient("https://example.invalid", "  "); !errors.Is(err, ErrMissingConfig) {
		t.Fatalf("blank key: got %v, want ErrMissingConfig", err)
	}
}

func TestEmbedRequestWireFormat(t *testing.T) {
	srv := newRecordingServer(t, respondVectors)
	c := newClient(t, srv, DefaultMaxBatchSize)

	if _, err := c.Embed(context.Background(), []string{"نص عربي"}); err != nil {
		t.Fatalf("Embed: %v", err)
	}

	reqs, bodies, auth := srv.snapshot()
	if len(reqs) != 1 || len(reqs[0].Inputs) != 1 {
		t.Fatalf("expected one request carrying one input, got %+v", reqs)
	}
	if !reqs[0].Normalize {
		t.Fatalf("expected normalize to be requested, body was %s", bodies[0])
	}
	// A bare string instead of an array changes the provider's response shape,
	// so the array form is part of the contract.
	if !strings.Contains(bodies[0], `"inputs":[`) {
		t.Fatalf("expected inputs as an array, got %s", bodies[0])
	}
	if got := auth[0]; got != "Bearer test-key" {
		t.Fatalf("expected bearer auth header, got %q", got)
	}
}

func TestEmbedSplitsIntoSubBatchesAndPreservesOrder(t *testing.T) {
	srv := newRecordingServer(t, respondVectors)
	c := newClient(t, srv, 32)

	in := inputs(70)
	vecs, err := c.Embed(context.Background(), in)
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}

	if len(vecs) != len(in) {
		t.Fatalf("got %d vectors for %d inputs", len(vecs), len(in))
	}
	if calls := srv.calls.Load(); calls != 3 {
		t.Fatalf("expected 3 sub-batch calls (70 inputs at 32/batch), got %d", calls)
	}

	reqs, _, _ := srv.snapshot()
	for i, want := range []int{32, 32, 6} {
		if got := len(reqs[i].Inputs); got != want {
			t.Fatalf("sub-batch %d: expected %d inputs, got %d", i, want, got)
		}
	}
	for i := range in {
		want := testVector(seedOf(in[i]))
		if !reflect.DeepEqual(vecs[i], want) {
			t.Fatalf("vector %d does not correspond to input %d (%q)", i, i, in[i])
		}
	}
}

func TestEmbedRetriesTransientFailures(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
	}{
		{"rate limited", http.StatusTooManyRequests},
		{"service unavailable", http.StatusServiceUnavailable},
		{"bad gateway", http.StatusBadGateway},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var srv *recordingServer
			srv = newRecordingServer(t, func(w http.ResponseWriter, raw []byte) {
				if srv.calls.Load() < 3 {
					w.WriteHeader(tc.status)
					return
				}
				writeVectorsFor(w, inputsOf(raw))
			})
			c := newClient(t, srv, DefaultMaxBatchSize)

			if _, err := c.Embed(context.Background(), []string{"نص"}); err != nil {
				t.Fatalf("expected retry to succeed, got %v", err)
			}
			if got := srv.calls.Load(); got != 3 {
				t.Fatalf("expected 3 attempts, got %d", got)
			}
		})
	}
}

func TestEmbedStopsAfterMaxAttempts(t *testing.T) {
	srv := newRecordingServer(t, func(w http.ResponseWriter, _ []byte) {
		w.WriteHeader(http.StatusServiceUnavailable)
	})
	c := newClient(t, srv, DefaultMaxBatchSize)

	_, err := c.Embed(context.Background(), []string{"نص"})

	var be *BatchError
	if !errors.As(err, &be) {
		t.Fatalf("expected *BatchError, got %v", err)
	}
	if be.Attempts != maxAttempts {
		t.Fatalf("expected %d attempts, got %d", maxAttempts, be.Attempts)
	}
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("expected ErrUnavailable, got %v", err)
	}
	if got := srv.calls.Load(); got != maxAttempts {
		t.Fatalf("expected %d http calls, got %d", maxAttempts, got)
	}
}

func TestEmbedDoesNotRetryClientErrors(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		want   error
	}{
		{"bad request", http.StatusBadRequest, ErrBadRequest},
		{"unauthorized", http.StatusUnauthorized, ErrUnauthorized},
		{"forbidden", http.StatusForbidden, ErrUnauthorized},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := newRecordingServer(t, func(w http.ResponseWriter, _ []byte) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(`{"error":"model is gated"}`))
			})
			c := newClient(t, srv, DefaultMaxBatchSize)

			_, err := c.Embed(context.Background(), []string{"نص"})
			if !errors.Is(err, tc.want) {
				t.Fatalf("expected %v, got %v", tc.want, err)
			}
			if got := srv.calls.Load(); got != 1 {
				t.Fatalf("client errors must not be retried, got %d calls", got)
			}
		})
	}
}

func TestEmbedRejectsMalformedResponses(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
	}{
		{"wrong vector count", vectorsJSON(1)},
		{"wrong width", defectJSON(2, ExpectedDimensions+1, 1)},
		{"not normalized", defectJSON(2, ExpectedDimensions, 2)},
		{"token level 3d", `[[[0.1,0.2,0.3]]]`},
		{"not json", `<html>gateway error</html>`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := newRecordingServer(t, func(w http.ResponseWriter, _ []byte) {
				_, _ = w.Write([]byte(tc.body))
			})
			c := newClient(t, srv, DefaultMaxBatchSize)

			// Two inputs, so a single-vector response is a real count mismatch.
			_, err := c.Embed(context.Background(), []string{"أ", "ب"})
			if !errors.Is(err, ErrBadResponse) {
				t.Fatalf("expected ErrBadResponse, got %v", err)
			}
			if got := srv.calls.Load(); got != 1 {
				t.Fatalf("malformed responses must not be retried, got %d calls", got)
			}
		})
	}
}

func TestEmbedBatchErrorIndexesCallerInput(t *testing.T) {
	srv := newRecordingServer(t, func(w http.ResponseWriter, raw []byte) {
		if len(inputsOf(raw)) == 6 {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		writeVectorsFor(w, inputsOf(raw))
	})
	c := newClient(t, srv, 32)

	in := inputs(70)
	_, err := c.Embed(context.Background(), in)

	var be *BatchError
	if !errors.As(err, &be) {
		t.Fatalf("expected *BatchError, got %v", err)
	}
	// The failing sub-batch is inputs[64:70]. Indices must be reported against
	// the caller's full input slice so a run can resume from there.
	if be.Start != 64 || be.End != 70 {
		t.Fatalf("expected failing range [64:70], got [%d:%d]", be.Start, be.End)
	}
}

func TestEmbedNeverLeaksAPIKey(t *testing.T) {
	const secret = "hf_super_secret_key_value"
	srv := newRecordingServer(t, func(w http.ResponseWriter, _ []byte) {
		w.WriteHeader(http.StatusUnauthorized)
	})
	c, err := NewClientWithOptions(srv.URL, secret, Options{HTTPClient: srv.Client(), Backoff: time.Millisecond})
	if err != nil {
		t.Fatalf("NewClientWithOptions: %v", err)
	}

	_, err = c.Embed(context.Background(), []string{"نص"})
	if err == nil {
		t.Fatal("expected an error")
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("api key leaked into error: %v", err)
	}
}

func TestEmbedEmptyInput(t *testing.T) {
	srv := newRecordingServer(t, respondVectors)
	c := newClient(t, srv, DefaultMaxBatchSize)

	vecs, err := c.Embed(context.Background(), nil)
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if len(vecs) != 0 {
		t.Fatalf("expected no vectors, got %d", len(vecs))
	}
	if got := srv.calls.Load(); got != 0 {
		t.Fatalf("empty input must not hit the provider, got %d calls", got)
	}
}

func TestEmbedHonoursContextCancellation(t *testing.T) {
	srv := newRecordingServer(t, respondVectors)
	c := newClient(t, srv, DefaultMaxBatchSize)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := c.Embed(ctx, []string{"نص"}); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}

func TestValidateVector(t *testing.T) {
	t.Run("accepts unit vector", func(t *testing.T) {
		if err := ValidateVector(testVector(1), ExpectedDimensions); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
	t.Run("rejects wrong width", func(t *testing.T) {
		if err := ValidateVector(make([]float64, 10), ExpectedDimensions); !errors.Is(err, ErrUnexpectedShape) {
			t.Fatalf("expected ErrUnexpectedShape, got %v", err)
		}
	})
	t.Run("rejects unnormalized", func(t *testing.T) {
		v := make([]float64, ExpectedDimensions)
		v[0] = 5
		if err := ValidateVector(v, ExpectedDimensions); !errors.Is(err, ErrBadResponse) {
			t.Fatalf("expected ErrBadResponse, got %v", err)
		}
	})
	t.Run("rejects zero vector", func(t *testing.T) {
		if err := ValidateVector(make([]float64, ExpectedDimensions), ExpectedDimensions); !errors.Is(err, ErrBadResponse) {
			t.Fatalf("expected ErrBadResponse, got %v", err)
		}
	})
	t.Run("rejects NaN", func(t *testing.T) {
		v := testVector(1)
		v[3] = math.NaN()
		if err := ValidateVector(v, ExpectedDimensions); !errors.Is(err, ErrBadResponse) {
			t.Fatalf("expected ErrBadResponse, got %v", err)
		}
	})
	t.Run("skips width check when disabled", func(t *testing.T) {
		if err := ValidateVector(normalizeInPlace([]float64{3, 4}), 0); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

func TestDefaultsMatchVerifiedConfiguration(t *testing.T) {
	c, err := NewClient("https://example.invalid", "k")
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if c.maxBatch != DefaultMaxBatchSize {
		t.Fatalf("expected default batch size %d, got %d", DefaultMaxBatchSize, c.maxBatch)
	}
	if c.dims != ExpectedDimensions {
		t.Fatalf("expected default dimensions %d, got %d", ExpectedDimensions, c.dims)
	}
	if c.backoff != defaultBackoff {
		t.Fatalf("expected default backoff %v, got %v", defaultBackoff, c.backoff)
	}
}

// vectorsJSON returns n distinct unit vectors.
func vectorsJSON(n int) string {
	out := make([][]float64, n)
	for i := range out {
		out[i] = testVector(float64(i + 1))
	}
	b, _ := json.Marshal(out)
	return string(b)
}

// defectJSON returns n vectors with a controlled width and value, used to reach
// the per-vector validation that a count mismatch would otherwise mask.
func defectJSON(n, dims int, scale float64) string {
	out := make([][]float64, n)
	for i := range out {
		v := make([]float64, dims)
		for j := range v {
			v[j] = scale
		}
		out[i] = v
	}
	b, _ := json.Marshal(out)
	return string(b)
}
