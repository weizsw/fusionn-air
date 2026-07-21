package cleanup

import (
	"context"
	"fmt"

	"github.com/fusionn-air/internal/client/radarr"
	"github.com/fusionn-air/internal/client/trakt"
	"github.com/fusionn-air/internal/config"
	"github.com/fusionn-air/pkg/logger"
)

func (s *Service) processMovies(ctx context.Context, result *ProcessingResult, cfg *config.Config, dryRun bool) (radarrTmdbIDs map[int]bool, runErr error) {
	if s.radarr == nil {
		logger.Debug("Radarr client not configured, skipping movie cleanup")
		return nil, nil
	}

	queue := s.queues[MediaTypeMovie]
	if err := retryPendingUnmonitor(queue, result, MediaTypeMovie, dryRun, func(item *QueueItem) (bool, error) {
		return s.unmonitorMovie(ctx, item.ID, item.Title, queue)
	}); err != nil {
		return nil, err
	}
	logger.Info("🎬 Fetching movies from Radarr...")
	movies, err := s.radarr.GetAllMovies(ctx)
	if err != nil {
		logger.Errorf("❌ Failed to get movies from Radarr: %v", err)
		return nil, nil
	}

	radarrTmdbIDs = make(map[int]bool, len(movies))
	for _, movie := range movies {
		if movie.TmdbID > 0 {
			radarrTmdbIDs[movie.TmdbID] = true
		}
	}

	result.IncrementScanned(MediaTypeMovie, len(movies))
	logger.Infof("🎬 Found %d movies in Radarr", len(movies))
	logger.Info("👁️  Fetching movie watch history from Trakt...")
	watchedMovies, err := s.trakt.GetWatchedMovies(ctx)
	watchedAvailable := err == nil
	if err != nil {
		logger.Errorf("❌ Failed to get watched movies: %v", err)
	}

	watchedByTmdb := make(map[int]*trakt.WatchedMovie)
	for i := range watchedMovies {
		if watchedMovies[i].Movie.IDs.TMDB > 0 {
			watchedByTmdb[watchedMovies[i].Movie.IDs.TMDB] = &watchedMovies[i]
		}
	}

	for _, movie := range movies {
		existing := queue.Get(movie.ID) != nil
		if !existing && !watchedAvailable {
			continue
		}
		res, err := s.processOneMovie(&movie, watchedByTmdb, queue, cfg, dryRun)
		if !existing && !dryRun && err == nil && res.Action == "queued" && queue.NeedsUnmonitor(movie.ID) {
			unmonitored, persistErr := s.unmonitorMovie(ctx, movie.ID, movie.Title, queue)
			if persistErr != nil {
				res.Action = "error"
				res.Reason = persistErr.Error()
				err = persistErr
			} else if unmonitored {
				res.Reason = queue.Get(movie.ID).Reason + " - queued for deletion (unmonitored)"
			} else {
				res.Reason = queue.Get(movie.ID).Reason + " - pending unmonitor"
			}
		}
		if res.ID != 0 {
			result.AddResult(res)
		}
		if err != nil {
			return radarrTmdbIDs, fmt.Errorf("process movie %q: %w", movie.Title, err)
		}
	}

	if err := s.processMovieRemovalQueue(ctx, result, queue, cfg, dryRun); err != nil {
		return radarrTmdbIDs, err
	}
	return radarrTmdbIDs, nil
}

func (s *Service) processOneMovie(movie *radarr.Movie, watchedByTmdb map[int]*trakt.WatchedMovie, queue *Queue, cfg *config.Config, dryRun bool) (MediaResult, error) {
	res := MediaResult{
		Type:       MediaTypeMovie,
		Title:      movie.Title,
		ID:         movie.ID,
		Year:       movie.Year,
		SizeOnDisk: radarr.FormatSize(movie.SizeOnDisk),
	}

	if isExcluded(movie.Title, cfg.Cleanup.Exclusions) {
		res.Action = "skipped"
		res.Reason = "in exclusion list"
		return res, nil
	}
	if queued, found := existingCandidateResult(queue, movie.ID, cfg.Cleanup.DelayDays, res, " - queued for deletion (unmonitored)"); found {
		return queued, nil
	}
	if !movie.Monitored {
		res.Action = "skipped"
		res.Reason = "not monitored"
		return res, nil
	}
	if !movie.HasFile {
		res.Action = "skipped"
		if movie.Status == radarr.StatusAnnounced || movie.Status == radarr.StatusInCinemas {
			res.Reason = "not yet released"
		} else {
			res.Reason = "no file on disk"
		}
		return res, nil
	}

	watched, found := watchedByTmdb[movie.TmdbID]
	if !found {
		res.Action = "skipped"
		res.Reason = "not watched"
		return res, nil
	}
	watchedReason := fmt.Sprintf("watched %s", watched.LastWatchedAt.Format("2006-01-02"))
	return scheduleCandidate(queue, &QueueItem{
		ID:         movie.ID,
		ExternalID: movie.TmdbID,
		Title:      movie.Title,
		Reason:     watchedReason,
		SizeOnDisk: movie.SizeOnDisk,
	}, cfg.Cleanup.DelayDays, dryRun, res, " - queued for deletion (unmonitored)")
}

func (s *Service) processMovieRemovalQueue(ctx context.Context, result *ProcessingResult, queue *Queue, cfg *config.Config, dryRun bool) error {
	return processReadyCandidates(ctx, result, queue, cfg.Cleanup.DelayDays, cfg.Cleanup.Exclusions, dryRun, removalPlan{
		mediaType: MediaTypeMovie, label: "movies", removedReason: "deleted", formatSize: radarr.FormatSize,
		remove: func(ctx context.Context, item *QueueItem) (bool, error) {
			movie, err := s.radarr.GetMovie(ctx, item.ID)
			if err != nil || movie == nil {
				return false, err
			}
			return true, s.radarr.DeleteMovie(ctx, item.ID, true)
		},
	})
}

func (s *Service) unmonitorMovie(ctx context.Context, movieID int, title string, queue *Queue) (bool, error) {
	if err := s.radarr.UnmonitorMovie(ctx, movieID); err != nil {
		logger.Warnf("⚠️  Failed to unmonitor %s: %v", title, err)
		return false, nil
	}
	if err := queue.MarkUnmonitored(movieID); err != nil {
		return false, fmt.Errorf("unmonitored %q but queue persistence failed: %w", title, err)
	}
	logger.Infof("🔕 Unmonitored movie: %s (queued for deletion)", title)
	return true, nil
}
