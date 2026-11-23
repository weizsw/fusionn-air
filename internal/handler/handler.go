package handler

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/fusionn-air/internal/scheduler"
	"github.com/fusionn-air/internal/service/watcher"
)

type Handler struct {
	watcher   *watcher.Service
	scheduler *scheduler.Scheduler
}

func New(watcherService *watcher.Service, sched *scheduler.Scheduler) *Handler {
	return &Handler{
		watcher:   watcherService,
		scheduler: sched,
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

	results, err := h.watcher.ProcessCalendar(c.Request.Context())
	if errors.Is(err, watcher.ErrAlreadyRunning) {
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
		return
	}
	if err != nil {
		_ = c.Error(err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "watcher processing complete",
		"results": results,
	})
}
