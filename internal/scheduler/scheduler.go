package scheduler

import (
	"context"
	"sync"

	"github.com/fusionn-air/internal/service/cleanup"
	"github.com/fusionn-air/internal/service/watcher"
	"github.com/fusionn-air/pkg/logger"
	"github.com/robfig/cron/v3"
)

type Scheduler struct {
	cron    *cron.Cron
	watcher *watcher.Service
	cleanup *cleanup.Service
	mu      sync.Mutex
	running bool
}

func New(watcherService *watcher.Service, cleanupService *cleanup.Service) *Scheduler {
	return &Scheduler{
		cron:    cron.New(cron.WithSeconds()),
		watcher: watcherService,
		cleanup: cleanupService,
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

	_, err := s.cron.AddFunc(cronWithSeconds, func() {
		s.runJob()
	})
	if err != nil {
		return err
	}

	s.cron.Start()
	s.running = true

	logger.Infof("⏰ Scheduler: %s", cronExpr)

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

// RunNow triggers both watcher and cleanup immediately
func (s *Scheduler) RunNow() {
	go s.runJob()
}

// RunWatcherNow triggers only the watcher job
func (s *Scheduler) RunWatcherNow() {
	go s.runWatcher()
}

// RunCleanupNow triggers only the cleanup job
func (s *Scheduler) RunCleanupNow() {
	go s.runCleanup()
}

func (s *Scheduler) runJob() {
	s.runWatcher()
	s.runCleanup()
}

func (s *Scheduler) runWatcher() {
	if s.watcher == nil {
		return
	}
	ctx := context.Background()
	_, err := s.watcher.ProcessCalendar(ctx)
	if err != nil {
		logger.Errorf("❌ Watcher job failed: %v", err)
	}
}

func (s *Scheduler) runCleanup() {
	if s.cleanup == nil {
		return
	}
	ctx := context.Background()
	_, err := s.cleanup.ProcessCleanup(ctx)
	if err != nil {
		logger.Errorf("❌ Cleanup job failed: %v", err)
	}
}

// IsRunning returns whether the scheduler is active
func (s *Scheduler) IsRunning() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.running
}
