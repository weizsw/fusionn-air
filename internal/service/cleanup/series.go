package cleanup

import (
	"context"
	"fmt"
	"strings"

	"github.com/fusionn-air/internal/client/sonarr"
	"github.com/fusionn-air/internal/client/trakt"
	"github.com/fusionn-air/internal/config"
	"github.com/fusionn-air/pkg/logger"
)

func (s *Service) processSeries(ctx context.Context, result *ProcessingResult, cfg *config.Config, dryRun bool) (sonarrTvdbIDs map[int]bool, runErr error) {
	if s.sonarr == nil {
		logger.Debug("Sonarr client not configured, skipping series cleanup")
		return nil, nil
	}

	queue := s.queues[MediaTypeSeries]
	if err := retryPendingUnmonitor(queue, result, MediaTypeSeries, dryRun, func(item *QueueItem) (bool, error) {
		return s.unmonitorSeries(ctx, item.ID, item.Title, queue)
	}); err != nil {
		return nil, err
	}

	logger.Info("📺 Fetching series from Sonarr...")
	series, err := s.sonarr.GetAllSeries(ctx)
	if err != nil {
		logger.Errorf("❌ Failed to get series from Sonarr: %v", err)
		return nil, nil
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
	watchedAvailable := err == nil
	if err != nil {
		logger.Errorf("❌ Failed to get watched shows: %v", err)
	}

	// Build lookup by TVDB ID
	watchedByTvdb := make(map[int]*trakt.WatchedShow)
	for i := range watchedShows {
		if watchedShows[i].Show.IDs.TVDB > 0 {
			watchedByTvdb[watchedShows[i].Show.IDs.TVDB] = &watchedShows[i]
		}
	}

	for _, ser := range series {
		existing := queue.Get(ser.ID) != nil
		if !existing && !watchedAvailable {
			continue
		}
		res, err := s.processOneSeries(ctx, &ser, watchedByTvdb, queue, cfg, dryRun)
		if !existing && !dryRun && err == nil && res.Action == "queued" && queue.NeedsUnmonitor(ser.ID) {
			unmonitored, persistErr := s.unmonitorSeries(ctx, ser.ID, ser.Title, queue)
			if persistErr != nil {
				res.Action = "error"
				res.Reason = persistErr.Error()
				err = persistErr
			} else if unmonitored {
				res.Reason = queue.Get(ser.ID).Reason + " - queued for deletion (unmonitored)"
			} else {
				res.Reason = queue.Get(ser.ID).Reason + " - pending unmonitor"
			}
		}
		if res.ID != 0 {
			result.AddResult(res)
		}
		if err != nil {
			return sonarrTvdbIDs, fmt.Errorf("process series %q: %w", ser.Title, err)
		}
	}

	if err := s.processSeriesRemovalQueue(ctx, result, queue, cfg, dryRun); err != nil {
		return sonarrTvdbIDs, err
	}
	return sonarrTvdbIDs, nil
}

func (s *Service) processOneSeries(ctx context.Context, ser *sonarr.Series, watchedByTvdb map[int]*trakt.WatchedShow, queue *Queue, cfg *config.Config, dryRun bool) (MediaResult, error) {
	res := MediaResult{Type: MediaTypeSeries, Title: ser.Title, ID: ser.ID, SizeOnDisk: sonarr.FormatSize(ser.Statistics.SizeOnDisk)}
	if isExcluded(ser.Title, cfg.Cleanup.Exclusions) {
		res.Action, res.Reason = "skipped", "in exclusion list"
		return res, nil
	}
	if queued, found := existingCandidateResult(queue, ser.ID, cfg.Cleanup.DelayDays, res, " - queued for deletion (unmonitored)"); found {
		return queued, nil
	}
	if !ser.Monitored {
		res.Action, res.Reason = "skipped", "not monitored"
		return res, nil
	}
	if ser.Statistics.EpisodeFileCount == 0 {
		res.Action = "skipped"
		if ser.Status == sonarr.StatusUpcoming || ser.Statistics.EpisodeCount == 0 {
			res.Reason = "not yet aired"
		} else {
			res.Reason = "no files on disk"
		}
		return res, nil
	}

	watched, found := watchedByTvdb[ser.TvdbID]
	if !found {
		res.Action, res.Reason = "skipped", "no watch history"
		return res, nil
	}
	progress, err := s.trakt.GetShowProgress(ctx, watched.Show.IDs.Trakt)
	if err != nil {
		res.Action, res.Reason = "error", fmt.Sprintf("trakt error: %v", err)
		return res, nil
	}
	seasons, _ := s.trakt.GetShowSeasons(ctx, watched.Show.IDs.Trakt)
	watchedOnDisk, unwatchedSeasons := checkWatchedOnDisk(ser, progress)
	if !watchedOnDisk {
		res.Action, res.Reason = "skipped", buildWatchingReason(progress, seasons, unwatchedSeasons)
		return res, nil
	}
	if moreEpisodesComing, reason := checkMoreEpisodesComing(ser, progress, seasons); moreEpisodesComing {
		res.Action, res.Reason = "skipped", reason
		return res, nil
	}

	watchedReason := fmt.Sprintf("fully watched (S%s)", formatSeasons(getSeasonsWithFiles(ser)))
	return scheduleCandidate(queue, &QueueItem{
		ID: ser.ID, ExternalID: ser.TvdbID, Title: ser.Title,
		Reason: watchedReason, SizeOnDisk: ser.Statistics.SizeOnDisk,
	}, cfg.Cleanup.DelayDays, dryRun, res, " - queued for deletion (unmonitored)")
}

func (s *Service) processSeriesRemovalQueue(ctx context.Context, result *ProcessingResult, queue *Queue, cfg *config.Config, dryRun bool) error {
	return processReadyCandidates(ctx, result, queue, cfg.Cleanup.DelayDays, cfg.Cleanup.Exclusions, dryRun, removalPlan{
		mediaType: MediaTypeSeries, label: "series", removedReason: "deleted", formatSize: sonarr.FormatSize,
		remove: func(ctx context.Context, item *QueueItem) (bool, error) {
			series, err := s.sonarr.GetSeries(ctx, item.ID)
			if err != nil || series == nil {
				return false, err
			}
			return true, s.sonarr.DeleteSeries(ctx, item.ID, true)
		},
	})
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

func (s *Service) unmonitorSeries(ctx context.Context, seriesID int, title string, queue *Queue) (bool, error) {
	if err := s.sonarr.UnmonitorSeries(ctx, seriesID); err != nil {
		logger.Warnf("⚠️  Failed to unmonitor %s: %v", title, err)
		return false, nil
	}
	if err := queue.MarkUnmonitored(seriesID); err != nil {
		return false, fmt.Errorf("unmonitored %q but queue persistence failed: %w", title, err)
	}
	logger.Infof("🔕 Unmonitored series: %s (queued for deletion)", title)
	return true, nil
}
