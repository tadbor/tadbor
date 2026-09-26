package retrieval

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
)

// RegisterRoutes attaches internal-only retrieval endpoints. Used by the
// generation pipeline and the reviewer dashboard, never by the reader app.
func RegisterRoutes(group *gin.RouterGroup, svc *Service) {
	group.GET("/retrieval/:surahId/:ayah", func(c *gin.Context) {
		surahID, err1 := strconv.Atoi(c.Param("surahId"))
		ayah, err2 := strconv.Atoi(c.Param("ayah"))
		if err1 != nil || err2 != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid surah/ayah"})
			return
		}

		// A real query embedding would be computed for thematic fallback use;
		// nil is fine here since exact mapping is tried first.
		evidence, err := svc.GetEvidence(c.Request.Context(), surahID, ayah, nil)
		if errors.Is(err, ErrInsufficientEvidence) {
			c.JSON(http.StatusUnprocessableEntity, gin.H{"status": "INSUFFICIENT_EVIDENCE"})
			return
		}
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, evidence)
	})
}
