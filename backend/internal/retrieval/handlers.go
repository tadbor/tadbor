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

		// GetEvidenceForAyah runs exact mapping first and only computes a thematic
		// query embedding if the ayah turns out to have no mapped evidence.
		evidence, err := svc.GetEvidenceForAyah(c.Request.Context(), surahID, ayah)
		if errors.Is(err, ErrInsufficientEvidence) {
			c.JSON(http.StatusUnprocessableEntity, gin.H{"status": "INSUFFICIENT_EVIDENCE"})
			return
		}
		if err != nil {
			// A dependency failure here is not the same as "no evidence" and must
			// not be reported as one, or a provider outage reads as a content gap.
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, evidence)
	})
}
