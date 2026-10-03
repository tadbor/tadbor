// Package embedding is the offline-only client for the sentence-embedding
// provider that backs the ingestion pipeline (docs/rag-architecture-part2.md
// §26 — a free multilingual model run through a hosted inference API).
//
// It is deliberately its own package rather than living inside
// internal/ingestion: the embedding provider is a shared dependency of the
// offline ingestion pipeline and, later, of query-time embedding for the
// retrieval service. Keeping the HTTP client here means neither caller needs to
// know the provider's wire format.
//
// Scope note: this package embeds *tafsir* text only. Quranic text is never
// embedded — verse identification is always exact (Part 1 §5).
package embedding

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	// ModelID is the verified model behind EMBEDDING_API_URL. It is recorded on
	// every stored vector so a future model change can be detected instead of
	// silently mixing embedding spaces in the same collection.
	ModelID = "BAAI/bge-m3"

	// ExpectedDimensions is bge-m3's output width. Verified against the live
	// endpoint; a response of any other width is rejected rather than stored.
	ExpectedDimensions = 1024

	// DefaultMaxBatchSize is the per-request input cap. The provider accepted 64
	// inputs per request when verified, but 32 keeps retry granularity (and
	// therefore wasted work) smaller on failure.
	DefaultMaxBatchSize = 32

	maxAttempts     = 3
	defaultBackoff  = 500 * time.Millisecond
	requestTimeout  = 60 * time.Second
	normTolerance   = 0.01
	maxResponseSize = 16 << 20 // 16 MiB: ~1.3 MiB for a full batch of 1024-dim vectors
	maxErrDetail    = 200
)

// Sentinel errors. Callers classify failures with errors.Is; only transport
// errors and provider 5xx/429 responses are ever retried.
var (
	ErrMissingConfig   = errors.New("embedding: EMBEDDING_API_URL and EMBEDDING_API_KEY are required")
	ErrUnauthorized    = errors.New("embedding: provider rejected the api key")
	ErrBadRequest      = errors.New("embedding: provider rejected the request")
	ErrRateLimited     = errors.New("embedding: provider rate limited the request")
	ErrUnavailable     = errors.New("embedding: provider unavailable")
	ErrBadResponse     = errors.New("embedding: malformed or invalid provider response")
	ErrUnexpectedShape = errors.New("embedding: unexpected vector shape")
)

// BatchError reports which slice of a caller's input failed. Start/End index the
// original texts slice (half-open), so a caller can resume exactly where the
// failure happened instead of re-embedding everything before it.
type BatchError struct {
	Start    int
	End      int
	Attempts int
	Err      error
}

func (e *BatchError) Error() string {
	return fmt.Sprintf("embedding: inputs [%d:%d] failed after %d attempt(s): %v", e.Start, e.End, e.Attempts, e.Err)
}

func (e *BatchError) Unwrap() error { return e.Err }

// Embedder turns text into normalized dense vectors. One input yields one
// vector, and callers rely on the returned slice being index-aligned with the
// input slice.
type Embedder interface {
	Embed(ctx context.Context, texts []string) ([][]float64, error)
}

// Options overrides the verified defaults. The zero value is the production
// configuration; tests use it to shrink retry delays and use a stub server.
type Options struct {
	MaxBatchSize int
	Dimensions   int
	HTTPClient   *http.Client
	Backoff      time.Duration
}

// Client is an Embedder backed by a HuggingFace-style feature-extraction
// endpoint ({"inputs": [...], "normalize": true} in, one vector per input out).
type Client struct {
	endpoint string
	apiKey   string
	http     *http.Client
	maxBatch int
	dims     int
	backoff  time.Duration
}

var _ Embedder = (*Client)(nil)

// NewClient builds a client from EMBEDDING_API_URL / EMBEDDING_API_KEY. It
// fails fast on missing configuration rather than at the first request.
func NewClient(endpoint, apiKey string) (*Client, error) {
	return NewClientWithOptions(endpoint, apiKey, Options{})
}

func NewClientWithOptions(endpoint, apiKey string, opts Options) (*Client, error) {
	if strings.TrimSpace(endpoint) == "" || strings.TrimSpace(apiKey) == "" {
		return nil, ErrMissingConfig
	}
	maxBatch := opts.MaxBatchSize
	if maxBatch <= 0 {
		maxBatch = DefaultMaxBatchSize
	}
	dims := opts.Dimensions
	if dims <= 0 {
		dims = ExpectedDimensions
	}
	httpClient := opts.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: requestTimeout}
	}
	backoff := opts.Backoff
	if backoff <= 0 {
		backoff = defaultBackoff
	}
	return &Client{
		endpoint: strings.TrimSpace(endpoint),
		apiKey:   strings.TrimSpace(apiKey),
		http:     httpClient,
		maxBatch: maxBatch,
		dims:     dims,
		backoff:  backoff,
	}, nil
}

// Embed embeds every input, splitting into sub-batches of at most maxBatch.
// Results are returned in input order. A failure anywhere fails the whole call
// and reports the offending input range via *BatchError; no partial result is
// returned, so a caller can never persist half a batch.
func (c *Client) Embed(ctx context.Context, texts []string) ([][]float64, error) {
	if len(texts) == 0 {
		return nil, nil
	}

	out := make([][]float64, 0, len(texts))
	for start := 0; start < len(texts); start += c.maxBatch {
		end := start + c.maxBatch
		if end > len(texts) {
			end = len(texts)
		}
		vecs, err := c.embedSubBatch(ctx, texts[start:end])
		if err != nil {
			var be *BatchError
			if errors.As(err, &be) {
				// Re-base the range onto the caller's full input slice.
				return nil, &BatchError{Start: start + be.Start, End: start + be.End, Attempts: be.Attempts, Err: be.Err}
			}
			return nil, err
		}
		out = append(out, vecs...)
	}
	return out, nil
}

// embedSubBatch retries only transport failures, 429, and 5xx — up to
// maxAttempts with exponential backoff. 4xx and malformed responses fail
// immediately: retrying them just burns quota on a request that can never
// succeed.
//
// Errors are reported against this sub-batch's own indices; Embed re-bases them
// onto the caller's full input slice.
func (c *Client) embedSubBatch(ctx context.Context, batch []string) ([][]float64, error) {
	fail := func(attempt int, err error) ([][]float64, error) {
		return nil, &BatchError{Start: 0, End: len(batch), Attempts: attempt, Err: err}
	}
	for attempt := 1; ; attempt++ {
		vecs, retryable, err := c.embedOnce(ctx, batch)
		if err == nil {
			return vecs, nil
		}
		if !retryable || attempt >= maxAttempts {
			return fail(attempt, err)
		}

		timer := time.NewTimer(c.backoff << (attempt - 1))
		select {
		case <-ctx.Done():
			timer.Stop()
			return fail(attempt, ctx.Err())
		case <-timer.C:
		}
	}
}

type embedRequest struct {
	Inputs    []string `json:"inputs"`
	Normalize bool     `json:"normalize"`
}

type providerError struct {
	Error string `json:"error"`
}

func (c *Client) embedOnce(ctx context.Context, batch []string) ([][]float64, bool, error) {
	body, err := json.Marshal(embedRequest{Inputs: batch, Normalize: true})
	if err != nil {
		return nil, false, fmt.Errorf("embedding: encode request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, false, fmt.Errorf("embedding: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.http.Do(req)
	if err != nil {
		// Transport-level failures are worth another attempt. The api key is
		// never part of this error: it lives in the header, not the URL.
		return nil, true, fmt.Errorf("embedding: request failed: %w", err)
	}
	defer resp.Body.Close()

	payload, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseSize))
	if err != nil {
		return nil, true, fmt.Errorf("embedding: read response: %w", err)
	}

	switch {
	case resp.StatusCode == http.StatusOK:
	case resp.StatusCode == http.StatusTooManyRequests:
		return nil, true, fmt.Errorf("%w (status %d)", ErrRateLimited, resp.StatusCode)
	case resp.StatusCode == http.StatusUnauthorized, resp.StatusCode == http.StatusForbidden:
		return nil, false, fmt.Errorf("%w (status %d)", ErrUnauthorized, resp.StatusCode)
	case resp.StatusCode == http.StatusRequestEntityTooLarge, resp.StatusCode >= 500:
		return nil, true, fmt.Errorf("%w (status %d)", ErrUnavailable, resp.StatusCode)
	case resp.StatusCode >= 400:
		return nil, false, fmt.Errorf("%w (status %d): %s", ErrBadRequest, resp.StatusCode, detail(payload))
	default:
		return nil, false, fmt.Errorf("embedding: unexpected status %d", resp.StatusCode)
	}

	// A 2-D decode is deliberate: this model returns one vector per input. A
	// token-level (3-D) payload fails to decode here rather than silently
	// producing a wrong-shaped vector.
	var vectors [][]float64
	if err := json.Unmarshal(payload, &vectors); err != nil {
		return nil, false, fmt.Errorf("%w: cannot decode response: %v", ErrBadResponse, err)
	}
	if len(vectors) != len(batch) {
		return nil, false, fmt.Errorf("%w: got %d vectors for %d inputs", ErrBadResponse, len(vectors), len(batch))
	}
	for i, v := range vectors {
		if err := ValidateVector(v, c.dims); err != nil {
			return nil, false, fmt.Errorf("%w: vector %d: %v", ErrBadResponse, i, err)
		}
	}
	return vectors, false, nil
}

// ValidateVector rejects anything that would poison a cosine comparison later:
// wrong width, non-finite values, or an unnormalized vector. Retrieval computes
// cosine in Go (docs/ADDENDUM-mongodb-pivot.md), so a bad vector stored today
// silently degrades retrieval tomorrow.
func ValidateVector(v []float64, dims int) error {
	if dims > 0 && len(v) != dims {
		return fmt.Errorf("%w: expected %d dimensions, got %d", ErrUnexpectedShape, dims, len(v))
	}
	var sumSquares float64
	for _, f := range v {
		if math.IsNaN(f) || math.IsInf(f, 0) {
			return fmt.Errorf("%w: contains NaN or Inf", ErrBadResponse)
		}
		sumSquares += f * f
	}
	norm := math.Sqrt(sumSquares)
	if norm == 0 {
		return fmt.Errorf("%w: zero vector", ErrBadResponse)
	}
	if math.Abs(norm-1) > normTolerance {
		return fmt.Errorf("%w: vector is not normalized (L2 norm %.4f)", ErrBadResponse, norm)
	}
	return nil
}

// detail surfaces the provider's own error message for 4xx debugging, bounded
// so a provider that echoes the whole payload cannot flood a log line.
func detail(payload []byte) string {
	var pe providerError
	if err := json.Unmarshal(payload, &pe); err != nil || pe.Error == "" {
		return ""
	}
	msg := pe.Error
	if len(msg) > maxErrDetail {
		msg = msg[:maxErrDetail] + "…"
	}
	return strconv.Quote(msg)
}
