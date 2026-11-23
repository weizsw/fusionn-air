package scheduler

import (
	"context"
	"errors"
	"sync"

	"github.com/robfig/cron/v3"
	"go.uber.org/zap"

	"github.com/fusionn-air/internal/service/watcher"
	"github.com/fusionn-air/pkg/logger"
)

var log = logger.Component("scheduler")

type Scheduler struct {
	cron    *cron.Cron
	watcher *watcher.Service
	mu      sync.Mutex
	running bool
}

func New(watcherService *watcher.Service) *Scheduler {
	return &Scheduler{
		cron:    cron.New(cron.WithSeconds()),
		watcher: watcherService,
	}
}

// Start begins the scheduled job
func (s *Scheduler) Start(cronExpr string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.running {
		return nil
	}

	// Convert standard cron (5 fields) to cron with seconds (6 fields)
	cronWithSeconds := "0 " + cronExpr

	_, err := s.cron.AddFunc(cronWithSeconds, s.runWatcher)
	if err != nil {
		return err
	}

	s.cron.Start()
	s.running = true

	log.Info(context.Background(), "scheduler_started", "Scheduler started", zap.String("cron", cronExpr))

	return nil
}

// Stop gracefully stops the scheduler
func (s *Scheduler) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.running {
		return
	}

	ctx := s.cron.Stop()
	<-ctx.Done()
	s.running = false
}

// RunNow triggers the watcher immediately.
func (s *Scheduler) RunNow() {
	go s.runWatcher()
}

func (s *Scheduler) runWatcher() {
	if s.watcher == nil {
		return
	}
	ctx := context.Background()
	_, err := s.watcher.ProcessCalendar(ctx)
	if errors.Is(err, watcher.ErrAlreadyRunning) {
		log.Info(ctx, "watcher_skipped", "Watcher run already active")
	} else if err != nil {
		log.Error(ctx, "watcher_failed", "Scheduled watcher run failed", zap.Error(err))
	}
}

// IsRunning returns whether the scheduler is active
func (s *Scheduler) IsRunning() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.running
}
