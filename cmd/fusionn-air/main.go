package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/fusionn-air/internal/client/apprise"
	"github.com/fusionn-air/internal/client/overseerr"
	"github.com/fusionn-air/internal/client/scrob"
	"github.com/fusionn-air/internal/client/trakt"
	"github.com/fusionn-air/internal/config"
	"github.com/fusionn-air/internal/handler"
	"github.com/fusionn-air/internal/scheduler"
	"github.com/fusionn-air/internal/service/watcher"
	"github.com/fusionn-air/internal/version"
	"github.com/fusionn-air/pkg/logger"
)

func main() {
	os.Exit(run())
}

func run() int {
	logger.Configure(logger.Config{})
	defer logger.Sync()
	log := logger.Component("main")
	ctx := context.Background()

	configPath := os.Getenv("CONFIG_PATH")
	if configPath == "" {
		configPath = "config/config.yaml"
	}
	cfgMgr, err := config.NewManager(configPath)
	if err != nil {
		log.Error(ctx, "service_failed", "Could not load configuration", zap.String("stage", "config"), zap.String("path", configPath), zap.Error(err))
		return 1
	}
	defer cfgMgr.Stop()
	cfg := cfgMgr.Get()
	version.PrintBanner(nil, logger.Log.Level().CapitalString())

	overseerrClient := overseerr.NewClient(cfg.Overseerr)
	var appriseClient *apprise.Client
	if cfg.Apprise.Enabled {
		appriseClient = apprise.NewClient(cfg.Apprise)
	}

	var watcherService *watcher.Service
	if cfg.Watcher.Enabled {
		var source watcher.Source
		switch cfg.Watcher.Source {
		case "scrob":
			client, err := scrob.NewClient(cfg.Scrob)
			if err != nil {
				log.Error(ctx, "service_failed", "Invalid Scrob configuration", zap.String("stage", "scrob"), zap.Error(err))
				return 1
			}
			source = watcher.NewScrobSource(client)
		case "trakt", "":
			client := trakt.NewClient(cfg.Trakt)
			if err := client.Initialize(ctx); err != nil {
				log.Error(ctx, "service_failed", "Trakt authorization failed", zap.String("stage", "trakt"), zap.Error(err))
				return 1
			}
			source = watcher.NewTraktSource(client)
		default:
			log.Error(ctx, "service_failed", "Expected scrob or trakt in watcher.source", zap.String("stage", "config"), zap.String("source", cfg.Watcher.Source))
			return 1
		}
		watcherService = watcher.NewService(source, overseerrClient, appriseClient, cfgMgr)
	}

	sched := scheduler.New(watcherService)
	if err := sched.Start(cfg.Scheduler.Cron); err != nil {
		log.Error(ctx, "service_failed", "Could not start scheduler", zap.String("stage", "scheduler"), zap.Error(err))
		return 1
	}
	defer sched.Stop()

	if os.Getenv("ENV") == "production" {
		gin.SetMode(gin.ReleaseMode)
	}
	gin.DefaultWriter = io.Discard
	router := gin.New()
	router.Use(requestLogger(), recoveryLogger())
	h := handler.New(watcherService, sched, cfg.Server.TriggerAPIKey)
	h.RegisterRoutes(router)

	srv := &http.Server{
		Addr:         fmt.Sprintf(":%d", cfg.Server.Port),
		Handler:      router,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	quit, stop := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	serveErr := make(chan error, 1)
	go func() { serveErr <- srv.ListenAndServe() }()

	log.Info(ctx, "service_started", "API server starting",
		zap.String("version", version.Version), zap.Int("port", cfg.Server.Port),
		zap.Bool("watcher_enabled", cfg.Watcher.Enabled), zap.String("source", cfg.Watcher.Source),
		zap.Bool("dry_run", cfg.Scheduler.DryRun), zap.Bool("notifications_enabled", cfg.Apprise.Enabled))

	if cfg.Scheduler.RunOnStart {
		if err := sched.RunNow(ctx); err != nil {
			log.Warn(ctx, "watcher_start_skipped", "Could not run watcher at startup", zap.Error(err))
		}
	}

	select {
	case err := <-serveErr:
		log.Error(ctx, "service_failed", "HTTP server failed", zap.String("stage", "http"), zap.Error(err))
		return 1
	case <-quit.Done():
	}

	log.Info(ctx, "service_stopping", "Shutting down")
	sched.Stop()

	shutdownCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Error(ctx, "service_failed", "HTTP server shutdown failed", zap.String("stage", "shutdown"), zap.Error(err))
		return 1
	}
	log.Info(ctx, "service_stopped", "Service stopped")
	return 0
}
