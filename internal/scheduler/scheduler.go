package scheduler

import (
	"context"
	"sync"
	"time"

	"github.com/fusionn-air/internal/service/watcher"
	"github.com/fusionn-air/pkg/logger"
	"github.com/robfig/cron/v3"
)

type Scheduler struct {
	cron     *cron.Cron
	watcher  *watcher.Service
	cronExpr string
	mu       sync.Mutex
	running  bool
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
		logger.Warn("[scheduler] already running, skipping start")
		return nil
	}

	// Convert standard cron (5 fields) to cron with seconds (6 fields)
	// by prepending "0 " for 0 seconds
	cronWithSeconds := "0 " + cronExpr

	entryID, err := s.cron.AddFunc(cronWithSeconds, func() {
		s.runJob()
	})
	if err != nil {
		logger.Errorf("[scheduler] failed to add cron job: %v", err)
		return err
	}

	s.cron.Start()
	s.running = true
	s.cronExpr = cronExpr

	// Calculate next run time
	entry := s.cron.Entry(entryID)
	nextRun := entry.Next

	logger.Infof("[scheduler] started with cron=%s next_run=%s", cronExpr, nextRun.Format(time.RFC3339))
	return nil
}

// Stop gracefully stops the scheduler
func (s *Scheduler) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.running {
		return
	}

	logger.Info("[scheduler] stopping...")
	ctx := s.cron.Stop()
	<-ctx.Done()
	s.running = false

	logger.Info("[scheduler] stopped")
}

// RunNow triggers an immediate run
func (s *Scheduler) RunNow() {
	logger.Info("[scheduler] manual trigger requested")
	go s.runJob()
}

func (s *Scheduler) runJob() {
	startTime := time.Now()
	logger.Info("[scheduler] job starting...")

	ctx := context.Background()
	results, err := s.watcher.ProcessCalendar(ctx)
	if err != nil {
		logger.Errorf("[scheduler] job failed after %v: %v", time.Since(startTime), err)
		return
	}

	requested := 0
	dryRun := 0
	skipped := 0
	errors := 0
	for _, r := range results {
		switch r.Action {
		case "requested":
			requested++
		case "dry_run":
			dryRun++
		case "skipped", "already_requested":
			skipped++
		case "error":
			errors++
		}
	}

	if dryRun > 0 {
		logger.Warnf("[scheduler] job completed in %v: total=%d would_request=%d skipped=%d errors=%d (DRY RUN)",
			time.Since(startTime), len(results), dryRun, skipped, errors)
	} else {
		logger.Infof("[scheduler] job completed in %v: total=%d requested=%d skipped=%d errors=%d",
			time.Since(startTime), len(results), requested, skipped, errors)
	}
}

// IsRunning returns whether the scheduler is active
func (s *Scheduler) IsRunning() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.running
}
