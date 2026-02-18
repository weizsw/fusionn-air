package cleanup

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/fusionn-air/internal/client/emby"
	"github.com/fusionn-air/internal/client/trakt"
	"github.com/fusionn-air/internal/config"
	"github.com/fusionn-air/pkg/logger"
)

func (s *Service) processEmbySeries(ctx context.Context, result *ProcessingResult, cfg *config.Config, dryRun bool, sonarrTvdbIDs map[int]bool) {
	if s.emby == nil {
		return
	}

	queue := s.queues[MediaTypeEmbySeries]

	logger.Info("📺 Fetching series from Emby...")
	embyItems, err := s.emby.GetAllSeries(ctx)
	if err != nil {
		logger.Errorf("❌ Failed to get series from Emby: %v", err)
		return
	}

	// Filter to orphans only (not in Sonarr)
	var orphans []emby.Item
	for _, item := range embyItems {
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
		return
	}

	logger.Info("👁️  Fetching TV watch history from Trakt...")
	watchedShows, err := s.trakt.GetWatchedShows(ctx)
	if err != nil {
		logger.Errorf("❌ Failed to get watched shows: %v", err)
		return
	}

	watchedByTvdb := make(map[int]*trakt.WatchedShow)
	for i := range watchedShows {
		if watchedShows[i].Show.IDs.TVDB > 0 {
			watchedByTvdb[watchedShows[i].Show.IDs.TVDB] = &watchedShows[i]
		}
	}

	for _, item := range orphans {
		res := s.processOneEmbySeries(ctx, item, watchedByTvdb, queue, cfg)
		if res.ID != 0 {
			result.AddResult(res)
		}
	}

	s.processEmbySeriesRemovalQueue(ctx, result, queue, cfg, dryRun)
}

func (s *Service) processOneEmbySeries(ctx context.Context, item emby.Item, watchedByTvdb map[int]*trakt.WatchedShow, queue *Queue, cfg *config.Config) MediaResult {
	embyID, err := strconv.Atoi(item.ID)
	if err != nil || embyID == 0 {
		logger.Warnf("Skipping Emby series %q (invalid ID: %s)", item.Name, item.ID)
		return MediaResult{}
	}
	tvdbID := emby.ParseProviderID(item.ProviderIDs, "Tvdb")

	res := MediaResult{
		Type:  MediaTypeEmbySeries,
		Title: item.Name,
		ID:    embyID,
	}

	if isExcluded(item.Name, cfg.Cleanup.Exclusions) {
		res.Action = "skipped"
		res.Reason = "in exclusion list"
		return res
	}

	if queue.IsQueued(embyID) {
		if queue.IsReadyForRemoval(embyID, cfg.Cleanup.DelayDays) {
			return MediaResult{}
		}
		queueItem := queue.Get(embyID)
		daysInQueue := int(time.Since(queueItem.MarkedAt).Hours() / 24)
		daysUntil := cfg.Cleanup.DelayDays - daysInQueue
		if daysUntil < 0 {
			daysUntil = 0
		}
		res.Action = "queued"
		res.Reason = queueItem.Reason + " - queued for deletion"
		res.DaysUntil = daysUntil
		return res
	}

	watched, found := watchedByTvdb[tvdbID]
	if !found {
		res.Action = "skipped"
		res.Reason = "no watch history"
		return res
	}

	progress, err := s.trakt.GetShowProgress(ctx, watched.Show.IDs.Trakt)
	if err != nil {
		res.Action = "error"
		res.Reason = fmt.Sprintf("trakt error: %v", err)
		return res
	}

	seasons, _ := s.trakt.GetShowSeasons(ctx, watched.Show.IDs.Trakt)

	watchedOnDisk, unwatchedSeasons := s.checkEmbyWatchedOnDisk(ctx, item.ID, progress)
	if !watchedOnDisk {
		res.Action = "skipped"
		res.Reason = buildWatchingReason(progress, seasons, unwatchedSeasons)
		return res
	}

	moreEpisodesComing, ongoingReason := checkEmbyMoreEpisodesComing(progress, seasons)
	if moreEpisodesComing {
		if queue.IsQueued(embyID) {
			queue.Remove(embyID)
		}
		res.Action = "skipped"
		res.Reason = ongoingReason
		return res
	}

	watchedReason := "fully watched (via Emby)"

	queue.Add(&QueueItem{
		ID:         embyID,
		ExternalID: tvdbID,
		Title:      item.Name,
		MarkedAt:   time.Now(),
		Reason:     watchedReason,
	})

	res.Action = "queued"
	res.Reason = watchedReason + " - queued for deletion"
	res.DaysUntil = cfg.Cleanup.DelayDays
	return res
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

func (s *Service) processEmbySeriesRemovalQueue(ctx context.Context, result *ProcessingResult, queue *Queue, cfg *config.Config, dryRun bool) {
	ready := queue.GetReadyForRemoval(cfg.Cleanup.DelayDays)
	if len(ready) == 0 {
		return
	}

	logger.Infof("🗑️  %d Emby series ready for removal", len(ready))

	for _, item := range ready {
		embyID := strconv.Itoa(item.ID)

		if dryRun {
			logger.Warnf("🗑️  [DRY RUN] Would delete from Emby: %s", item.Title)
			result.AddResult(MediaResult{
				Type:   MediaTypeEmbySeries,
				Title:  item.Title,
				ID:     item.ID,
				Action: "dry_run_remove",
				Reason: "would be deleted",
			})
		} else {
			if err := s.emby.DeleteItem(ctx, embyID); err != nil {
				logger.Errorf("❌ Failed to delete %s from Emby: %v", item.Title, err)
				result.AddResult(MediaResult{
					Type:   MediaTypeEmbySeries,
					Title:  item.Title,
					ID:     item.ID,
					Action: "error",
					Reason: fmt.Sprintf("delete failed: %v", err),
				})
				continue
			}
			logger.Infof("✅ Deleted from Emby: %s", item.Title)
			result.AddResult(MediaResult{
				Type:   MediaTypeEmbySeries,
				Title:  item.Title,
				ID:     item.ID,
				Action: "removed",
				Reason: "deleted from Emby",
			})
		}

		queue.Remove(item.ID)
	}
}
