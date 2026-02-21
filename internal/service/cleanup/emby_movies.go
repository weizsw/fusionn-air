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

func (s *Service) processEmbyMovieItems(ctx context.Context, result *ProcessingResult, cfg *config.Config, dryRun bool, radarrTmdbIDs map[int]bool, movies []emby.Item) {
	if s.emby == nil {
		return
	}

	queue := s.queues[MediaTypeEmbyMovie]

	if len(movies) == 0 {
		logger.Info("🎬 No movies found in non-excluded libraries")
		return
	}

	logger.Infof("🎬 Total movies fetched: %d", len(movies))

	var orphans []emby.Item
	for _, item := range movies {
		tmdbID := emby.ParseProviderID(item.ProviderIDs, "Tmdb")
		if tmdbID == 0 {
			logger.Warnf("Skipping Emby movie %q (no TMDB ID)", item.Name)
			continue
		}
		if radarrTmdbIDs[tmdbID] {
			continue
		}
		orphans = append(orphans, item)
	}

	result.IncrementScanned(MediaTypeEmbyMovie, len(orphans))
	logger.Infof("🎬 Found %d orphan movies in Emby (not in Radarr)", len(orphans))

	if len(orphans) == 0 {
		return
	}

	logger.Info("👁️  Fetching movie watch history from Trakt...")
	watchedMovies, err := s.trakt.GetWatchedMovies(ctx)
	if err != nil {
		logger.Errorf("❌ Failed to get watched movies: %v", err)
		return
	}

	watchedByTmdb := make(map[int]*trakt.WatchedMovie)
	for i := range watchedMovies {
		if watchedMovies[i].Movie.IDs.TMDB > 0 {
			watchedByTmdb[watchedMovies[i].Movie.IDs.TMDB] = &watchedMovies[i]
		}
	}

	for _, item := range orphans {
		res := s.processOneEmbyMovie(item, watchedByTmdb, queue, cfg)
		if res.ID != 0 {
			result.AddResult(res)
		}
	}

	s.processEmbyMovieRemovalQueue(ctx, result, queue, cfg, dryRun)
}

func (s *Service) processOneEmbyMovie(item emby.Item, watchedByTmdb map[int]*trakt.WatchedMovie, queue *Queue, cfg *config.Config) MediaResult {
	embyID, err := strconv.Atoi(item.ID)
	if err != nil || embyID == 0 {
		logger.Warnf("Skipping Emby movie %q (invalid ID: %s)", item.Name, item.ID)
		return MediaResult{}
	}
	tmdbID := emby.ParseProviderID(item.ProviderIDs, "Tmdb")

	res := MediaResult{
		Type:  MediaTypeEmbyMovie,
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

	watched, found := watchedByTmdb[tmdbID]
	if !found {
		res.Action = "skipped"
		res.Reason = "not watched"
		return res
	}

	watchedReason := fmt.Sprintf("watched %s (via Emby)", watched.LastWatchedAt.Format("2006-01-02"))

	queue.Add(&QueueItem{
		ID:         embyID,
		ExternalID: tmdbID,
		Title:      item.Name,
		MarkedAt:   time.Now(),
		Reason:     watchedReason,
	})

	res.Action = "queued"
	res.Reason = watchedReason + " - queued for deletion"
	res.DaysUntil = cfg.Cleanup.DelayDays
	return res
}

func (s *Service) processEmbyMovieRemovalQueue(ctx context.Context, result *ProcessingResult, queue *Queue, cfg *config.Config, dryRun bool) {
	ready := queue.GetReadyForRemoval(cfg.Cleanup.DelayDays)
	if len(ready) == 0 {
		return
	}

	logger.Infof("🗑️  %d Emby movies ready for removal", len(ready))

	for _, item := range ready {
		embyID := strconv.Itoa(item.ID)

		if dryRun {
			logger.Warnf("🗑️  [DRY RUN] Would delete from Emby: %s", item.Title)
			result.AddResult(MediaResult{
				Type:   MediaTypeEmbyMovie,
				Title:  item.Title,
				ID:     item.ID,
				Action: "dry_run_remove",
				Reason: "would be deleted",
			})
		} else {
			if err := s.emby.DeleteItem(ctx, embyID); err != nil {
				logger.Errorf("❌ Failed to delete movie %s from Emby: %v", item.Title, err)
				result.AddResult(MediaResult{
					Type:   MediaTypeEmbyMovie,
					Title:  item.Title,
					ID:     item.ID,
					Action: "error",
					Reason: fmt.Sprintf("delete failed: %v", err),
				})
				continue
			}
			logger.Infof("✅ Deleted movie from Emby: %s", item.Title)
			result.AddResult(MediaResult{
				Type:   MediaTypeEmbyMovie,
				Title:  item.Title,
				ID:     item.ID,
				Action: "removed",
				Reason: "deleted from Emby",
			})
		}

		queue.Remove(item.ID)
	}
}
