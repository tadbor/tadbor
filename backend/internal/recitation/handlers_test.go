package recitation

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"tadbor/backend/internal/contract"

	"github.com/gin-gonic/gin"
)

type fakeReader struct {
	reciters    []Reciter
	recitations []Recitation
	err         error

	gotReciterID string
}

func (f *fakeReader) ListReciters(context.Context) ([]Reciter, error) {
	return f.reciters, f.err
}

func (f *fakeReader) GetSurahRecitations(_ context.Context, _ int, reciterID string) ([]Recitation, error) {
	f.gotReciterID = reciterID
	return f.recitations, f.err
}

func TestReciterHandlerMatchesContract(t *testing.T) {
	gin.SetMode(gin.TestMode)
	doc := contract.LoadSpec(t)
	reciters := contract.LoadFixtureAs[[]Reciter](t, "reciters.json")

	tests := []struct {
		name       string
		reader     *fakeReader
		wantStatus int
	}{
		{name: "available", reader: &fakeReader{reciters: reciters}, wantStatus: http.StatusOK},
		{name: "storage error", reader: &fakeReader{err: errors.New("fixture storage failure")}, wantStatus: http.StatusInternalServerError},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			router := gin.New()
			RegisterRoutes(router, test.reader)

			response := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodGet, "/reciters", nil)
			router.ServeHTTP(response, request)

			if response.Code != test.wantStatus {
				t.Fatalf("status = %d, want %d; body: %s", response.Code, test.wantStatus, response.Body.String())
			}
			contract.ValidateJSONResponse(t, doc, "listReciters", response.Code, response.Body.Bytes())
		})
	}
}

func TestRecitationHandlerMatchesContract(t *testing.T) {
	gin.SetMode(gin.TestMode)
	doc := contract.LoadSpec(t)
	available := contract.LoadFixtureAs[[]Recitation](t, "recitations_available.json")

	tests := []struct {
		name        string
		path        string
		reader      *fakeReader
		wantStatus  int
		wantReciter string
	}{
		{
			name:        "available",
			path:        "/surahs/12/recitations?reciter=fixture-reciter-1",
			reader:      &fakeReader{recitations: available},
			wantStatus:  http.StatusOK,
			wantReciter: "fixture-reciter-1",
		},
		{
			name:        "empty for reciter",
			path:        "/surahs/12/recitations?reciter=fixture-reciter-2",
			reader:      &fakeReader{},
			wantStatus:  http.StatusOK,
			wantReciter: "fixture-reciter-2",
		},
		{
			name:       "invalid surah id",
			path:       "/surahs/not-a-number/recitations?reciter=fixture-reciter-1",
			reader:     &fakeReader{},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "missing reciter",
			path:       "/surahs/12/recitations",
			reader:     &fakeReader{},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "storage error",
			path:       "/surahs/12/recitations?reciter=fixture-reciter-1",
			reader:     &fakeReader{err: errors.New("fixture storage failure")},
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
			if test.wantReciter != "" && test.reader.gotReciterID != test.wantReciter {
				t.Fatalf("service received reciter %q, want %q", test.reader.gotReciterID, test.wantReciter)
			}
			contract.ValidateJSONResponse(t, doc, "listSurahRecitations", response.Code, response.Body.Bytes())
		})
	}
}
