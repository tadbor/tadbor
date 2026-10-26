package content

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
)

// ExplanationReader is the read surface for the public explanation route.
type ExplanationReader interface {
	GetExplanations(ctx context.Context, surahID int, style string) ([]Explanation, error)
}

// IntroductionReader is the read surface for the public introduction route.
type IntroductionReader interface {
	GetIntroduction(ctx context.Context, surahID int, style string) (*SurahIntroduction, error)
}

func RegisterRoutes(router *gin.Engine, svc ExplanationReader) {
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
		if explanations == nil {
			explanations = []Explanation{}
		}
		c.JSON(http.StatusOK, explanations)
	})
}

func RegisterIntroductionRoutes(router *gin.Engine, svc IntroductionReader) {
	router.GET("/surahs/:surahId/introduction", func(c *gin.Context) {
		surahID, err := strconv.Atoi(c.Param("surahId"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid surah id"})
			return
		}
		style := c.DefaultQuery("style", "simplified_ar")
		intro, err := svc.GetIntroduction(c.Request.Context(), surahID, style)
		if err != nil {
			if errors.Is(err, ErrIntroductionProcessing) {
				c.JSON(http.StatusAccepted, gin.H{"status": "processing"})
				return
			}
			if errors.Is(err, ErrIntroductionUnavailable) {
				c.JSON(http.StatusNotFound, gin.H{"status": "unavailable"})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{"error": "introduction service unavailable"})
			return
		}
		c.JSON(http.StatusOK, intro)
	})
}
