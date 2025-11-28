package cleanup

import (
	"context"
	"fmt"
	"time"

	"github.com/fusionn-air/internal/client/radarr"
	"github.com/fusionn-air/internal/client/trakt"
	"github.com/fusionn-air/pkg/logger"
)

// processMovies handles movie cleanup using Radarr
func (s *Service) processMovies(ctx context.Context, result *ProcessingResult) {
	if s.radarr == nil {
		logger.Debug("Radarr client not configured, skipping movie cleanup")
		return
	}

	queue := s.queues[MediaTypeMovie]

	logger.Info("🎬 Fetching movies from Radarr...")
	movies, err := s.radarr.GetAllMovies(ctx)
	if err != nil {
		logger.Errorf("❌ Failed to get movies from Radarr: %v", err)
		return
	}

	result.IncrementScanned(MediaTypeMovie, len(movies))
	logger.Infof("🎬 Found %d movies in Radarr", len(movies))

	// Get watched movies from Trakt
	logger.Info("👁️  Fetching movie watch history from Trakt...")
	watchedMovies, err := s.trakt.GetWatchedMovies(ctx)
	if err != nil {
		logger.Errorf("❌ Failed to get watched movies: %v", err)
		return
	}

	// Build lookup by TMDB ID
	watchedByTmdb := make(map[int]*trakt.WatchedMovie)
	for i := range watchedMovies {
		if watchedMovies[i].Movie.IDs.TMDB > 0 {
			watchedByTmdb[watchedMovies[i].Movie.IDs.TMDB] = &watchedMovies[i]
		}
	}

	// Process each movie
	for _, movie := range movies {
		res := s.processOneMovie(&movie, watchedByTmdb, queue)
		result.AddResult(res)
	}

	// Process removal queue
	s.processMovieRemovalQueue(ctx, result, queue)
}

func (s *Service) processOneMovie(movie *radarr.Movie, watchedByTmdb map[int]*trakt.WatchedMovie, queue *Queue) MediaResult {
	res := MediaResult{
		Type:       MediaTypeMovie,
		Title:      movie.Title,
		ID:         movie.ID,
		Year:       movie.Year,
		SizeOnDisk: radarr.FormatSize(movie.SizeOnDisk),
	}

	// Check exclusions
	if s.isExcluded(movie.Title) {
		res.Action = "skipped"
		res.Reason = "in exclusion list"
		return res
	}

	// Check if movie is monitored
	if !movie.Monitored {
		res.Action = "skipped"
		res.Reason = "not monitored"
		return res
	}

	// Check if movie has a file
	if !movie.HasFile {
		res.Action = "skipped"
		if movie.Status == radarr.StatusAnnounced || movie.Status == radarr.StatusInCinemas {
			res.Reason = "not yet released"
		} else {
			res.Reason = "no file on disk"
		}
		return res
	}

	// Find in Trakt watched movies
	watched, found := watchedByTmdb[movie.TmdbID]
	if !found {
		res.Action = "skipped"
		res.Reason = "not watched"
		return res
	}

	// Movie is watched
	watchedReason := fmt.Sprintf("watched %s", watched.LastWatchedAt.Format("2006-01-02"))

	// Check if already queued
	if queue.IsQueued(movie.ID) {
		queueItem := queue.Get(movie.ID)
		daysInQueue := int(time.Since(queueItem.MarkedAt).Hours() / 24)
		daysUntil := s.cfg.DelayDays - daysInQueue
		if daysUntil < 0 {
			daysUntil = 0
		}
		res.Action = "queued"
		res.Reason = watchedReason
		res.DaysUntil = daysUntil
		return res
	}

	// Add to queue
	queue.Add(&QueueItem{
		ID:         movie.ID,
		ExternalID: movie.TmdbID,
		Title:      movie.Title,
		MarkedAt:   time.Now(),
		Reason:     watchedReason,
		SizeOnDisk: movie.SizeOnDisk,
	})

	res.Action = "queued"
	res.Reason = watchedReason + " - added to queue"
	res.DaysUntil = s.cfg.DelayDays
	return res
}

func (s *Service) processMovieRemovalQueue(ctx context.Context, result *ProcessingResult, queue *Queue) {
	ready := queue.GetReadyForRemoval(s.cfg.DelayDays)
	if len(ready) == 0 {
		return
	}

	logger.Infof("🗑️  %d movies ready for removal", len(ready))

	for _, item := range ready {
		movie, err := s.radarr.GetMovie(ctx, item.ID)
		if err != nil {
			logger.Errorf("❌ Error checking movie %s: %v", item.Title, err)
			continue
		}

		if movie == nil {
			logger.Infof("ℹ️  %s already removed, clearing from queue", item.Title)
			queue.Remove(item.ID)
			continue
		}

		if s.dryRun {
			logger.Warnf("🗑️  [DRY RUN] Would delete: %s (%s)", item.Title, radarr.FormatSize(item.SizeOnDisk))
			result.AddResult(MediaResult{
				Type:       MediaTypeMovie,
				Title:      item.Title,
				ID:         item.ID,
				Action:     "dry_run_remove",
				Reason:     "would be deleted",
				SizeOnDisk: radarr.FormatSize(item.SizeOnDisk),
			})
		} else {
			if err := s.radarr.DeleteMovie(ctx, item.ID, true); err != nil {
				logger.Errorf("❌ Failed to delete movie %s: %v", item.Title, err)
				result.AddResult(MediaResult{
					Type:   MediaTypeMovie,
					Title:  item.Title,
					ID:     item.ID,
					Action: "error",
					Reason: fmt.Sprintf("delete failed: %v", err),
				})
				continue
			}
			logger.Infof("✅ Deleted movie: %s (%s freed)", item.Title, radarr.FormatSize(item.SizeOnDisk))
			result.AddResult(MediaResult{
				Type:       MediaTypeMovie,
				Title:      item.Title,
				ID:         item.ID,
				Action:     "removed",
				Reason:     "deleted",
				SizeOnDisk: radarr.FormatSize(item.SizeOnDisk),
			})
		}

		queue.Remove(item.ID)
	}
}
