package quran

import (
	"context"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
)

// AyahReader is the read surface the public handlers need. Keeping it narrow
// lets handler tests exercise the real HTTP contract without a live MongoDB.
type AyahReader interface {
	GetSurah(ctx context.Context, surahID int) ([]Ayah, error)
}

func RegisterRoutes(router *gin.Engine, svc AyahReader) {
	router.GET("/surahs/:surahId/ayahs", func(c *gin.Context) {
		surahID, err := strconv.Atoi(c.Param("surahId"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid surah id"})
			return
		}
		ayahs, err := svc.GetSurah(c.Request.Context(), surahID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if ayahs == nil {
			ayahs = []Ayah{}
		}
		c.JSON(http.StatusOK, ayahs)
	})
}
