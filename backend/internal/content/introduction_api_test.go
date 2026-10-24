package content

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/mongo/integration/mtest"
)

func TestIntroductionRouteRegistered(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mt := mtest.New(t, mtest.NewOptions().ClientType(mtest.Mock))
	defer mt.Close()

	mt.Run("introduction route exists", func(mt *mtest.T) {
		r := gin.New()
		svc := NewService(mt.DB)
		RegisterIntroductionRoutes(r, svc)

		req := httptest.NewRequest(http.MethodGet, "/surahs/12/introduction", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code == http.StatusNotFound {
			// Check if it's route not found vs our 404
			body := w.Body.String()
			if body == "404 page not found" {
				t.Fatal("route not registered")
			}
		}
	})
}
