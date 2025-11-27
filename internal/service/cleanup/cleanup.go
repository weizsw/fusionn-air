package cleanup

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/fusionn-air/internal/client/sonarr"
	"github.com/fusionn-air/internal/client/trakt"
	"github.com/fusionn-air/internal/config"
	"github.com/fusionn-air/pkg/logger"
)

// Service handles cleanup of fully watched series
type Service struct {
	sonarr *sonarr.Client
	trakt  *trakt.Client
	cfg    config.CleanupConfig
	dryRun bool
	queue  *Queue

	mu          sync.RWMutex
	lastRun     time.Time
	lastResults *ProcessingResult
}

// ProcessingResult holds the results of a cleanup run
type ProcessingResult struct {
	Scanned        int            `json:"scanned"`
	MarkedForQueue int            `json:"marked_for_queue"`
	Removed        int            `json:"removed"`
	Skipped        int            `json:"skipped"`
	Errors         int            `json:"errors"`
	Details        []SeriesResult `json:"details"`
}

// SeriesResult holds the result for a single series
type SeriesResult struct {
	Title      string `json:"title"`
	SonarrID   int    `json:"sonarr_id"`
	Action     string `json:"action"` // "queued", "removed", "skipped", "error"
	Reason     string `json:"reason"`
	DaysUntil  int    `json:"days_until,omitempty"` // Days until removal (if queued)
	SizeOnDisk string `json:"size_on_disk,omitempty"`
}

func NewService(sonarrClient *sonarr.Client, traktClient *trakt.Client, cfg config.CleanupConfig, dryRun bool) *Service {
	return &Service{
		sonarr: sonarrClient,
		trakt:  traktClient,
		cfg:    cfg,
		dryRun: dryRun,
		queue:  NewQueue(),
	}
}

// ProcessCleanup runs the cleanup logic
func (s *Service) ProcessCleanup(ctx context.Context) (*ProcessingResult, error) {
	if !s.cfg.Enabled {
		logger.Debug("Cleanup is disabled, skipping")
		return nil, nil
	}

	startTime := time.Now()

	logger.Info("")
	logger.Info("┌──────────────────────────────────────────────────────────────┐")
	logger.Info("│               CLEANUP PROCESSING STARTED                     │")
	logger.Info("└──────────────────────────────────────────────────────────────┘")

	if s.dryRun {
		logger.Warn("⚠️  DRY RUN MODE - No actual deletions will be made")
	}

	result := &ProcessingResult{}

	// Step 1: Get all series from Sonarr
	logger.Info("📺 Fetching series from Sonarr...")
	series, err := s.sonarr.GetAllSeries(ctx)
	if err != nil {
		logger.Errorf("❌ Failed to get series from Sonarr: %v", err)
		return nil, fmt.Errorf("getting series: %w", err)
	}

	result.Scanned = len(series)
	logger.Infof("📺 Found %d series in Sonarr", len(series))

	// Step 2: Get watched shows from Trakt
	logger.Info("👁️  Fetching watch history from Trakt...")
	watchedShows, err := s.trakt.GetWatchedShows(ctx)
	if err != nil {
		logger.Errorf("❌ Failed to get watched shows: %v", err)
		return nil, fmt.Errorf("getting watched shows: %w", err)
	}

	// Build a map for quick lookup by TVDB ID
	watchedByTvdb := make(map[int]*trakt.WatchedShow)
	for i := range watchedShows {
		if watchedShows[i].Show.IDs.TVDB > 0 {
			watchedByTvdb[watchedShows[i].Show.IDs.TVDB] = &watchedShows[i]
		}
	}

	// Step 3: Process each series
	for _, ser := range series {
		serResult := s.processSeries(ctx, &ser, watchedByTvdb)
		result.Details = append(result.Details, serResult)

		switch serResult.Action {
		case "queued":
			result.MarkedForQueue++
		case "removed":
			result.Removed++
		case "skipped":
			result.Skipped++
		case "error":
			result.Errors++
		}
	}

	// Step 4: Process removal queue
	s.processRemovalQueue(ctx, result)

	// Store results
	s.mu.Lock()
	s.lastRun = time.Now()
	s.lastResults = result
	s.mu.Unlock()

	// Print summary
	s.printSummary(result, startTime)

	return result, nil
}

func (s *Service) processSeries(ctx context.Context, ser *sonarr.Series, watchedByTvdb map[int]*trakt.WatchedShow) SeriesResult {
	result := SeriesResult{
		Title:      ser.Title,
		SonarrID:   ser.ID,
		SizeOnDisk: sonarr.FormatSize(ser.Statistics.SizeOnDisk),
	}

	// Check exclusions
	if s.isExcluded(ser.Title) {
		result.Action = "skipped"
		result.Reason = "in exclusion list"
		return result
	}

	// Check if series has ended
	if !sonarr.IsSeriesEnded(ser) {
		result.Action = "skipped"
		result.Reason = fmt.Sprintf("still %s", ser.Status)
		return result
	}

	// Check if series has any files
	if ser.Statistics.EpisodeFileCount == 0 {
		result.Action = "skipped"
		result.Reason = "no files on disk"
		return result
	}

	// Find in Trakt watched shows
	watched, found := watchedByTvdb[ser.TvdbID]
	if !found {
		result.Action = "skipped"
		result.Reason = "not in Trakt history"
		return result
	}

	// Get detailed progress from Trakt
	progress, err := s.trakt.GetShowProgress(ctx, watched.Show.IDs.Trakt)
	if err != nil {
		result.Action = "error"
		result.Reason = fmt.Sprintf("failed to get progress: %v", err)
		return result
	}

	// Check if fully watched
	if progress.Completed < progress.Aired {
		pct := float64(progress.Completed) / float64(progress.Aired) * 100
		result.Action = "skipped"
		result.Reason = fmt.Sprintf("not fully watched (%d/%d = %.0f%%)", progress.Completed, progress.Aired, pct)
		return result
	}

	// Series is fully watched and ended!
	// Check if already in queue
	if s.queue.IsQueued(ser.ID) {
		queueItem := s.queue.Get(ser.ID)
		daysInQueue := int(time.Since(queueItem.MarkedAt).Hours() / 24)
		daysUntil := s.cfg.DelayDays - daysInQueue
		if daysUntil < 0 {
			daysUntil = 0
		}
		result.Action = "queued"
		result.Reason = fmt.Sprintf("in queue for %d days", daysInQueue)
		result.DaysUntil = daysUntil
		return result
	}

	// Add to queue
	s.queue.Add(&QueueItem{
		SonarrID:   ser.ID,
		TvdbID:     ser.TvdbID,
		Title:      ser.Title,
		MarkedAt:   time.Now(),
		Reason:     "fully watched",
		SizeOnDisk: ser.Statistics.SizeOnDisk,
	})

	result.Action = "queued"
	result.Reason = "fully watched - added to removal queue"
	result.DaysUntil = s.cfg.DelayDays
	return result
}

func (s *Service) processRemovalQueue(ctx context.Context, result *ProcessingResult) {
	ready := s.queue.GetReadyForRemoval(s.cfg.DelayDays)
	if len(ready) == 0 {
		return
	}

	logger.Infof("🗑️  %d series ready for removal", len(ready))

	for _, item := range ready {
		// Verify series still exists in Sonarr
		ser, err := s.sonarr.GetSeries(ctx, item.SonarrID)
		if err != nil {
			logger.Errorf("❌ Error checking series %s: %v", item.Title, err)
			continue
		}

		if ser == nil {
			// Already removed from Sonarr
			logger.Infof("ℹ️  %s already removed from Sonarr, removing from queue", item.Title)
			s.queue.Remove(item.SonarrID)
			continue
		}

		// Delete from Sonarr
		if s.dryRun {
			logger.Warnf("🗑️  [DRY RUN] Would delete: %s (%s)", item.Title, sonarr.FormatSize(item.SizeOnDisk))
		} else {
			if err := s.sonarr.DeleteSeries(ctx, item.SonarrID, true); err != nil {
				logger.Errorf("❌ Failed to delete %s: %v", item.Title, err)
				// Update result
				for i := range result.Details {
					if result.Details[i].SonarrID == item.SonarrID {
						result.Details[i].Action = "error"
						result.Details[i].Reason = fmt.Sprintf("delete failed: %v", err)
						result.Errors++
						result.MarkedForQueue--
					}
				}
				continue
			}
			logger.Infof("✓  Deleted: %s (%s freed)", item.Title, sonarr.FormatSize(item.SizeOnDisk))
		}

		// Remove from queue
		s.queue.Remove(item.SonarrID)

		// Update result
		for i := range result.Details {
			if result.Details[i].SonarrID == item.SonarrID {
				if s.dryRun {
					result.Details[i].Action = "dry_run_remove"
					result.Details[i].Reason = "would be deleted"
				} else {
					result.Details[i].Action = "removed"
					result.Details[i].Reason = "deleted from Sonarr"
				}
			}
		}
		result.Removed++
	}
}

func (s *Service) isExcluded(title string) bool {
	titleLower := strings.ToLower(title)
	for _, exc := range s.cfg.Exclusions {
		if strings.ToLower(exc) == titleLower {
			return true
		}
	}
	return false
}

func (s *Service) printSummary(result *ProcessingResult, startTime time.Time) {
	var toRemove []string
	var queued []string
	var skipped []string
	var errors []string

	for _, r := range result.Details {
		info := fmt.Sprintf("   • %-35s  ← %s", r.Title, r.Reason)
		switch r.Action {
		case "removed", "dry_run_remove":
			if r.SizeOnDisk != "" {
				info = fmt.Sprintf("   ✓ %-35s  ← %s (%s)", r.Title, r.Reason, r.SizeOnDisk)
			}
			toRemove = append(toRemove, info)
		case "queued":
			if r.DaysUntil > 0 {
				info = fmt.Sprintf("   ⏳ %-35s  ← %s (removes in %d days)", r.Title, r.Reason, r.DaysUntil)
			} else {
				info = fmt.Sprintf("   ⏳ %-35s  ← %s", r.Title, r.Reason)
			}
			queued = append(queued, info)
		case "skipped":
			skipped = append(skipped, info)
		case "error":
			errors = append(errors, fmt.Sprintf("   ✗ %-35s  ← %s", r.Title, r.Reason))
		}
	}

	logger.Info("")
	logger.Info("┌──────────────────────────────────────────────────────────────┐")
	logger.Info("│                    CLEANUP RESULTS                           │")
	logger.Info("└──────────────────────────────────────────────────────────────┘")

	if len(toRemove) > 0 {
		logger.Info("")
		if s.dryRun {
			logger.Warnf("🗑️  WOULD REMOVE (%d):", len(toRemove))
		} else {
			logger.Infof("🗑️  REMOVED (%d):", len(toRemove))
		}
		for _, line := range toRemove {
			logger.Info(line)
		}
	}

	if len(queued) > 0 {
		logger.Info("")
		logger.Infof("⏳ IN REMOVAL QUEUE (%d):", len(queued))
		for _, line := range queued {
			logger.Info(line)
		}
	}

	if len(skipped) > 0 {
		logger.Info("")
		logger.Infof("⏭️  SKIPPED (%d):", len(skipped))
		for _, line := range skipped {
			logger.Info(line)
		}
	}

	if len(errors) > 0 {
		logger.Info("")
		logger.Errorf("❌ ERRORS (%d):", len(errors))
		for _, line := range errors {
			logger.Error(line)
		}
	}

	logger.Info("")
	logger.Info("────────────────────────────────────────────────────────────────")
	logger.Infof("⏱️  Completed in %v", time.Since(startTime).Round(time.Millisecond))
	logger.Info("")
}

// GetStats returns the current cleanup stats
func (s *Service) GetStats() *ProcessingResult {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.lastResults
}

// GetQueue returns the current cleanup queue
func (s *Service) GetQueue() []*QueueItem {
	return s.queue.GetAll()
}

// GetLastRun returns the last run time
func (s *Service) GetLastRun() time.Time {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.lastRun
}

