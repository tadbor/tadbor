package review

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"tadbor/backend/internal/platform"

	"github.com/gin-gonic/gin"
)

type fakeIntroductionReviewer struct {
	called     bool
	calls      int
	decision   string
	checklist  IntroductionChecklist
	reviewerID string
}

func (f *fakeIntroductionReviewer) PendingIntroductions(context.Context) ([]IntroductionQueueItem, error) {
	return []IntroductionQueueItem{}, nil
}

func (f *fakeIntroductionReviewer) DecideIntroduction(_ context.Context, _, reviewerID, decision, _ string, checklist IntroductionChecklist) error {
	f.called = true
	f.calls++
	f.reviewerID = reviewerID
	f.decision = decision
	f.checklist = checklist
	return nil
}

func completeChecklist() IntroductionChecklist {
	return IntroductionChecklist{
		ClaimsSupported: true, DisagreementsFair: true, NoQuranMisattribution: true,
		ClassificationChecked: true, HedgingChecked: true, CitationsResolve: true,
		CommentarySeparated: true,
	}
}

func TestIntroductionChecklistRequiresEveryCriterion(t *testing.T) {
	checklist := completeChecklist()
	if !checklist.Complete() {
		t.Fatal("complete checklist did not pass")
	}
	checklist.HedgingChecked = false
	if checklist.Complete() {
		t.Fatal("checklist passed with a required criterion missing")
	}
}

func TestIntroductionDecisionRouteRequiresReviewerAuthentication(t *testing.T) {
	gin.SetMode(gin.TestMode)
	reviewer := &fakeIntroductionReviewer{}
	router := gin.New()
	group := router.Group("/internal", platform.ReviewerAuthMiddleware("secret"))
	RegisterIntroductionRoutes(group, reviewer)

	body, _ := json.Marshal(map[string]any{
		"reviewer_id": "reviewer", "decision": "approve", "checklist": completeChecklist(),
	})
	request := httptest.NewRequest(http.MethodPost, "/internal/review/introductions/intro-12/decision", strings.NewReader(string(body)))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated status = %d, want %d", response.Code, http.StatusUnauthorized)
	}
	if reviewer.called {
		t.Fatal("unauthenticated decision reached the review service")
	}
}

func TestIntroductionDecisionRouteFailsClosedEvenWithSharedPassword(t *testing.T) {
	gin.SetMode(gin.TestMode)
	reviewer := &fakeIntroductionReviewer{}
	router := gin.New()
	group := router.Group("/internal", platform.ReviewerAuthMiddleware("secret"))
	RegisterIntroductionRoutes(group, reviewer)

	body, _ := json.Marshal(map[string]any{
		"reviewer_id": "spoofed-person", "decision": "approve", "checklist": completeChecklist(),
	})
	request := httptest.NewRequest(http.MethodPost, "/internal/review/introductions/intro-12/decision", strings.NewReader(string(body)))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set(platform.ReviewerPasswordHeader, "secret")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("authenticated status = %d, want %d; body=%s", response.Code, http.StatusServiceUnavailable, response.Body.String())
	}
	if reviewer.called || reviewer.calls != 0 {
		t.Fatalf("disabled decision route called reviewer service: %+v", reviewer)
	}
	if !strings.Contains(response.Body.String(), "trusted reviewer identity") {
		t.Fatalf("response = %s, want missing identity prerequisite", response.Body.String())
	}
}

func TestIntroductionDecisionRouteRejectsUnauthenticatedRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)
	reviewer := &fakeIntroductionReviewer{}
	router := gin.New()
	group := router.Group("/internal", platform.ReviewerAuthMiddleware("secret"))
	RegisterIntroductionRoutes(group, reviewer)

	body, _ := json.Marshal(map[string]any{
		"reviewer_id": "spoofed-person", "decision": "approve", "checklist": completeChecklist(),
	})
	request := httptest.NewRequest(http.MethodPost, "/internal/review/introductions/intro-12/decision", strings.NewReader(string(body)))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated status = %d, want %d", response.Code, http.StatusUnauthorized)
	}
	if reviewer.called {
		t.Fatal("unauthenticated decision reached review service")
	}
}
