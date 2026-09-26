package content

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
)

func RegisterRoutes(router *gin.Engine, svc *Service) {
	router.GET("/surahs/:surahId/explanations", func(c *gin.Context) {
		surahID, err := strconv.Atoi(c.Param("surahId"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid surah id"})
			return
		}
		style := c.DefaultQuery("style", "simplified_ar")

		explanations, err := svc.GetExplanations(c.Request.Context(), surahID, style)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, explanations)
	})
}
