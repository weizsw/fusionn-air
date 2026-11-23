package handler

import (
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/fusionn-air/internal/scheduler"
	"github.com/fusionn-air/internal/service/watcher"
)

type Handler struct {
	watcher    *watcher.Service
	scheduler  *scheduler.Scheduler
	triggerKey string
}

func New(watcherService *watcher.Service, sched *scheduler.Scheduler, triggerKey string) *Handler {
	return &Handler{
		watcher:    watcherService,
		scheduler:  sched,
		triggerKey: triggerKey,
	}
}

// RegisterRoutes sets up the HTTP routes
func (h *Handler) RegisterRoutes(r *gin.Engine) {
	api := r.Group("/api/v1")
	{
		// Health
		api.GET("/health", h.Health)

		// Watcher endpoints
		api.GET("/watcher/stats", h.WatcherStats)
		api.POST("/watcher/run", h.TriggerWatcher)

		// Legacy endpoints (for backwards compatibility)
		api.GET("/stats", h.WatcherStats)
		api.POST("/process", h.TriggerWatcher)
	}
}

// Health returns service health status
func (h *Handler) Health(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"status":          "ok",
		"scheduler":       h.scheduler.IsRunning(),
		"watcher_enabled": h.watcher != nil,
	})
}

// WatcherStats returns watcher statistics
func (h *Handler) WatcherStats(c *gin.Context) {
	if h.watcher == nil {
		c.JSON(http.StatusOK, gin.H{
			"enabled": false,
			"message": "watcher is disabled",
		})
		return
	}
	stats := h.watcher.GetStats()
	c.JSON(http.StatusOK, gin.H{
		"enabled": true,
		"stats":   stats,
	})
}

// TriggerWatcher manually triggers calendar processing
func (h *Handler) TriggerWatcher(c *gin.Context) {
	if h.watcher == nil {
		c.JSON(http.StatusOK, gin.H{
			"enabled": false,
			"message": "watcher is disabled",
		})
		return
	}

	if h.triggerKey == "" {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Manual runs require server.trigger_api_key"})
		return
	}
	expected := sha256.Sum256([]byte(h.triggerKey))
	presented := sha256.Sum256([]byte(c.GetHeader("X-API-Key")))
	if subtle.ConstantTimeCompare(expected[:], presented[:]) != 1 {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid or missing API key"})
		return
	}

	err := h.scheduler.RunNow(c.Request.Context())
	if errors.Is(err, watcher.ErrAlreadyRunning) {
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
		return
	}
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Watcher unavailable"})
		return
	}

	c.Header("Location", "/api/v1/watcher/stats")
	c.JSON(http.StatusAccepted, gin.H{"message": "Watcher run accepted"})
}
