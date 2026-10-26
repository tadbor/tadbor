package recitation

import (
	"context"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
)

// Reader is the read surface for the public audio routes.
type Reader interface {
	ListReciters(ctx context.Context) ([]Reciter, error)
	GetSurahRecitations(ctx context.Context, surahID int, reciterID string) ([]Recitation, error)
}

func RegisterRoutes(router *gin.Engine, svc Reader) {
	router.GET("/reciters", func(c *gin.Context) {
		reciters, err := svc.ListReciters(c.Request.Context())
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if reciters == nil {
			reciters = []Reciter{}
		}
		c.JSON(http.StatusOK, reciters)
	})

	router.GET("/surahs/:surahId/recitations", func(c *gin.Context) {
		surahID, err := strconv.Atoi(c.Param("surahId"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid surah id"})
			return
		}
		reciterID := c.Query("reciter")
		if reciterID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "reciter query param required"})
			return
		}

		recitations, err := svc.GetSurahRecitations(c.Request.Context(), surahID, reciterID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if recitations == nil {
			recitations = []Recitation{}
		}
		c.JSON(http.StatusOK, recitations)
	})
}
