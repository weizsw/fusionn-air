package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/fusionn-air/internal/client/overseerr"
	"github.com/fusionn-air/internal/client/trakt"
	"github.com/fusionn-air/internal/config"
	"github.com/fusionn-air/internal/handler"
	"github.com/fusionn-air/internal/scheduler"
	"github.com/fusionn-air/internal/service/watcher"
	"github.com/fusionn-air/pkg/logger"
	"github.com/gin-gonic/gin"
)

func main() {
	// Initialize logger
	isDev := os.Getenv("ENV") != "production"
	logger.Init(isDev)
	defer logger.Sync()

	logger.Info("========== fusionn-air starting ==========")

	// Load configuration
	configPath := os.Getenv("CONFIG_PATH")
	if configPath == "" {
		configPath = "config/config.yaml"
	}

	logger.Infof("loading config from: %s", configPath)
	cfg, err := config.Load(configPath)
	if err != nil {
		logger.Fatalf("failed to load config: %v", err)
	}

	logger.Info("configuration loaded successfully")
	logger.Debugf("config: server.port=%d scheduler.cron=%s scheduler.calendar_days=%d dry_run=%v",
		cfg.Server.Port, cfg.Scheduler.Cron, cfg.Scheduler.CalendarDays, cfg.Scheduler.DryRun)

	if cfg.Scheduler.DryRun {
		logger.Warn("*** DRY RUN MODE ENABLED - no actual requests will be made to Overseerr ***")
	}

	// Initialize clients
	logger.Debug("initializing trakt client...")
	traktClient := trakt.NewClient(cfg.Trakt)

	// Authenticate with Trakt (will prompt for device auth if needed)
	logger.Info("authenticating with Trakt...")
	ctx := context.Background()
	if err := traktClient.Initialize(ctx); err != nil {
		logger.Fatalf("failed to authenticate with Trakt: %v", err)
	}

	logger.Debug("initializing overseerr client...")
	overseerrClient := overseerr.NewClient(cfg.Overseerr)

	// Initialize services
	logger.Debug("initializing watcher service...")
	watcherService := watcher.NewService(traktClient, overseerrClient, cfg.Scheduler)

	// Initialize scheduler
	logger.Debug("initializing scheduler...")
	sched := scheduler.New(watcherService)
	if err := sched.Start(cfg.Scheduler.Cron); err != nil {
		logger.Fatalf("failed to start scheduler: %v", err)
	}

	// Run immediately on startup if configured
	if cfg.Scheduler.RunOnStart {
		logger.Info("run_on_start enabled, triggering initial run...")
		sched.RunNow()
	}

	// Initialize HTTP server
	if !isDev {
		gin.SetMode(gin.ReleaseMode)
	}

	router := gin.New()
	router.Use(gin.Recovery())
	router.Use(requestLogger())

	// Register routes
	h := handler.New(watcherService, sched)
	h.RegisterRoutes(router)

	// Start server
	srv := &http.Server{
		Addr:         fmt.Sprintf(":%d", cfg.Server.Port),
		Handler:      router,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Graceful shutdown
	go func() {
		logger.Infof("HTTP server starting on :%d", cfg.Server.Port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Fatalf("server error: %v", err)
		}
	}()

	logger.Info("========== fusionn-air ready ==========")

	// Wait for interrupt signal
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	sig := <-quit

	logger.Infof("received signal: %v, shutting down...", sig)

	// Stop scheduler
	logger.Debug("stopping scheduler...")
	sched.Stop()

	// Graceful shutdown with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	logger.Debug("shutting down HTTP server...")
	if err := srv.Shutdown(ctx); err != nil {
		logger.Errorf("server shutdown error: %v", err)
	}

	logger.Info("========== fusionn-air stopped ==========")
}

// requestLogger returns a gin middleware for logging HTTP requests
func requestLogger() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		path := c.Request.URL.Path
		query := c.Request.URL.RawQuery
		method := c.Request.Method
		clientIP := c.ClientIP()
		userAgent := c.Request.UserAgent()

		// Log incoming request
		if query != "" {
			logger.Debugf("[http] --> %s %s?%s from %s", method, path, query, clientIP)
		} else {
			logger.Debugf("[http] --> %s %s from %s", method, path, clientIP)
		}

		// Process request
		c.Next()

		// Log response
		latency := time.Since(start)
		status := c.Writer.Status()
		bodySize := c.Writer.Size()
		errors := c.Errors.ByType(gin.ErrorTypePrivate).String()

		fullPath := path
		if query != "" {
			fullPath = path + "?" + query
		}

		// Choose log level based on status code
		switch {
		case status >= 500:
			logger.Errorf("[http] <-- %s %s %d %v size=%d ip=%s ua=%s errors=%s",
				method, fullPath, status, latency, bodySize, clientIP, userAgent, errors)
		case status >= 400:
			logger.Warnf("[http] <-- %s %s %d %v size=%d ip=%s",
				method, fullPath, status, latency, bodySize, clientIP)
		default:
			logger.Infof("[http] <-- %s %s %d %v size=%d",
				method, fullPath, status, latency, bodySize)
		}
	}
}
