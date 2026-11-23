package main

import (
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap/zapcore"

	"github.com/fusionn-air/pkg/logger"
)

func TestHTTPLogging(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Setenv("LOG_LEVEL", "INFO")
	var logs bytes.Buffer
	logger.Configure(logger.Config{Output: zapcore.AddSync(&logs)})
	t.Cleanup(func() { logger.Configure(logger.Config{}) })

	router := gin.New()
	router.Use(requestLogger(), recoveryLogger())
	router.GET("/api/v1/health", func(c *gin.Context) { c.Status(http.StatusOK) })
	router.GET("/failure", func(c *gin.Context) {
		_ = c.Error(errors.New("calendar unavailable"))
		c.Status(http.StatusInternalServerError)
	})
	router.POST("/watcher/:id", func(c *gin.Context) {
		if got := logger.RequestID(c.Request.Context()); got != "test-request" {
			t.Errorf("request context ID = %q", got)
		}
		panic("untrusted panic text")
	})

	health := httptest.NewRecorder()
	router.ServeHTTP(health, httptest.NewRequest(http.MethodGet, "/api/v1/health", nil))
	if id := health.Header().Get("X-Request-ID"); health.Code != http.StatusOK || !logger.ValidRequestID(id) {
		t.Fatalf("health status = %d, request ID = %q", health.Code, id)
	}
	if logs.Len() != 0 {
		t.Fatalf("health check logged at INFO: %s", &logs)
	}

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/watcher/untrusted-path-value", nil)
	request.Header.Set("X-Request-ID", "test-request")
	router.ServeHTTP(response, request)
	if id := response.Header().Get("X-Request-ID"); response.Code != http.StatusInternalServerError || id != "test-request" {
		t.Fatalf("panic response status = %d, request ID = %q", response.Code, id)
	}

	lines := strings.Split(strings.TrimSpace(logs.String()), "\n")
	if len(lines) != 2 || !strings.Contains(lines[0], "ERROR component=http event=request_panicked request_id=test-request") ||
		!strings.Contains(lines[1], "WARN component=http event=request_completed request_id=test-request") ||
		!strings.Contains(lines[1], `route="/watcher/:id" status=500`) {
		t.Fatalf("unexpected HTTP logs: %s", &logs)
	}
	if strings.Contains(logs.String(), "untrusted panic text") || strings.Contains(logs.String(), "untrusted-path-value") {
		t.Fatalf("HTTP log contains raw request or panic data: %s", &logs)
	}

	logs.Reset()
	router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/failure", nil))
	if got := logs.String(); strings.Count(got, "\n") != 1 || !strings.Contains(got, `error="calendar unavailable"`) {
		t.Fatalf("expected one request error with its cause: %s", got)
	}
}
