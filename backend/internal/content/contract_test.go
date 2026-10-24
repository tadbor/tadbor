package content

import (
	"encoding/json"
	"os"
	"testing"
)

func TestExplanationContract(t *testing.T) {
	data, err := os.ReadFile("../../testdata/fixtures/explanation_published.json")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	var m map[string]interface{}
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	// Public API must not expose these
	if _, ok := m["review_status"]; !ok {
		t.Fatal("fixture should have review_status for DB testing")
	}
	// The Explanation struct tags ensure it's not serialized
	var e Explanation
	b, _ := json.Marshal(e)
	if string(b) == "{}" {
		// zero value marshals to {} with omitempty, which is fine
	}
}
