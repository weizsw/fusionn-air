package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/fusionn-air/internal/scheduler"
	"github.com/fusionn-air/internal/service/cleanup"
	"github.com/fusionn-air/internal/service/watcher"
)

type Handler struct {
	watcher   *watcher.Service
	cleanup   *cleanup.Service
	scheduler *scheduler.Scheduler
}

func New(watcherService *watcher.Service, cleanupService *cleanup.Service, sched *scheduler.Scheduler) *Handler {
	return &Handler{
		watcher:   watcherService,
		cleanup:   cleanupService,
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

		// Cleanup endpoints
		api.GET("/cleanup/stats", h.CleanupStats)
		api.GET("/cleanup/queue", h.CleanupQueue)
		api.POST("/cleanup/run", h.TriggerCleanup)

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
		"cleanup_enabled": h.cleanup != nil,
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
	if err != nil {
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

// CleanupStats returns cleanup statistics
func (h *Handler) CleanupStats(c *gin.Context) {
	if h.cleanup == nil {
		c.JSON(http.StatusOK, gin.H{
			"enabled": false,
			"message": "cleanup is disabled",
		})
		return
	}

	stats := h.cleanup.GetStats()
	c.JSON(http.StatusOK, gin.H{
		"enabled":  true,
		"last_run": h.cleanup.GetLastRun(),
		"stats":    stats,
	})
}

// CleanupQueue returns the current cleanup queue
func (h *Handler) CleanupQueue(c *gin.Context) {
	if h.cleanup == nil {
		c.JSON(http.StatusOK, gin.H{
			"enabled": false,
			"queue":   []interface{}{},
		})
		return
	}

	queue := h.cleanup.GetQueue()
	c.JSON(http.StatusOK, gin.H{
		"enabled": true,
		"queue":   queue,
	})
}

// TriggerCleanup manually triggers cleanup processing
func (h *Handler) TriggerCleanup(c *gin.Context) {
	if h.cleanup == nil {
		c.JSON(http.StatusOK, gin.H{
			"enabled": false,
			"message": "cleanup is disabled",
		})
		return
	}

	results, err := h.cleanup.ProcessCleanup(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "cleanup processing complete",
		"results": results,
	})
}
