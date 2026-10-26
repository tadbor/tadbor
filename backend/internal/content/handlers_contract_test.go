package content

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"tadbor/backend/internal/contract"

	"github.com/gin-gonic/gin"
)

type fakeContentReader struct {
	explanations []Explanation
	err          error

	gotExplanationStyle string
}

func (f *fakeContentReader) GetExplanations(_ context.Context, _ int, style string) ([]Explanation, error) {
	f.gotExplanationStyle = style
	return f.explanations, f.err
}

func TestExplanationHandlerMatchesContract(t *testing.T) {
	gin.SetMode(gin.TestMode)
	doc := contract.LoadSpec(t)
	available := contract.LoadFixtureAs[[]Explanation](t, "explanations_available.json")

	tests := []struct {
		name       string
		path       string
		reader     *fakeContentReader
		wantStatus int
		wantStyle  string
	}{
		{
			name:       "available default style",
			path:       "/surahs/12/explanations",
			reader:     &fakeContentReader{explanations: available},
			wantStatus: http.StatusOK,
			wantStyle:  "simplified_ar",
		},
		{
			name:       "empty exact style",
			path:       "/surahs/12/explanations?style=en",
			reader:     &fakeContentReader{},
			wantStatus: http.StatusOK,
			wantStyle:  "en",
		},
		{
			name:       "invalid surah id",
			path:       "/surahs/not-a-number/explanations",
			reader:     &fakeContentReader{},
			wantStatus: http.StatusBadRequest,
			wantStyle:  "",
		},
		{
			name:       "storage error",
			path:       "/surahs/12/explanations?style=egyptian_ar",
			reader:     &fakeContentReader{err: errors.New("fixture storage failure")},
			wantStatus: http.StatusInternalServerError,
			wantStyle:  "egyptian_ar",
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			router := gin.New()
			RegisterRoutes(router, test.reader)

			response := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodGet, test.path, nil)
			router.ServeHTTP(response, request)

			if response.Code != test.wantStatus {
				t.Fatalf("status = %d, want %d; body: %s", response.Code, test.wantStatus, response.Body.String())
			}
			if test.wantStyle != "" && test.reader.gotExplanationStyle != test.wantStyle {
				t.Fatalf("service received style %q, want %q (no cross-style fallback)", test.reader.gotExplanationStyle, test.wantStyle)
			}
			contract.ValidateJSONResponse(t, doc, "listSurahExplanations", response.Code, response.Body.Bytes())
		})
	}
}
