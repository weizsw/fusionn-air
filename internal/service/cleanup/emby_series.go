package cleanup

import (
	"context"
	"fmt"
	"strconv"

	"github.com/fusionn-air/internal/client/emby"
	"github.com/fusionn-air/internal/client/trakt"
	"github.com/fusionn-air/internal/config"
	"github.com/fusionn-air/pkg/logger"
)

func (s *Service) processEmbySeriesItems(ctx context.Context, result *ProcessingResult, cfg *config.Config, dryRun bool, sonarrTvdbIDs map[int]bool, series []emby.Item) error {
	if s.emby == nil {
		return nil
	}

	queue := s.queues[MediaTypeEmbySeries]

	if sonarrTvdbIDs == nil || len(series) == 0 {
		return s.processEmbySeriesRemovalQueue(ctx, result, queue, cfg, dryRun)
	}

	logger.Infof("📺 Total series fetched: %d", len(series))

	// Filter to orphans only (not in Sonarr)
	var orphans []emby.Item
	for _, item := range series {
		tvdbID := emby.ParseProviderID(item.ProviderIDs, "Tvdb")
		if tvdbID == 0 {
			logger.Warnf("Skipping Emby series %q (no TVDB ID)", item.Name)
			continue
		}
		if sonarrTvdbIDs[tvdbID] {
			continue
		}
		orphans = append(orphans, item)
	}

	result.IncrementScanned(MediaTypeEmbySeries, len(orphans))
	logger.Infof("📺 Found %d orphan series in Emby (not in Sonarr)", len(orphans))

	if len(orphans) == 0 {
		return s.processEmbySeriesRemovalQueue(ctx, result, queue, cfg, dryRun)
	}

	logger.Info("👁️  Fetching TV watch history from Trakt...")
	watchedShows, err := s.trakt.GetWatchedShows(ctx)
	if err != nil {
		logger.Errorf("❌ Failed to get watched shows: %v", err)
		return s.processEmbySeriesRemovalQueue(ctx, result, queue, cfg, dryRun)
	}

	watchedByTvdb := make(map[int]*trakt.WatchedShow)
	for i := range watchedShows {
		if watchedShows[i].Show.IDs.TVDB > 0 {
			watchedByTvdb[watchedShows[i].Show.IDs.TVDB] = &watchedShows[i]
		}
	}

	for _, item := range orphans {
		res, err := s.processOneEmbySeries(ctx, item, watchedByTvdb, queue, cfg, dryRun)
		if res.ID != 0 {
			result.AddResult(res)
		}
		if err != nil {
			return fmt.Errorf("process Emby series %q: %w", item.Name, err)
		}
	}

	return s.processEmbySeriesRemovalQueue(ctx, result, queue, cfg, dryRun)
}

func (s *Service) processOneEmbySeries(ctx context.Context, item emby.Item, watchedByTvdb map[int]*trakt.WatchedShow, queue *Queue, cfg *config.Config, dryRun bool) (MediaResult, error) {
	embyID, _ := strconv.Atoi(item.ID)
	if embyID == 0 {
		logger.Warnf("Skipping Emby series %q (invalid ID: %s)", item.Name, item.ID)
		return MediaResult{}, nil
	}
	tvdbID := emby.ParseProviderID(item.ProviderIDs, "Tvdb")
	res := MediaResult{Type: MediaTypeEmbySeries, Title: item.Name, ID: embyID}
	if isExcluded(item.Name, cfg.Cleanup.Exclusions) {
		res.Action, res.Reason = "skipped", "in exclusion list"
		return res, nil
	}
	if queued, found := existingCandidateResult(queue, embyID, cfg.Cleanup.DelayDays, res, " - queued for deletion"); found {
		return queued, nil
	}
	watched, found := watchedByTvdb[tvdbID]
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
	watchedOnDisk, unwatchedSeasons := s.checkEmbyWatchedOnDisk(ctx, item.ID, progress)
	if !watchedOnDisk {
		res.Action, res.Reason = "skipped", buildWatchingReason(progress, seasons, unwatchedSeasons)
		return res, nil
	}
	if moreEpisodesComing, reason := checkEmbyMoreEpisodesComing(progress, seasons); moreEpisodesComing {
		res.Action, res.Reason = "skipped", reason
		return res, nil
	}

	const watchedReason = "fully watched (via Emby)"
	return scheduleCandidate(queue, &QueueItem{ID: embyID, ExternalID: tvdbID, Title: item.Name, Reason: watchedReason}, cfg.Cleanup.DelayDays, dryRun, res, " - queued for deletion")
}

func (s *Service) checkEmbyWatchedOnDisk(ctx context.Context, seriesID string, progress *trakt.ShowProgress) (bool, []int) {
	var unwatchedSeasons []int

	traktProgress := make(map[int]*trakt.SeasonProgress)
	for i := range progress.Seasons {
		traktProgress[progress.Seasons[i].Number] = &progress.Seasons[i]
	}

	seasons, err := s.emby.GetSeasons(ctx, seriesID)
	if err != nil {
		logger.Warnf("Failed to get Emby seasons for %s: %v", seriesID, err)
		return false, nil
	}

	for _, season := range seasons {
		seasonNum := season.IndexNumber
		if seasonNum == 0 {
			continue
		}

		episodes, err := s.emby.GetEpisodes(ctx, seriesID, season.ID)
		if err != nil {
			logger.Warnf("Failed to get Emby episodes for season %d: %v", seasonNum, err)
			continue
		}

		var filesOnDisk int
		for _, ep := range episodes {
			if ep.LocationType != "Virtual" {
				filesOnDisk++
			}
		}

		if filesOnDisk == 0 {
			continue
		}

		traktSeason, found := traktProgress[seasonNum]
		if !found {
			unwatchedSeasons = append(unwatchedSeasons, seasonNum)
			continue
		}

		if traktSeason.Completed < filesOnDisk {
			unwatchedSeasons = append(unwatchedSeasons, seasonNum)
		}
	}

	return len(unwatchedSeasons) == 0, unwatchedSeasons
}

func checkEmbyMoreEpisodesComing(progress *trakt.ShowProgress, seasons []trakt.SeasonSummary) (bool, string) {
	totalEps := make(map[int]int)
	for _, s := range seasons {
		totalEps[s.Number] = s.EpisodeCount
	}

	for _, sp := range progress.Seasons {
		if sp.Number == 0 {
			continue
		}
		total := totalEps[sp.Number]
		if total > 0 && sp.Aired < total {
			return true, fmt.Sprintf("S%02d ongoing (%d/%d aired)", sp.Number, sp.Aired, total)
		}
	}

	if progress.NextEpisode != nil {
		return true, fmt.Sprintf("S%02d ongoing", progress.NextEpisode.Season)
	}

	return false, ""
}

func (s *Service) processEmbySeriesRemovalQueue(ctx context.Context, result *ProcessingResult, queue *Queue, cfg *config.Config, dryRun bool) error {
	return processReadyCandidates(ctx, result, queue, cfg.Cleanup.DelayDays, cfg.Cleanup.Exclusions, dryRun, removalPlan{
		mediaType: MediaTypeEmbySeries, label: "Emby series", removedReason: "deleted from Emby",
		remove: func(ctx context.Context, item *QueueItem) (bool, error) {
			return true, s.emby.DeleteItem(ctx, strconv.Itoa(item.ID))
		},
	})
}
