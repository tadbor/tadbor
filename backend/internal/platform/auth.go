package platform

import (
	"crypto/subtle"
	"net/http"

	"github.com/gin-gonic/gin"
)

// ReviewerPasswordHeader carries the shared reviewer password from the admin
// dashboard to the API. It is a header rather than a query parameter so it does
// not end up in access logs, and rather than a cookie so the API itself never
// has to issue sessions — see docs/reviewer-dashboard.md for why that trade is
// acceptable for a single reviewer and what to do instead past localhost.
const ReviewerPasswordHeader = "X-Reviewer-Password"

// ReviewerAuthMiddleware gates everything mounted under /internal.
//
// The password comes from REVIEWER_PASSWORD and is compared in constant time.
// An unset password disables /internal rather than opening it: the failure mode
// of a missing secret must be a closed door, not a silent bypass of the
// mandatory-review gate this project is built around.
//
// This is a shared secret, not an identity system. It answers "is this request
// from the reviewer?" and nothing else — there is no per-person attribution, no
// expiry, and no revocation short of changing the password. Part 2 §27 asks for
// Supabase Auth; see docs/reviewer-dashboard.md for when that stops being
// optional.
func ReviewerAuthMiddleware(password string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if password == "" {
			c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{
				"error": "REVIEWER_PASSWORD is not set: /internal is disabled, not open",
			})
			return
		}

		presented := c.GetHeader(ReviewerPasswordHeader)
		if subtle.ConstantTimeCompare([]byte(presented), []byte(password)) != 1 {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": "reviewer password required",
			})
			return
		}

		c.Next()
	}
}
