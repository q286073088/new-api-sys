package middleware

import (
	"context"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
)

func RequestId() func(c *gin.Context) {
	return func(c *gin.Context) {
		id := common.NewRequestId()
		c.Set(common.RequestIdKey, id)
		ctx := context.WithValue(common.WithRequestTimeline(c.Request.Context()), common.RequestIdKey, id)
		c.Request = c.Request.WithContext(ctx)
		c.Header(common.RequestIdKey, id)
		c.Next()
	}
}

// RequestPhase records a boundary before subsequent middleware executes.
func RequestPhase(name string) gin.HandlerFunc {
	return func(c *gin.Context) { common.MarkRequestPhase(c.Request.Context(), name) }
}
