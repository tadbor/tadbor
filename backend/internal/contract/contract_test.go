package contract_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"tadbor/backend/internal/contract"

	"github.com/getkin/kin-openapi/openapi3"
)

var requiredOperations = map[string]string{
	"getHealth":             "/health",
	"listSurahs":            "/surahs",
	"getSurahAyahs":         "/surahs/{surahId}/ayahs",
	"listSurahExplanations": "/surahs/{surahId}/explanations",
	"getSurahIntroduction":  "/surahs/{surahId}/introduction",
	"listReciters":          "/reciters",
	"listSurahRecitations":  "/surahs/{surahId}/recitations",
}

var canonicalStyles = []string{"simplified_ar", "egyptian_ar", "en"}

func TestSpecValidates(t *testing.T) {
	contract.LoadSpec(t)
}

func TestSpecDefinesMVPContract(t *testing.T) {
	doc := contract.LoadSpec(t)

	for operationID, path := range requiredOperations {
		item := doc.Paths.Value(path)
		if item == nil {
			t.Errorf("missing path %s", path)
			continue
		}
		found := false
		for _, operation := range item.Operations() {
			if operation.OperationID == operationID {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("path %s does not define operationId %s", path, operationID)
		}
	}

	readiness := schema(t, doc, "Readiness")
	if got, want := enumValues(readiness), []string{"available", "processing", "unavailable"}; !equalStrings(got, want) {
		t.Errorf("Readiness enum = %v, want %v", got, want)
	}

	styleID := schema(t, doc, "StyleId")
	if got := enumValues(styleID); !equalStrings(got, canonicalStyles) {
		t.Errorf("StyleId enum = %v, want %v", got, canonicalStyles)
	}

	for _, field := range []string{"number", "name_arabic", "name_transliteration", "ayah_count", "readiness"} {
		if _, ok := schema(t, doc, "Surah").Properties[field]; !ok {
			t.Errorf("Surah schema must define %q (the #32 metadata contract)", field)
		}
	}
	for _, field := range []string{"number", "name_arabic", "name_transliteration", "ayah_count", "readiness"} {
		if !contains(schema(t, doc, "Surah").Required, field) {
			t.Errorf("Surah schema must require %q", field)
		}
	}

	readinessFields := schema(t, doc, "SurahReadiness").Properties
	for _, field := range []string{"ayah_text", "introduction", "audio", "explanations"} {
		if _, ok := readinessFields[field]; !ok {
			t.Errorf("SurahReadiness schema must define %q", field)
		}
	}

	for _, schemaName := range []string{"Ayah", "Explanation", "Introduction", "Reciter", "Recitation"} {
		for _, forbidden := range []string{"review_status", "source_version_snapshot", "prompt_version", "model_version", "reviewer_id", "created_at", "updated_at"} {
			if _, ok := schema(t, doc, schemaName).Properties[forbidden]; ok {
				t.Errorf("%s schema must not expose internal field %q", schemaName, forbidden)
			}
		}
	}

	for _, operationID := range []string{"listSurahExplanations", "getSurahIntroduction"} {
		operation := contract.FindOperation(t, doc, operationID)
		for _, status := range []int{202, 404} {
			if operation.Responses.Status(status) == nil {
				t.Errorf("%s must document the %d state", operationID, status)
			}
		}
	}

	description := strings.ToLower(doc.Info.Description)
	for _, want := range []string{"never falls back", "#34", "#35", "#36", "200", "202", "404"} {
		if !strings.Contains(description, want) {
			t.Errorf("OpenAPI description must document %q", want)
		}
	}
}

func TestFixturesValidateAgainstSpec(t *testing.T) {
	doc := contract.LoadSpec(t)
	fixtures := contract.LoadFixtures(t)

	for _, fixture := range fixtures {
		fixture := fixture
		t.Run(fixture.Name, func(t *testing.T) {
			raw := contract.ReadFixture(t, fixture.File)
			contract.ValidateJSONResponse(t, doc, fixture.OperationID, fixture.Status, raw)
		})
	}
}

func TestFixturesCoverEveryReadinessState(t *testing.T) {
	type readinessFixture struct {
		Readiness struct {
			AyahText     string            `json:"ayah_text"`
			Introduction map[string]string `json:"introduction"`
			Audio        string            `json:"audio"`
			Explanations map[string]string `json:"explanations"`
		} `json:"readiness"`
	}

	var surahs []readinessFixture
	contract.UnmarshalFixture(t, "surahs.json", &surahs)
	if len(surahs) == 0 {
		t.Fatal("surahs.json is empty")
	}

	states := map[string]map[string]bool{}
	add := func(contentType, state string) {
		if states[contentType] == nil {
			states[contentType] = map[string]bool{}
		}
		states[contentType][state] = true
	}

	for _, surah := range surahs {
		add("ayah_text", surah.Readiness.AyahText)
		add("audio", surah.Readiness.Audio)
		for _, state := range surah.Readiness.Introduction {
			add("introduction", state)
		}
		for style, state := range surah.Readiness.Explanations {
			add("explanation:"+style, state)
		}
	}

	requireStates := func(contentType string, want ...string) {
		t.Helper()
		got := states[contentType]
		for _, state := range want {
			if !got[state] {
				t.Errorf("fixtures do not cover %q state for %q", state, contentType)
			}
		}
	}
	requireStates("ayah_text", "available", "unavailable")
	if states["ayah_text"]["processing"] {
		t.Error("ayah text must never be processing (immutable verified corpus)")
	}
	requireStates("audio", "available", "processing", "unavailable")
	requireStates("introduction", "available", "processing", "unavailable")
	for _, style := range canonicalStyles {
		requireStates("explanation:"+style, "available", "processing", "unavailable")
	}
}

func TestFixturesDoNotExposeInternalFields(t *testing.T) {
	fixtures := contract.LoadFixtures(t)
	forbidden := []string{"review_status", "source_version_snapshot", "prompt_version", "model_version", "reviewer_id", "created_at", "updated_at"}

	seen := map[string]bool{}
	for _, fixture := range fixtures {
		if seen[fixture.File] {
			continue
		}
		seen[fixture.File] = true
		raw := contract.ReadFixture(t, fixture.File)
		var value any
		if err := json.Unmarshal(raw, &value); err != nil {
			t.Fatalf("%s: %v", fixture.File, err)
		}
		assertNoKeys(t, fixture.File, value, forbidden)
	}
}

func TestFixtureFilesAreReferenced(t *testing.T) {
	fixtures := contract.LoadFixtures(t)
	referenced := map[string]bool{contract.ManifestFile: true}
	for _, fixture := range fixtures {
		referenced[fixture.File] = true
	}

	entries, err := os.ReadDir(contract.FixtureDir(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		if !referenced[entry.Name()] {
			t.Errorf("fixture %s is not referenced by %s", entry.Name(), contract.ManifestFile)
		}
	}
}

func schema(t *testing.T, doc *openapi3.T, name string) *openapi3.Schema {
	t.Helper()
	ref := doc.Components.Schemas[name]
	if ref == nil || ref.Value == nil {
		t.Fatalf("missing schema %q", name)
	}
	return ref.Value
}

func enumValues(schema *openapi3.Schema) []string {
	values := make([]string, 0, len(schema.Enum))
	for _, value := range schema.Enum {
		if text, ok := value.(string); ok {
			values = append(values, text)
		}
	}
	return values
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func equalStrings(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func assertNoKeys(t *testing.T, file string, value any, forbidden []string) {
	t.Helper()
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			for _, banned := range forbidden {
				if key == banned {
					t.Errorf("%s exposes internal field %q", file, key)
				}
			}
			assertNoKeys(t, file, child, forbidden)
		}
	case []any:
		for _, child := range typed {
			assertNoKeys(t, file, child, forbidden)
		}
	}
}
