package cleanup

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/fusionn-air/internal/client/radarr"
	"github.com/fusionn-air/internal/client/trakt"
	"github.com/fusionn-air/internal/config"
	"github.com/fusionn-air/pkg/logger"
)

// processMovies handles movie cleanup using Radarr
func (s *Service) processMovies(ctx context.Context, result *ProcessingResult, cfg *config.Config, dryRun bool) {
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
		res := s.processOneMovie(&movie, watchedByTmdb, queue, cfg)
		// Unmonitor if newly queued
		if res.Action == "queued" && strings.HasSuffix(res.Reason, "added to queue") {
			s.unmonitorMovie(ctx, movie.ID, movie.Title, queue, dryRun)
		}
		result.AddResult(res)
	}

	// Process removal queue
	s.processMovieRemovalQueue(ctx, result, queue, cfg, dryRun)
}

func (s *Service) processOneMovie(movie *radarr.Movie, watchedByTmdb map[int]*trakt.WatchedMovie, queue *Queue, cfg *config.Config) MediaResult {
	res := MediaResult{
		Type:       MediaTypeMovie,
		Title:      movie.Title,
		ID:         movie.ID,
		Year:       movie.Year,
		SizeOnDisk: radarr.FormatSize(movie.SizeOnDisk),
	}

	// Check exclusions
	if isExcluded(movie.Title, cfg.Cleanup.Exclusions) {
		res.Action = "skipped"
		res.Reason = "in exclusion list"
		return res
	}

	// Check if already in queue (before checking monitored status)
	// This ensures queued items remain visible even after being unmonitored
	if queue.IsQueued(movie.ID) {
		queueItem := queue.Get(movie.ID)
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
	res.Reason = watchedReason + " - queued for deletion (unmonitored)"
	res.DaysUntil = cfg.Cleanup.DelayDays
	return res
}

func (s *Service) processMovieRemovalQueue(ctx context.Context, result *ProcessingResult, queue *Queue, cfg *config.Config, dryRun bool) {
	ready := queue.GetReadyForRemoval(cfg.Cleanup.DelayDays)
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

		if dryRun {
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

// unmonitorMovie unmonitors a movie in Radarr when it's added to the cleanup queue
func (s *Service) unmonitorMovie(ctx context.Context, movieID int, title string, queue *Queue, dryRun bool) {
	if dryRun {
		logger.Warnf("🔕 [DRY RUN] Would unmonitor movie: %s (queued for deletion)", title)
		return
	}

	if err := s.radarr.UnmonitorMovie(ctx, movieID); err != nil {
		logger.Warnf("⚠️  Failed to unmonitor %s: %v", title, err)
		return
	}

	logger.Infof("🔕 Unmonitored movie: %s (queued for deletion)", title)
	queue.MarkUnmonitored(movieID)
}
