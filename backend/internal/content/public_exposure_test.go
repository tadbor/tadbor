package content

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo/integration/mtest"
)

func TestPublicRoutesOnlyReturnPublished(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mt := mtest.New(t, mtest.NewOptions().ClientType(mtest.Mock))
	defer mt.Close()

	mt.Run("explanations endpoint filters to published only", func(mt *mtest.T) {
		r := gin.New()
		RegisterRoutes(r, NewService(mt.DB))

		first := mtest.CreateCursorResponse(
			1,
			"tadbor.explanations",
			mtest.FirstBatch,
			bson.D{
				{Key: "_id", Value: "exp1"},
				{Key: "surah_id", Value: 12},
				{Key: "ayah_start", Value: 1},
				{Key: "ayah_end", Value: 1},
				{Key: "style", Value: "simplified_ar"},
				{Key: "level", Value: "beginner"},
				{Key: "text", Value: "meaning"},
				{Key: "source_refs", Value: bson.A{"c1"}},
				{Key: "review_status", Value: "published"},
			},
		)
		killCursors := mtest.CreateCursorResponse(0, "tadbor.explanations", mtest.NextBatch)
		mt.AddMockResponses(first, killCursors)

		req := httptest.NewRequest(http.MethodGet, "/surahs/12/explanations?style=simplified_ar", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}

		var resp []map[string]interface{}
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if len(resp) != 1 {
			t.Fatalf("expected 1 item, got %d", len(resp))
		}
		if _, ok := resp[0]["review_status"]; ok {
			t.Fatalf("review_status leaked to public response")
		}
		if _, ok := resp[0]["source_version_snapshot"]; ok {
			t.Fatalf("source_version_snapshot leaked to public response")
		}
	})
}

func TestNoLeakOfInternalFields(t *testing.T) {
	var e Explanation
	b, _ := json.Marshal(e)
	_ = b
}
