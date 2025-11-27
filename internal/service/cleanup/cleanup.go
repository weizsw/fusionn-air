package cleanup

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/fusionn-air/internal/client/apprise"
	"github.com/fusionn-air/internal/client/sonarr"
	"github.com/fusionn-air/internal/client/trakt"
	"github.com/fusionn-air/internal/config"
	"github.com/fusionn-air/pkg/logger"
)

// Service handles cleanup of fully watched series
type Service struct {
	sonarr  *sonarr.Client
	trakt   *trakt.Client
	apprise *apprise.Client
	cfg     config.CleanupConfig
	dryRun  bool
	queue   *Queue

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

func NewService(sonarrClient *sonarr.Client, traktClient *trakt.Client, appriseClient *apprise.Client, cfg config.CleanupConfig, dryRun bool) *Service {
	return &Service{
		sonarr:  sonarrClient,
		trakt:   traktClient,
		apprise: appriseClient,
		cfg:     cfg,
		dryRun:  dryRun,
		queue:   NewQueue(),
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

	// Send notification
	s.sendNotification(ctx, result)

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
		result.Reason = "in exclusion list - never remove"
		return result
	}

	// Check if series is monitored
	if !ser.Monitored {
		result.Action = "skipped"
		result.Reason = "not monitored in Sonarr"
		return result
	}

	// Check if series has any files
	if ser.Statistics.EpisodeFileCount == 0 {
		result.Action = "skipped"
		// Check if series hasn't aired yet
		if ser.Status == sonarr.StatusUpcoming || ser.Statistics.EpisodeCount == 0 {
			result.Reason = "not yet aired"
		} else {
			result.Reason = "no files on disk"
		}
		return result
	}

	// Find in Trakt watched shows
	watched, found := watchedByTvdb[ser.TvdbID]
	if !found {
		result.Action = "skipped"
		result.Reason = "no watch history in Trakt"
		return result
	}

	// Get detailed progress from Trakt
	progress, err := s.trakt.GetShowProgress(ctx, watched.Show.IDs.Trakt)
	if err != nil {
		result.Action = "error"
		result.Reason = fmt.Sprintf("failed to get Trakt progress: %v", err)
		return result
	}

	// Get season info for total episode counts
	seasons, _ := s.trakt.GetShowSeasons(ctx, watched.Show.IDs.Trakt)

	// Check if user has watched all episodes that are ON DISK in Sonarr
	// (not all aired episodes, just what's in Sonarr)
	watchedOnDisk, unwatchedSeasons := s.checkWatchedOnDisk(ser, progress)
	if !watchedOnDisk {
		result.Action = "skipped"
		result.Reason = buildWatchingReason(progress, seasons, unwatchedSeasons)
		return result
	}

	// All episodes on disk are watched, but check if more episodes are coming
	// Check both NextEpisode AND if any season has more episodes to air
	moreEpisodesComing, ongoingReason := checkMoreEpisodesComing(ser, progress, seasons)
	if moreEpisodesComing {
		// Remove from queue if it was previously queued (show is ongoing)
		if s.queue.IsQueued(ser.ID) {
			s.queue.Remove(ser.ID)
			logger.Debugf("Removed %s from queue - more episodes coming", ser.Title)
		}
		result.Action = "skipped"
		result.Reason = ongoingReason
		return result
	}

	// All episodes on disk are watched and no more episodes coming!
	// Build reason with season info
	seasonsOnDisk := getSeasonsWithFiles(ser)
	watchedReason := fmt.Sprintf("all on-disk episodes watched (S%s)", formatSeasons(seasonsOnDisk))

	// Check if already in queue
	if s.queue.IsQueued(ser.ID) {
		queueItem := s.queue.Get(ser.ID)
		daysInQueue := int(time.Since(queueItem.MarkedAt).Hours() / 24)
		daysUntil := s.cfg.DelayDays - daysInQueue
		if daysUntil < 0 {
			daysUntil = 0
		}
		result.Action = "queued"
		result.Reason = watchedReason
		result.DaysUntil = daysUntil
		return result
	}

	// Add to queue
	s.queue.Add(&QueueItem{
		SonarrID:   ser.ID,
		TvdbID:     ser.TvdbID,
		Title:      ser.Title,
		MarkedAt:   time.Now(),
		Reason:     watchedReason,
		SizeOnDisk: ser.Statistics.SizeOnDisk,
	})

	result.Action = "queued"
	result.Reason = watchedReason + " - added to queue"
	result.DaysUntil = s.cfg.DelayDays
	return result
}

// checkMoreEpisodesComing checks if any season with files has more episodes to air
func checkMoreEpisodesComing(ser *sonarr.Series, progress *trakt.ShowProgress, seasons []trakt.SeasonSummary) (bool, string) {
	// Build map of total episode counts per season
	totalEps := make(map[int]int)
	for _, s := range seasons {
		totalEps[s.Number] = s.EpisodeCount
	}

	// Build map of season progress
	progressMap := make(map[int]*trakt.SeasonProgress)
	for i := range progress.Seasons {
		progressMap[progress.Seasons[i].Number] = &progress.Seasons[i]
	}

	// Check each season that has files on disk
	for _, season := range ser.Seasons {
		if season.SeasonNumber == 0 {
			continue
		}
		if season.Statistics == nil || season.Statistics.EpisodeFileCount == 0 {
			continue
		}

		// This season has files - check if more episodes are coming
		total := totalEps[season.SeasonNumber]
		sp := progressMap[season.SeasonNumber]

		if sp != nil && total > 0 && sp.Aired < total {
			// More episodes to air in this season
			return true, fmt.Sprintf("S%02d ongoing (%d/%d eps, %d aired)",
				season.SeasonNumber, sp.Completed, total, sp.Aired)
		}
	}

	// Also check NextEpisode as fallback
	if progress.NextEpisode != nil {
		seasonNum := progress.NextEpisode.Season
		sp := progressMap[seasonNum]
		total := totalEps[seasonNum]
		if sp != nil {
			if total == 0 {
				total = sp.Aired
			}
			return true, fmt.Sprintf("S%02d ongoing (%d/%d eps, %d aired)",
				seasonNum, sp.Completed, total, sp.Aired)
		}
		return true, fmt.Sprintf("S%02d ongoing", seasonNum)
	}

	return false, ""
}

// buildWatchingReason builds the skip reason when user is still watching
func buildWatchingReason(progress *trakt.ShowProgress, seasons []trakt.SeasonSummary, unwatchedSeasons []int) string {
	if len(unwatchedSeasons) == 0 {
		return "still watching"
	}

	// Use the first unwatched season for the message
	seasonNum := unwatchedSeasons[0]

	// Find season progress
	var seasonProgress *trakt.SeasonProgress
	for i := range progress.Seasons {
		if progress.Seasons[i].Number == seasonNum {
			seasonProgress = &progress.Seasons[i]
			break
		}
	}

	// Find total episode count from seasons
	total := 0
	for _, s := range seasons {
		if s.Number == seasonNum {
			total = s.EpisodeCount
			break
		}
	}

	if seasonProgress != nil {
		if total == 0 {
			total = seasonProgress.Aired // fallback
		}
		return fmt.Sprintf("watching S%02d (%d/%d eps, %d aired)",
			seasonNum, seasonProgress.Completed, total, seasonProgress.Aired)
	}

	return fmt.Sprintf("S%02d not watched", seasonNum)
}

// buildAwaitingReason builds the skip reason when more episodes are coming
func buildAwaitingReason(progress *trakt.ShowProgress, seasons []trakt.SeasonSummary) string {
	nextEp := progress.NextEpisode
	seasonNum := nextEp.Season

	// Find season progress
	var seasonProgress *trakt.SeasonProgress
	for i := range progress.Seasons {
		if progress.Seasons[i].Number == seasonNum {
			seasonProgress = &progress.Seasons[i]
			break
		}
	}

	// Find total episode count from seasons
	total := 0
	for _, s := range seasons {
		if s.Number == seasonNum {
			total = s.EpisodeCount
			break
		}
	}

	if seasonProgress != nil {
		if total == 0 {
			total = seasonProgress.Aired // fallback
		}
		return fmt.Sprintf("S%02d ongoing (%d/%d eps, %d aired)",
			seasonNum, seasonProgress.Completed, total, seasonProgress.Aired)
	}

	return fmt.Sprintf("S%02d ongoing", seasonNum)
}

// formatSeasons formats season numbers like "1,2,3" or "2"
func formatSeasons(seasons []int) string {
	if len(seasons) == 0 {
		return ""
	}
	strs := make([]string, len(seasons))
	for i, s := range seasons {
		strs[i] = fmt.Sprintf("%02d", s)
	}
	return strings.Join(strs, ",")
}

// getSeasonsWithFiles returns season numbers that have files on disk
func getSeasonsWithFiles(ser *sonarr.Series) []int {
	var seasons []int
	for _, s := range ser.Seasons {
		if s.SeasonNumber == 0 {
			continue
		}
		if s.Statistics != nil && s.Statistics.EpisodeFileCount > 0 {
			seasons = append(seasons, s.SeasonNumber)
		}
	}
	return seasons
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
			logger.Infof("✅ Deleted: %s (%s freed)", item.Title, sonarr.FormatSize(item.SizeOnDisk))
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
	for _, exc := range s.cfg.Exclusions {
		if strings.EqualFold(exc, title) {
			return true
		}
	}
	return false
}

// checkWatchedOnDisk checks if user has watched all episodes that exist on disk in Sonarr
// Returns (allWatched, unwatchedSeasonNumbers)
func (s *Service) checkWatchedOnDisk(ser *sonarr.Series, progress *trakt.ShowProgress) (bool, []int) {
	var unwatchedSeasons []int

	// Build map of Trakt progress by season number
	traktProgress := make(map[int]*trakt.SeasonProgress)
	for i := range progress.Seasons {
		traktProgress[progress.Seasons[i].Number] = &progress.Seasons[i]
	}

	// Check each season in Sonarr that has files
	for _, season := range ser.Seasons {
		// Skip specials (season 0) and seasons with no files
		if season.SeasonNumber == 0 {
			continue
		}
		if season.Statistics == nil || season.Statistics.EpisodeFileCount == 0 {
			continue
		}

		// This season has files on disk - check if watched
		traktSeason, found := traktProgress[season.SeasonNumber]
		if !found {
			// No watch progress for this season at all
			unwatchedSeasons = append(unwatchedSeasons, season.SeasonNumber)
			continue
		}

		// Check if user watched at least as many episodes as we have files
		// (we can't know exactly which episodes are on disk, so we compare counts)
		filesOnDisk := season.Statistics.EpisodeFileCount
		watchedEpisodes := traktSeason.Completed

		if watchedEpisodes < filesOnDisk {
			unwatchedSeasons = append(unwatchedSeasons, season.SeasonNumber)
		}
	}

	return len(unwatchedSeasons) == 0, unwatchedSeasons
}

func (s *Service) printSummary(result *ProcessingResult, startTime time.Time) {
	var toRemove []string
	var queued []string
	var skipped []string
	var errors []string

	for _, r := range result.Details {
		switch r.Action {
		case "removed", "dry_run_remove":
			info := fmt.Sprintf("   ✅ %-35s", r.Title)
			if r.SizeOnDisk != "" {
				info += fmt.Sprintf(" [%s]", r.SizeOnDisk)
			}
			info += fmt.Sprintf("  ← %s", r.Reason)
			toRemove = append(toRemove, info)
		case "queued":
			info := fmt.Sprintf("   ⏳ %-35s", r.Title)
			if r.SizeOnDisk != "" {
				info += fmt.Sprintf(" [%s]", r.SizeOnDisk)
			}
			if r.DaysUntil > 0 {
				info += fmt.Sprintf("  ← %s (removes in %d days)", r.Reason, r.DaysUntil)
			} else {
				info += fmt.Sprintf("  ← %s (ready to remove)", r.Reason)
			}
			queued = append(queued, info)
		case "skipped":
			info := fmt.Sprintf("   ⏭️  %-35s", r.Title)
			info += fmt.Sprintf("  ← %s", r.Reason)
			skipped = append(skipped, info)
		case "error":
			info := fmt.Sprintf("   ❌ %-35s", r.Title)
			info += fmt.Sprintf("  ← %s", r.Reason)
			errors = append(errors, info)
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
		logger.Infof("⏳ QUEUED FOR REMOVAL (%d):", len(queued))
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

// sendNotification sends a notification with cleanup results
func (s *Service) sendNotification(ctx context.Context, result *ProcessingResult) {
	if s.apprise == nil || !s.apprise.IsEnabled() {
		return
	}

	// Build notification
	logger.Info("🔔 Sending notification...")
	formatter := &apprise.SlackFormatter{}
	var details []apprise.CleanupDetail
	for _, r := range result.Details {
		details = append(details, apprise.CleanupDetail{
			Title:      r.Title,
			Action:     r.Action,
			Reason:     r.Reason,
			DaysUntil:  r.DaysUntil,
			SizeOnDisk: r.SizeOnDisk,
		})
	}

	title := "🧹 Cleanup Results"
	if s.dryRun {
		title = "🧹 Cleanup Results (DRY RUN)"
	}

	body := formatter.FormatCleanupResults(result.Removed, result.MarkedForQueue, result.Skipped, result.Errors, details, s.dryRun)

	notifyType := "info"
	if result.Removed > 0 {
		notifyType = "success"
	}

	if err := s.apprise.Notify(ctx, title, body, notifyType); err != nil {
		logger.Warnf("🔔 Failed to send notification: %v", err)
	} else {
		logger.Info("🔔 Notification sent successfully")
	}
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
