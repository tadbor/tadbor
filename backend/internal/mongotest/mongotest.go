// Package mongotest supplies the connection details for MongoDB-backed tests.
//
// Locally, tests skip when MONGO_TEST_URI is unset so a plain `go test ./...`
// stays hermetic. In CI the variable is mandatory: a missing service or a
// misconfigured workflow must fail rather than silently skip the integration
// coverage.
package mongotest

import (
	"os"
	"testing"
)

// URI returns MONGO_TEST_URI, skipping outside CI when it is unset and failing
// in CI when it is unset.
func URI(t *testing.T) string {
	t.Helper()

	uri := os.Getenv("MONGO_TEST_URI")
	if uri != "" {
		return uri
	}
	if InCI() {
		t.Fatal("MONGO_TEST_URI is required in CI; MongoDB integration tests must not be skipped")
	}
	t.Skip("set MONGO_TEST_URI to run MongoDB-backed tests")
	return ""
}

// InCI reports whether tests are running in a CI environment.
func InCI() bool {
	return os.Getenv("CI") != "" || os.Getenv("GITHUB_ACTIONS") != ""
}
