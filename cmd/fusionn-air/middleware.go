package main

import (
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/fusionn-air/pkg/logger"
)

const panicReported = "panic_reported"

func requestLogger() gin.HandlerFunc {
	log := logger.Component("http")
	return func(c *gin.Context) {
		start := time.Now()
		requestID := c.GetHeader("X-Request-ID")
		if !logger.ValidRequestID(requestID) {
			requestID = logger.NewRequestID()
		}
		c.Header("X-Request-ID", requestID)
		ctx := logger.WithRequestID(c.Request.Context(), requestID)
		c.Request = c.Request.WithContext(ctx)

		c.Next()

		route := c.FullPath()
		if route == "" {
			route = "unmatched"
		}
		status := c.Writer.Status()
		fields := []zap.Field{
			zap.String("method", c.Request.Method), zap.String("route", route),
			zap.Int("status", status), logger.DurMs(time.Since(start)),
		}
		switch {
		case status >= http.StatusInternalServerError && c.GetBool(panicReported):
			log.Warn(ctx, "request_completed", "Request failed", fields...)
		case status >= http.StatusInternalServerError:
			if failure := c.Errors.Last(); failure != nil {
				fields = append(fields, zap.Error(failure.Err))
			}
			log.Error(ctx, "request_completed", "Request failed", fields...)
		case status >= http.StatusBadRequest:
			log.Warn(ctx, "request_completed", "Request rejected", fields...)
		case route == "/api/v1/health":
			log.Debug(ctx, "request_completed", "Health check completed", fields...)
		default:
			log.Info(ctx, "request_completed", "Request completed", fields...)
		}
	}
}

func recoveryLogger() gin.HandlerFunc {
	log := logger.Component("http")
	return gin.CustomRecoveryWithWriter(nil, func(c *gin.Context, recovered any) {
		log.Error(c.Request.Context(), "request_panicked", "Request panicked",
			zap.String("panic_type", fmt.Sprintf("%T", recovered)), logger.Preview(fmt.Sprint(recovered)), zap.Stack("stack"))
		c.Set(panicReported, true)
		c.AbortWithStatus(http.StatusInternalServerError)
	})
}
