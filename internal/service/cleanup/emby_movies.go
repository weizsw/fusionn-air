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

func (s *Service) processEmbyMovieItems(ctx context.Context, result *ProcessingResult, cfg *config.Config, dryRun bool, radarrTmdbIDs map[int]bool, movies []emby.Item) error {
	if s.emby == nil {
		return nil
	}
	queue := s.queues[MediaTypeEmbyMovie]
	if radarrTmdbIDs == nil || len(movies) == 0 {
		return s.processEmbyMovieRemovalQueue(ctx, result, queue, cfg, dryRun)
	}
	logger.Infof("🎬 Total movies fetched: %d", len(movies))

	var orphans []emby.Item
	for _, item := range movies {
		tmdbID := emby.ParseProviderID(item.ProviderIDs, "Tmdb")
		if tmdbID == 0 {
			logger.Warnf("Skipping Emby movie %q (no TMDB ID)", item.Name)
			continue
		}
		if !radarrTmdbIDs[tmdbID] {
			orphans = append(orphans, item)
		}
	}
	result.IncrementScanned(MediaTypeEmbyMovie, len(orphans))
	logger.Infof("🎬 Found %d orphan movies in Emby (not in Radarr)", len(orphans))
	if len(orphans) == 0 {
		return s.processEmbyMovieRemovalQueue(ctx, result, queue, cfg, dryRun)
	}

	logger.Info("👁️  Fetching movie watch history from Trakt...")
	watchedMovies, err := s.trakt.GetWatchedMovies(ctx)
	if err != nil {
		logger.Errorf("❌ Failed to get watched movies: %v", err)
		return s.processEmbyMovieRemovalQueue(ctx, result, queue, cfg, dryRun)
	}
	watchedByTmdb := make(map[int]*trakt.WatchedMovie)
	for i := range watchedMovies {
		if watchedMovies[i].Movie.IDs.TMDB > 0 {
			watchedByTmdb[watchedMovies[i].Movie.IDs.TMDB] = &watchedMovies[i]
		}
	}

	for _, item := range orphans {
		res, err := s.processOneEmbyMovie(item, watchedByTmdb, queue, cfg, dryRun)
		if res.ID != 0 {
			result.AddResult(res)
		}
		if err != nil {
			return fmt.Errorf("process Emby movie %q: %w", item.Name, err)
		}
	}
	return s.processEmbyMovieRemovalQueue(ctx, result, queue, cfg, dryRun)
}

func (s *Service) processOneEmbyMovie(item emby.Item, watchedByTmdb map[int]*trakt.WatchedMovie, queue *Queue, cfg *config.Config, dryRun bool) (MediaResult, error) {
	embyID, _ := strconv.Atoi(item.ID)
	if embyID == 0 {
		logger.Warnf("Skipping Emby movie %q (invalid ID: %s)", item.Name, item.ID)
		return MediaResult{}, nil
	}
	tmdbID := emby.ParseProviderID(item.ProviderIDs, "Tmdb")
	res := MediaResult{Type: MediaTypeEmbyMovie, Title: item.Name, ID: embyID}
	if isExcluded(item.Name, cfg.Cleanup.Exclusions) {
		res.Action, res.Reason = "skipped", "in exclusion list"
		return res, nil
	}
	if queued, found := existingCandidateResult(queue, embyID, cfg.Cleanup.DelayDays, res, " - queued for deletion"); found {
		return queued, nil
	}
	watched, found := watchedByTmdb[tmdbID]
	if !found {
		res.Action, res.Reason = "skipped", "not watched"
		return res, nil
	}

	watchedReason := fmt.Sprintf("watched %s (via Emby)", watched.LastWatchedAt.Format("2006-01-02"))
	return scheduleCandidate(queue, &QueueItem{ID: embyID, ExternalID: tmdbID, Title: item.Name, Reason: watchedReason}, cfg.Cleanup.DelayDays, dryRun, res, " - queued for deletion")
}

func (s *Service) processEmbyMovieRemovalQueue(ctx context.Context, result *ProcessingResult, queue *Queue, cfg *config.Config, dryRun bool) error {
	return processReadyCandidates(ctx, result, queue, cfg.Cleanup.DelayDays, cfg.Cleanup.Exclusions, dryRun, removalPlan{
		mediaType: MediaTypeEmbyMovie, label: "Emby movies", removedReason: "deleted from Emby",
		remove: func(ctx context.Context, item *QueueItem) (bool, error) {
			return true, s.emby.DeleteItem(ctx, strconv.Itoa(item.ID))
		},
	})
}
