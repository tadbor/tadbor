package platform

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func newGatedRouter(password string) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	g := r.Group("", ReviewerAuthMiddleware(password))
	g.GET("/internal/thing", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"ok": true}) })
	return r
}

func TestReviewerAuthAllowsTheCorrectPassword(t *testing.T) {
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/internal/thing", nil)
	req.Header.Set(ReviewerPasswordHeader, "hunter2")

	newGatedRouter("hunter2").ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
}

func TestReviewerAuthRejectsAWrongOrMissingPassword(t *testing.T) {
	cases := map[string]string{
		"wrong password": "hunter3",
		"empty header":   "",
		"a prefix":       "hunter",
		"with a suffix":  "hunter2 ",
	}
	for name, presented := range cases {
		t.Run(name, func(t *testing.T) {
			w := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/internal/thing", nil)
			if presented != "" {
				req.Header.Set(ReviewerPasswordHeader, presented)
			}

			newGatedRouter("hunter2").ServeHTTP(w, req)

			if w.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want 401", w.Code)
			}
		})
	}
}

// The whole project rests on "nothing is published without a human decision", so
// an unset password must close /internal rather than open it.
func TestReviewerAuthFailsClosedWhenThePasswordIsUnset(t *testing.T) {
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/internal/thing", nil)
	// Even a caller who somehow presents a password must not get in.
	req.Header.Set(ReviewerPasswordHeader, "anything")

	newGatedRouter("").ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", w.Code)
	}
	if body := w.Body.String(); !strings.Contains(body, "not set") {
		t.Errorf("body = %q, want an explanation of the missing setting", body)
	}
}
