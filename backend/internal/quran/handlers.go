package quran

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
)

func RegisterRoutes(router *gin.Engine, svc *Service) {
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
		c.JSON(http.StatusOK, ayahs)
	})
}
