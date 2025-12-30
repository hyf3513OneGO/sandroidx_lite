package middlewares

import (
	"github.com/gin-gonic/gin"
	"sandroidx.com/sandroidx_lite/services"
)

func NewRequestMetricsMiddleware(collector *services.RequestMetricsCollector) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Next()
		if collector != nil {
			collector.Record(c.Writer.Status())
		}
	}
}


