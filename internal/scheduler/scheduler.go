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
	watcher *watcher.Service
	mu      sync.Mutex
	cron    *cron.Cron
	ctx     context.Context
	cancel  context.CancelFunc
	done    <-chan struct{}
}

func New(watcherService *watcher.Service) *Scheduler {
	return &Scheduler{watcher: watcherService}
}

// Start begins the scheduled job.
func (s *Scheduler) Start(cronExpr string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cron != nil {
		return nil
	}

	schedule := cron.New()
	if _, err := schedule.AddFunc(cronExpr, s.runWatcher); err != nil {
		return err
	}
	s.ctx, s.cancel = context.WithCancel(context.Background())
	s.cron = schedule
	s.cron.Start()
	log.Info(s.ctx, "scheduler_started", "Scheduler started", zap.String("cron", cronExpr))
	return nil
}

// Stop cancels and waits for the active watcher run.
func (s *Scheduler) Stop() {
	s.mu.Lock()
	if s.cron == nil {
		s.mu.Unlock()
		return
	}
	s.cancel()
	stopped := s.cron.Stop()
	done := s.done
	s.cron = nil
	s.done = nil
	s.mu.Unlock()

	<-stopped.Done()
	if done != nil {
		<-done
	}
}

// RunNow starts a run owned by the scheduler. It carries the request ID into the run.
func (s *Scheduler) RunNow(requestCtx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cron == nil || s.watcher == nil {
		return errors.New("watcher unavailable")
	}
	ctx := logger.WithRequestID(s.ctx, logger.RequestID(requestCtx))
	done, err := s.watcher.StartCalendar(ctx)
	if err != nil {
		return err
	}
	s.done = done
	return nil
}

func (s *Scheduler) runWatcher() {
	if s.watcher == nil {
		return
	}
	ctx := context.Background()
	if err := s.RunNow(ctx); errors.Is(err, watcher.ErrAlreadyRunning) {
		log.Info(ctx, "watcher_skipped", "Watcher run already active")
	}
}

// IsRunning reports whether the scheduler is active.
func (s *Scheduler) IsRunning() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cron != nil
}
