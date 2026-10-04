package review

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
)

func RegisterRoutes(group *gin.RouterGroup, svc *Service) {
	group.GET("/review/queue", func(c *gin.Context) {
		items, err := svc.PendingQueue(c.Request.Context())
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, items)
	})

	group.POST("/review/:explanationId/decision", func(c *gin.Context) {
		var body struct {
			ReviewerID string `json:"reviewer_id" binding:"required"`
			Decision   string `json:"decision" binding:"required"`
			Comments   string `json:"comments"`
			// Text is the reviewer's corrected text, and is required for
			// edit_approve. It is not a general-purpose content write: without a
			// decision it changes nothing, so "published" still cannot happen
			// except through a reviewer.
			Text string `json:"text"`
		}
		if err := c.ShouldBindJSON(&body); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		err := svc.Decide(c.Request.Context(), c.Param("explanationId"), body.ReviewerID, body.Decision, body.Comments, body.Text)
		switch {
		case err == nil:
			c.JSON(http.StatusOK, gin.H{"status": "recorded"})
		case errors.Is(err, ErrNotPending):
			c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
		case errors.Is(err, ErrBadDecision):
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		default:
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		}
	})
}
