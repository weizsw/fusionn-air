package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/fusionn-air/internal/client/overseerr"
	"github.com/fusionn-air/internal/client/sonarr"
	"github.com/fusionn-air/internal/client/trakt"
	"github.com/fusionn-air/internal/config"
	"github.com/fusionn-air/internal/handler"
	"github.com/fusionn-air/internal/scheduler"
	"github.com/fusionn-air/internal/service/cleanup"
	"github.com/fusionn-air/internal/service/watcher"
	"github.com/fusionn-air/internal/version"
	"github.com/fusionn-air/pkg/logger"
)

func main() {
	// Initialize logger
	isDev := os.Getenv("ENV") != "production"
	logger.Init(isDev)
	defer logger.Sync()

	version.PrintBanner(nil)

	// Load configuration
	configPath := os.Getenv("CONFIG_PATH")
	if configPath == "" {
		configPath = "config/config.yaml"
	}

	logger.Infof("📁 Loading config: %s", configPath)
	cfg, err := config.Load(configPath)
	if err != nil {
		logger.Fatalf("❌ Config error: %v", err)
	}

	if cfg.Scheduler.DryRun {
		logger.Warn("⚠️  DRY RUN MODE - No actual requests/deletions will be made")
	}

	// Initialize Trakt client
	logger.Info("🔗 Connecting to Trakt...")
	traktClient := trakt.NewClient(cfg.Trakt)

	ctx := context.Background()
	if err := traktClient.Initialize(ctx); err != nil {
		logger.Fatalf("❌ Trakt auth failed: %v", err)
	}
	logger.Info("✓  Trakt connected")

	// Initialize Overseerr client
	logger.Info("🔗 Connecting to Overseerr...")
	overseerrClient := overseerr.NewClient(cfg.Overseerr)
	logger.Info("✓  Overseerr configured")

	// Initialize Sonarr client (if cleanup enabled)
	var sonarrClient *sonarr.Client
	var cleanupService *cleanup.Service

	if cfg.Cleanup.Enabled {
		logger.Info("🔗 Connecting to Sonarr...")
		sonarrClient = sonarr.NewClient(cfg.Sonarr)
		logger.Info("✓  Sonarr configured")

		cleanupService = cleanup.NewService(sonarrClient, traktClient, cfg.Cleanup, cfg.Scheduler.DryRun)
		logger.Infof("🧹 Cleanup: enabled (delay=%d days)", cfg.Cleanup.DelayDays)
	} else {
		logger.Info("🧹 Cleanup: disabled")
	}

	// Initialize watcher service
	var watcherService *watcher.Service
	if cfg.Watcher.Enabled {
		watcherService = watcher.NewService(traktClient, overseerrClient, cfg.Watcher, cfg.Scheduler.DryRun)
		logger.Infof("👁️  Watcher: enabled (calendar_days=%d)", cfg.Watcher.CalendarDays)
	} else {
		logger.Info("👁️  Watcher: disabled")
	}

	// Initialize scheduler
	sched := scheduler.New(watcherService, cleanupService)
	if err := sched.Start(cfg.Scheduler.Cron); err != nil {
		logger.Fatalf("❌ Scheduler error: %v", err)
	}

	// Initialize HTTP server
	if !isDev {
		gin.SetMode(gin.ReleaseMode)
	}

	router := gin.New()
	router.Use(gin.Recovery())
	router.Use(requestLogger())

	h := handler.New(watcherService, cleanupService, sched)
	h.RegisterRoutes(router)

	srv := &http.Server{
		Addr:         fmt.Sprintf(":%d", cfg.Server.Port),
		Handler:      router,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Fatalf("❌ Server error: %v", err)
		}
	}()

	logger.Infof("🌐 API server: http://localhost:%d", cfg.Server.Port)
	logger.Info("")
	logger.Info("────────────────────────────────────────────────────────────────")
	logger.Info("✓  Ready! Waiting for scheduled runs...")
	logger.Info("────────────────────────────────────────────────────────────────")

	// Run immediately on startup if configured
	if cfg.Scheduler.RunOnStart {
		logger.Info("")
		logger.Info("🚀 Running initial jobs (run_on_start=true)...")
		sched.RunNow()
	}

	// Wait for interrupt signal
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Info("")
	logger.Info("🛑 Shutting down...")

	sched.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		logger.Errorf("❌ Shutdown error: %v", err)
	}

	logger.Info("👋 Goodbye!")
}

// requestLogger returns a gin middleware for logging HTTP requests
func requestLogger() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		path := c.Request.URL.Path

		c.Next()

		// Only log non-health endpoints or errors
		status := c.Writer.Status()
		if path != "/api/v1/health" || status >= 400 {
			latency := time.Since(start)
			logger.Debugf("HTTP %s %s → %d (%v)", c.Request.Method, path, status, latency)
		}
	}
}
