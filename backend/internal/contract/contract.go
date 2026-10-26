// Package contract loads the public OpenAPI contract and validates fixtures and
// handler responses against it. It is shared by handler tests in each service
// package so a response-shape change fails in the pull request that introduces
// the drift.
package contract

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
)

const (
	SpecRelativePath     = "docs/api/openapi.yaml"
	FixturesRelativePath = "backend/testdata/fixtures/contract"
	ManifestFile         = "manifest.json"
	JSONMediaType        = "application/json"
)

// Fixture maps one JSON fixture file to an OpenAPI operation response.
type Fixture struct {
	Name        string `json:"name"`
	OperationID string `json:"operationId"`
	Status      int    `json:"status"`
	File        string `json:"file"`
}

// RepoRoot walks up from this file until it finds the OpenAPI contract. Tests
// can therefore run from any package directory.
func RepoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("contract: cannot determine source location")
	}
	dir := filepath.Dir(file)
	for {
		if _, err := os.Stat(filepath.Join(dir, SpecRelativePath)); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("contract: repository root not found from %s", filepath.Dir(file))
		}
		dir = parent
	}
}

// LoadSpec loads and validates the OpenAPI document.
func LoadSpec(t *testing.T) *openapi3.T {
	t.Helper()
	loader := openapi3.NewLoader()
	loader.IsExternalRefsAllowed = true
	doc, err := loader.LoadFromFile(filepath.Join(RepoRoot(t), SpecRelativePath))
	if err != nil {
		t.Fatalf("contract: load %s: %v", SpecRelativePath, err)
	}
	if err := doc.Validate(context.Background()); err != nil {
		t.Fatalf("contract: invalid OpenAPI document: %v", err)
	}
	return doc
}

// FindOperation resolves an operation by operationId.
func FindOperation(t *testing.T, doc *openapi3.T, operationID string) *openapi3.Operation {
	t.Helper()
	if doc.Paths == nil {
		t.Fatal("contract: OpenAPI document has no paths")
	}
	for _, item := range doc.Paths.Map() {
		for _, operation := range item.Operations() {
			if operation.OperationID == operationID {
				return operation
			}
		}
	}
	t.Fatalf("contract: operation %q not found", operationID)
	return nil
}

// ValidateJSONResponse checks a body against the named operation and status.
func ValidateJSONResponse(t *testing.T, doc *openapi3.T, operationID string, status int, body []byte) {
	t.Helper()
	operation := FindOperation(t, doc, operationID)

	responseRef := operation.Responses.Status(status)
	if responseRef == nil || responseRef.Value == nil {
		t.Fatalf("contract: operation %q has no response for status %d", operationID, status)
	}
	media := responseRef.Value.Content.Get(JSONMediaType)
	if media == nil || media.Schema == nil || media.Schema.Value == nil {
		t.Fatalf("contract: operation %q status %d has no %s schema", operationID, status, JSONMediaType)
	}

	var value any
	if err := json.Unmarshal(body, &value); err != nil {
		t.Fatalf("contract: response for %q status %d is not valid JSON: %v\nbody: %s", operationID, status, err, body)
	}
	if err := media.Schema.Value.VisitJSON(value); err != nil {
		t.Fatalf("contract: response for %q status %d does not match the schema: %v\nbody: %s", operationID, status, err, body)
	}
}

// LoadFixtures reads the fixture manifest.
func LoadFixtures(t *testing.T) []Fixture {
	t.Helper()
	raw := ReadFixture(t, ManifestFile)
	var fixtures []Fixture
	if err := json.Unmarshal(raw, &fixtures); err != nil {
		t.Fatalf("contract: parse %s: %v", ManifestFile, err)
	}
	if len(fixtures) == 0 {
		t.Fatalf("contract: %s has no fixtures", ManifestFile)
	}
	return fixtures
}

// ReadFixture returns a fixture file's raw bytes.
func ReadFixture(t *testing.T, file string) []byte {
	t.Helper()
	path := filepath.Join(RepoRoot(t), FixturesRelativePath, file)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("contract: read fixture %s: %v", file, err)
	}
	return raw
}

// UnmarshalFixture decodes a fixture into dst.
func UnmarshalFixture(t *testing.T, file string, dst any) {
	t.Helper()
	if err := json.Unmarshal(ReadFixture(t, file), dst); err != nil {
		t.Fatalf("contract: decode fixture %s: %v", file, err)
	}
}

// FixturePath returns the absolute path of a fixture.
func FixturePath(t *testing.T, file string) string {
	t.Helper()
	return filepath.Join(RepoRoot(t), FixturesRelativePath, file)
}

// FixtureDir returns the absolute fixture directory.
func FixtureDir(t *testing.T) string {
	t.Helper()
	return filepath.Join(RepoRoot(t), FixturesRelativePath)
}

// MustMarshal encodes a handler value as JSON or fails the test.
func MustMarshal(t *testing.T, value any) []byte {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("contract: marshal response: %v", err)
	}
	return raw
}

// LoadFixtureAs is a small generic helper for handler tests.
func LoadFixtureAs[T any](t *testing.T, file string) T {
	t.Helper()
	var value T
	UnmarshalFixture(t, file, &value)
	return value
}
