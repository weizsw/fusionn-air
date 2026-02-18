package cleanup

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/fusionn-air/internal/client/sonarr"
	"github.com/fusionn-air/internal/client/trakt"
	"github.com/fusionn-air/internal/config"
	"github.com/fusionn-air/pkg/logger"
)

func (s *Service) processSeries(ctx context.Context, result *ProcessingResult, cfg *config.Config, dryRun bool) (sonarrTvdbIDs map[int]bool) {
	if s.sonarr == nil {
		logger.Debug("Sonarr client not configured, skipping series cleanup")
		return nil
	}

	queue := s.queues[MediaTypeSeries]

	logger.Info("📺 Fetching series from Sonarr...")
	series, err := s.sonarr.GetAllSeries(ctx)
	if err != nil {
		logger.Errorf("❌ Failed to get series from Sonarr: %v", err)
		return nil
	}

	sonarrTvdbIDs = make(map[int]bool, len(series))
	for _, ser := range series {
		if ser.TvdbID > 0 {
			sonarrTvdbIDs[ser.TvdbID] = true
		}
	}

	result.IncrementScanned(MediaTypeSeries, len(series))
	logger.Infof("📺 Found %d series in Sonarr", len(series))

	// Get watched shows from Trakt
	logger.Info("👁️  Fetching TV watch history from Trakt...")
	watchedShows, err := s.trakt.GetWatchedShows(ctx)
	if err != nil {
		logger.Errorf("❌ Failed to get watched shows: %v", err)
		return
	}

	// Build lookup by TVDB ID
	watchedByTvdb := make(map[int]*trakt.WatchedShow)
	for i := range watchedShows {
		if watchedShows[i].Show.IDs.TVDB > 0 {
			watchedByTvdb[watchedShows[i].Show.IDs.TVDB] = &watchedShows[i]
		}
	}

	// Process each series
	for _, ser := range series {
		res := s.processOneSeries(ctx, &ser, watchedByTvdb, queue, cfg)
		// Unmonitor if newly queued
		if res.Action == "queued" && res.Reason != "" && strings.HasSuffix(res.Reason, "added to queue") {
			s.unmonitorSeries(ctx, ser.ID, ser.Title, queue, dryRun)
		}
		// Only add non-empty results (skip items ready for removal)
		if res.ID != 0 {
			result.AddResult(res)
		}
	}

	s.processSeriesRemovalQueue(ctx, result, queue, cfg, dryRun)
	return
}

func (s *Service) processOneSeries(ctx context.Context, ser *sonarr.Series, watchedByTvdb map[int]*trakt.WatchedShow, queue *Queue, cfg *config.Config) MediaResult {
	res := MediaResult{
		Type:       MediaTypeSeries,
		Title:      ser.Title,
		ID:         ser.ID,
		SizeOnDisk: sonarr.FormatSize(ser.Statistics.SizeOnDisk),
	}

	// Check exclusions
	if isExcluded(ser.Title, cfg.Cleanup.Exclusions) {
		res.Action = "skipped"
		res.Reason = "in exclusion list"
		return res
	}

	// Check if already in queue (before checking monitored status)
	// This ensures queued items remain visible even after being unmonitored
	// However, skip items that are ready for removal - they'll appear in REMOVED section
	if queue.IsQueued(ser.ID) {
		// If item is ready for removal, skip it here so it doesn't appear twice
		// It will be processed by processSeriesRemovalQueue and appear as "removed"
		if queue.IsReadyForRemoval(ser.ID, cfg.Cleanup.DelayDays) {
			// Return empty result - this item will be handled by removal queue
			return MediaResult{}
		}

		queueItem := queue.Get(ser.ID)
		daysInQueue := int(time.Since(queueItem.MarkedAt).Hours() / 24)
		daysUntil := cfg.Cleanup.DelayDays - daysInQueue
		if daysUntil < 0 {
			daysUntil = 0
		}
		res.Action = "queued"
		res.Reason = queueItem.Reason + " - queued for deletion (unmonitored)"
		res.DaysUntil = daysUntil
		return res
	}

	// Check if series is monitored
	if !ser.Monitored {
		res.Action = "skipped"
		res.Reason = "not monitored"
		return res
	}

	// Check if series has any files
	if ser.Statistics.EpisodeFileCount == 0 {
		res.Action = "skipped"
		if ser.Status == sonarr.StatusUpcoming || ser.Statistics.EpisodeCount == 0 {
			res.Reason = "not yet aired"
		} else {
			res.Reason = "no files on disk"
		}
		return res
	}

	// Find in Trakt watched shows
	watched, found := watchedByTvdb[ser.TvdbID]
	if !found {
		res.Action = "skipped"
		res.Reason = "no watch history"
		return res
	}

	// Get detailed progress from Trakt
	progress, err := s.trakt.GetShowProgress(ctx, watched.Show.IDs.Trakt)
	if err != nil {
		res.Action = "error"
		res.Reason = fmt.Sprintf("trakt error: %v", err)
		return res
	}

	// Get season info for total episode counts
	seasons, _ := s.trakt.GetShowSeasons(ctx, watched.Show.IDs.Trakt)

	// Check if user has watched all episodes that are ON DISK
	watchedOnDisk, unwatchedSeasons := checkWatchedOnDisk(ser, progress)
	if !watchedOnDisk {
		res.Action = "skipped"
		res.Reason = buildWatchingReason(progress, seasons, unwatchedSeasons)
		return res
	}

	// Check if more episodes are coming
	moreEpisodesComing, ongoingReason := checkMoreEpisodesComing(ser, progress, seasons)
	if moreEpisodesComing {
		if queue.IsQueued(ser.ID) {
			queue.Remove(ser.ID)
			logger.Debugf("Removed %s from queue - more episodes coming", ser.Title)
		}
		res.Action = "skipped"
		res.Reason = ongoingReason
		return res
	}

	// All episodes on disk are watched and no more coming
	seasonsOnDisk := getSeasonsWithFiles(ser)
	watchedReason := fmt.Sprintf("fully watched (S%s)", formatSeasons(seasonsOnDisk))

	// Add to queue
	queue.Add(&QueueItem{
		ID:         ser.ID,
		ExternalID: ser.TvdbID,
		Title:      ser.Title,
		MarkedAt:   time.Now(),
		Reason:     watchedReason,
		SizeOnDisk: ser.Statistics.SizeOnDisk,
	})

	res.Action = "queued"
	res.Reason = watchedReason + " - queued for deletion (unmonitored)"
	res.DaysUntil = cfg.Cleanup.DelayDays
	return res
}

func (s *Service) processSeriesRemovalQueue(ctx context.Context, result *ProcessingResult, queue *Queue, cfg *config.Config, dryRun bool) {
	ready := queue.GetReadyForRemoval(cfg.Cleanup.DelayDays)
	if len(ready) == 0 {
		return
	}

	logger.Infof("🗑️  %d series ready for removal", len(ready))

	for _, item := range ready {
		ser, err := s.sonarr.GetSeries(ctx, item.ID)
		if err != nil {
			logger.Errorf("❌ Error checking series %s: %v", item.Title, err)
			continue
		}

		if ser == nil {
			logger.Infof("ℹ️  %s already removed, clearing from queue", item.Title)
			queue.Remove(item.ID)
			continue
		}

		if dryRun {
			logger.Warnf("🗑️  [DRY RUN] Would delete: %s (%s)", item.Title, sonarr.FormatSize(item.SizeOnDisk))
			result.AddResult(MediaResult{
				Type:       MediaTypeSeries,
				Title:      item.Title,
				ID:         item.ID,
				Action:     "dry_run_remove",
				Reason:     "would be deleted",
				SizeOnDisk: sonarr.FormatSize(item.SizeOnDisk),
			})
		} else {
			if err := s.sonarr.DeleteSeries(ctx, item.ID, true); err != nil {
				logger.Errorf("❌ Failed to delete %s: %v", item.Title, err)
				result.AddResult(MediaResult{
					Type:   MediaTypeSeries,
					Title:  item.Title,
					ID:     item.ID,
					Action: "error",
					Reason: fmt.Sprintf("delete failed: %v", err),
				})
				continue
			}
			logger.Infof("✅ Deleted: %s (%s freed)", item.Title, sonarr.FormatSize(item.SizeOnDisk))
			result.AddResult(MediaResult{
				Type:       MediaTypeSeries,
				Title:      item.Title,
				ID:         item.ID,
				Action:     "removed",
				Reason:     "deleted",
				SizeOnDisk: sonarr.FormatSize(item.SizeOnDisk),
			})
		}

		queue.Remove(item.ID)
	}
}

// checkWatchedOnDisk checks if user has watched all episodes on disk
func checkWatchedOnDisk(ser *sonarr.Series, progress *trakt.ShowProgress) (bool, []int) {
	var unwatchedSeasons []int

	traktProgress := make(map[int]*trakt.SeasonProgress)
	for i := range progress.Seasons {
		traktProgress[progress.Seasons[i].Number] = &progress.Seasons[i]
	}

	for _, season := range ser.Seasons {
		if season.SeasonNumber == 0 {
			continue
		}
		if season.Statistics == nil || season.Statistics.EpisodeFileCount == 0 {
			continue
		}

		traktSeason, found := traktProgress[season.SeasonNumber]
		if !found {
			unwatchedSeasons = append(unwatchedSeasons, season.SeasonNumber)
			continue
		}

		if traktSeason.Completed < season.Statistics.EpisodeFileCount {
			unwatchedSeasons = append(unwatchedSeasons, season.SeasonNumber)
		}
	}

	return len(unwatchedSeasons) == 0, unwatchedSeasons
}

// checkMoreEpisodesComing checks if any season with files has more episodes to air
func checkMoreEpisodesComing(ser *sonarr.Series, progress *trakt.ShowProgress, seasons []trakt.SeasonSummary) (bool, string) {
	totalEps := make(map[int]int)
	for _, s := range seasons {
		totalEps[s.Number] = s.EpisodeCount
	}

	progressMap := make(map[int]*trakt.SeasonProgress)
	for i := range progress.Seasons {
		progressMap[progress.Seasons[i].Number] = &progress.Seasons[i]
	}

	for _, season := range ser.Seasons {
		if season.SeasonNumber == 0 || season.Statistics == nil || season.Statistics.EpisodeFileCount == 0 {
			continue
		}

		total := totalEps[season.SeasonNumber]
		sp := progressMap[season.SeasonNumber]

		if sp != nil && total > 0 && sp.Aired < total {
			return true, fmt.Sprintf("S%02d ongoing (%d/%d aired)", season.SeasonNumber, sp.Aired, total)
		}
	}

	if progress.NextEpisode != nil {
		return true, fmt.Sprintf("S%02d ongoing", progress.NextEpisode.Season)
	}

	return false, ""
}

// buildWatchingReason builds the skip reason when user is still watching
func buildWatchingReason(progress *trakt.ShowProgress, seasons []trakt.SeasonSummary, unwatchedSeasons []int) string {
	if len(unwatchedSeasons) == 0 {
		return "still watching"
	}

	seasonNum := unwatchedSeasons[0]

	var sp *trakt.SeasonProgress
	for i := range progress.Seasons {
		if progress.Seasons[i].Number == seasonNum {
			sp = &progress.Seasons[i]
			break
		}
	}

	total := 0
	for _, s := range seasons {
		if s.Number == seasonNum {
			total = s.EpisodeCount
			break
		}
	}

	if sp != nil {
		if total == 0 {
			total = sp.Aired
		}
		return fmt.Sprintf("watching S%02d (%d/%d)", seasonNum, sp.Completed, total)
	}

	return fmt.Sprintf("S%02d unwatched", seasonNum)
}

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

// unmonitorSeries unmonitors a series in Sonarr when it's added to the cleanup queue
func (s *Service) unmonitorSeries(ctx context.Context, seriesID int, title string, queue *Queue, dryRun bool) {
	if dryRun {
		logger.Warnf("🔕 [DRY RUN] Would unmonitor series: %s (queued for deletion)", title)
		return
	}

	if err := s.sonarr.UnmonitorSeries(ctx, seriesID); err != nil {
		logger.Warnf("⚠️  Failed to unmonitor %s: %v", title, err)
		return
	}

	logger.Infof("🔕 Unmonitored series: %s (queued for deletion)", title)
	queue.MarkUnmonitored(seriesID)
}
