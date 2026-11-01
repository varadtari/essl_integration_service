package middleware

import (
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// RequestId gives every request an id before any other middleware runs, so logs, error
// responses and the debug activity log can be joined on it. The template takes this from the
// AWS load balancer header; here there is no load balancer in front of the POC.
func RequestId() gin.HandlerFunc {
	return func(c *gin.Context) {

		requestId := c.GetHeader("X-Request-Id")
		if requestId == "" {
			requestId = uuid.New().String()
		}

		c.Set("requestId", requestId)
		c.Writer.Header().Set("X-Request-Id", requestId)

		c.Next()
	}
}
