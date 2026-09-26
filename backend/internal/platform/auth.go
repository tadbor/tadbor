package platform

import "github.com/gin-gonic/gin"

// ReviewerAuthMiddleware is a placeholder. Before deploying past localhost,
// replace this with real auth (e.g. Supabase Auth, or a simple signed-token
// check) gating everything under /internal — see Part 2 §27.
func ReviewerAuthMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		// TODO: verify reviewer identity here.
		c.Next()
	}
}
