package content

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/mongo"
)

type fakeIntroductionRepository struct {
	intro          *SurahIntroduction
	pending        bool
	findErr        error
	attributionErr error
	attributions   []SourceAttribution
	gotSurahID     int
	gotStyle       string
	gotRefs        []string
}

func (r *fakeIntroductionRepository) FindPublished(_ context.Context, surahID int, style string) (*SurahIntroduction, error) {
	r.gotSurahID, r.gotStyle = surahID, style
	if r.findErr != nil {
		return nil, r.findErr
	}
	if r.intro == nil {
		return nil, mongo.ErrNoDocuments
	}
	return r.intro, nil
}

func (r *fakeIntroductionRepository) HasUnpublished(_ context.Context, surahID int, style string) (bool, error) {
	r.gotSurahID, r.gotStyle = surahID, style
	return r.pending, nil
}

func (r *fakeIntroductionRepository) FindAttributions(_ context.Context, refs []string) ([]SourceAttribution, error) {
	r.gotRefs = append([]string(nil), refs...)
	if r.attributionErr != nil {
		return nil, r.attributionErr
	}
	return r.attributions, nil
}

func TestGetIntroductionUsesExactStyleAndResolvesAttribution(t *testing.T) {
	repo := &fakeIntroductionRepository{
		intro: &SurahIntroduction{
			ID: "intro-12", SurahID: 12, Style: "simplified_ar", Level: "beginner",
			SourceRefs: []string{"chunk-1"}, ReviewStatus: "published",
		},
		attributions: []SourceAttribution{{
			SourceID: "ibn-kathir-ar", Title: "Tafsir Ibn Kathir", Author: "Ibn Kathir",
			SourceVersion: "v1", SourceRefs: []string{"chunk-1"},
		}},
	}
	svc := NewServiceWithIntroductionRepository(repo)

	got, err := svc.GetIntroduction(context.Background(), 12, "simplified_ar")
	if err != nil {
		t.Fatalf("GetIntroduction: %v", err)
	}
	if repo.gotSurahID != 12 || repo.gotStyle != "simplified_ar" {
		t.Fatalf("lookup = (%d, %q), want (12, simplified_ar)", repo.gotSurahID, repo.gotStyle)
	}
	if len(repo.gotRefs) != 1 || repo.gotRefs[0] != "chunk-1" {
		t.Fatalf("attribution refs = %v, want [chunk-1]", repo.gotRefs)
	}
	if len(got.Attributions) != 1 || got.Attributions[0].SourceID != "ibn-kathir-ar" {
		t.Fatalf("attributions = %+v, want registered source", got.Attributions)
	}
}

func TestGetIntroductionDistinguishesProcessingFromUnavailable(t *testing.T) {
	for _, tc := range []struct {
		name    string
		pending bool
		wantErr error
	}{
		{name: "processing", pending: true, wantErr: ErrIntroductionProcessing},
		{name: "unavailable", wantErr: ErrIntroductionUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &fakeIntroductionRepository{pending: tc.pending}
			_, err := NewServiceWithIntroductionRepository(repo).GetIntroduction(context.Background(), 12, "en")
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("GetIntroduction error = %v, want %v", err, tc.wantErr)
			}
			if repo.gotStyle != "en" {
				t.Fatalf("style lookup = %q, want en", repo.gotStyle)
			}
		})
	}
}

func TestPublicIntroductionJSONHidesInternalFields(t *testing.T) {
	intro := SurahIntroduction{
		ID: "intro-12", SurahID: 12, Style: "simplified_ar", ReviewStatus: "published",
		SourceVersionSnapshot: "private-snapshot", PromptVersion: "private-prompt",
		ModelVersion: "private-model", Sections: []Section{{Type: "themes", Title: "Themes", Content: "grounded", SourceRefs: []string{"chunk-1"}}},
		Attributions: []SourceAttribution{{SourceID: "source", Title: "Tafsir", Author: "Author", SourceVersion: "v1", SourceRefs: []string{"chunk-1"}}},
	}
	body, err := json.Marshal(intro)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	for _, forbidden := range []string{"review_status", "source_version_snapshot", "private-snapshot", "prompt_version", "private-prompt", "model_version", "private-model"} {
		if strings.Contains(string(body), forbidden) {
			t.Errorf("public JSON leaks %q: %s", forbidden, body)
		}
	}
	if !strings.Contains(string(body), `"attributions"`) || !strings.Contains(string(body), `"source_refs"`) {
		t.Errorf("public JSON omits source attribution: %s", body)
	}
}

func TestIntroductionHTTPResponses(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name       string
		repo       *fakeIntroductionRepository
		path       string
		wantStatus int
		wantBody   string
	}{
		{
			name: "published response and default style",
			repo: &fakeIntroductionRepository{
				intro:        &SurahIntroduction{ID: "intro-12", SurahID: 12, Style: "simplified_ar", ReviewStatus: "published", SourceRefs: []string{"chunk-1"}},
				attributions: []SourceAttribution{{SourceID: "source-1", Title: "Source", Author: "Author", SourceVersion: "v1", SourceRefs: []string{"chunk-1"}}},
			},
			path: "/surahs/12/introduction", wantStatus: http.StatusOK, wantBody: `"id":"intro-12"`,
		},
		{
			name: "explicit style",
			repo: &fakeIntroductionRepository{
				intro:        &SurahIntroduction{ID: "intro-en", SurahID: 12, Style: "en", ReviewStatus: "published", SourceRefs: []string{"chunk-1"}},
				attributions: []SourceAttribution{{SourceID: "source-1", Title: "Source", Author: "Author", SourceVersion: "v1", SourceRefs: []string{"chunk-1"}}},
			},
			path: "/surahs/12/introduction?style=en", wantStatus: http.StatusOK, wantBody: `"style":"en"`,
		},
		{
			name: "processing",
			repo: &fakeIntroductionRepository{pending: true},
			path: "/surahs/12/introduction?style=en", wantStatus: http.StatusAccepted, wantBody: `"status":"processing"`,
		},
		{
			name: "unavailable",
			repo: &fakeIntroductionRepository{},
			path: "/surahs/12/introduction?style=en", wantStatus: http.StatusNotFound, wantBody: `"status":"unavailable"`,
		},
		{
			name: "internal error is sanitized",
			repo: &fakeIntroductionRepository{
				intro:          &SurahIntroduction{ID: "private", SurahID: 12, Style: "en", SourceRefs: []string{"secret-ref"}},
				attributionErr: errors.New("mongo connection details and secret-ref"),
			},
			path: "/surahs/12/introduction?style=en", wantStatus: http.StatusInternalServerError,
			wantBody: `"error":"introduction service unavailable"`,
		},
		{
			name: "invalid surah id",
			repo: &fakeIntroductionRepository{},
			path: "/surahs/nope/introduction", wantStatus: http.StatusBadRequest, wantBody: `"error":"invalid surah id"`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			router := gin.New()
			RegisterIntroductionRoutes(router, NewServiceWithIntroductionRepository(tc.repo))
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, tc.path, nil))
			if response.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d; body=%s", response.Code, tc.wantStatus, response.Body.String())
			}
			if !strings.Contains(response.Body.String(), tc.wantBody) {
				t.Errorf("body = %s, want substring %s", response.Body.String(), tc.wantBody)
			}
			if tc.name == "internal error is sanitized" && strings.Contains(response.Body.String(), "secret-ref") {
				t.Errorf("public response leaked internal detail: %s", response.Body.String())
			}
			if tc.wantStatus == http.StatusOK && (tc.repo.gotSurahID != 12 || tc.repo.gotStyle != expectedStyle(tc.path)) {
				t.Errorf("lookup = (%d, %q), unexpected for %s", tc.repo.gotSurahID, tc.repo.gotStyle, tc.path)
			}
		})
	}
}

func expectedStyle(path string) string {
	if strings.Contains(path, "style=en") {
		return "en"
	}
	return "simplified_ar"
}

func TestPublishedIntroductionFilterRequiresPublishedExactStyle(t *testing.T) {
	filter := publishedIntroductionFilter(12, "en")
	if filter["surah_id"] != 12 || filter["style"] != "en" || filter["review_status"] != "published" {
		t.Fatalf("published filter = %#v, want surah 12, exact style en, published status", filter)
	}
}
