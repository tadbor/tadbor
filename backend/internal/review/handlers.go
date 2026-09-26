package review

import (
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
		}
		if err := c.ShouldBindJSON(&body); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		err := svc.Decide(c.Request.Context(), c.Param("explanationId"), body.ReviewerID, body.Decision, body.Comments)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"status": "recorded"})
	})
}
