package handler

import (
	"net/http"

	"github.com/fusionn-air/internal/scheduler"
	"github.com/fusionn-air/internal/service/watcher"
	"github.com/gin-gonic/gin"
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
		api.GET("/health", h.Health)
		api.GET("/stats", h.Stats)
		api.POST("/process", h.TriggerProcess)
	}
}

// Health returns service health status
func (h *Handler) Health(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"status":    "ok",
		"scheduler": h.scheduler.IsRunning(),
	})
}

// Stats returns processing statistics
func (h *Handler) Stats(c *gin.Context) {
	stats := h.watcher.GetStats()
	c.JSON(http.StatusOK, stats)
}

// TriggerProcess manually triggers calendar processing
func (h *Handler) TriggerProcess(c *gin.Context) {
	results, err := h.watcher.ProcessCalendar(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "processing complete",
		"results": results,
	})
}
