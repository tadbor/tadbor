package quran

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"tadbor/backend/internal/contract"

	"github.com/gin-gonic/gin"
)

type fakeAyahReader struct {
	ayahs []Ayah
	err   error
}

func (f fakeAyahReader) GetSurah(context.Context, int) ([]Ayah, error) {
	return f.ayahs, f.err
}

func TestAyahHandlerMatchesContract(t *testing.T) {
	gin.SetMode(gin.TestMode)
	doc := contract.LoadSpec(t)
	ayahs := contract.LoadFixtureAs[[]Ayah](t, "ayahs_available.json")

	tests := []struct {
		name       string
		path       string
		reader     fakeAyahReader
		wantStatus int
	}{
		{
			name:       "available",
			path:       "/surahs/12/ayahs",
			reader:     fakeAyahReader{ayahs: ayahs},
			wantStatus: http.StatusOK,
		},
		{
			name:       "no ayahs is an empty array",
			path:       "/surahs/12/ayahs",
			reader:     fakeAyahReader{},
			wantStatus: http.StatusOK,
		},
		{
			name:       "invalid surah id",
			path:       "/surahs/not-a-number/ayahs",
			reader:     fakeAyahReader{},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "storage error",
			path:       "/surahs/12/ayahs",
			reader:     fakeAyahReader{err: errors.New("fixture storage failure")},
			wantStatus: http.StatusInternalServerError,
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
			contract.ValidateJSONResponse(t, doc, "getSurahAyahs", response.Code, response.Body.Bytes())
		})
	}
}
