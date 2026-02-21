package cleanup

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/fusionn-air/internal/client/apprise"
	"github.com/fusionn-air/internal/client/emby"
	"github.com/fusionn-air/internal/client/radarr"
	"github.com/fusionn-air/internal/client/sonarr"
	"github.com/fusionn-air/internal/client/trakt"
	"github.com/fusionn-air/internal/config"
	"github.com/fusionn-air/pkg/logger"
)

// MediaType identifies the type of media being processed
type MediaType string

const (
	MediaTypeSeries     MediaType = "series"
	MediaTypeMovie      MediaType = "movie"
	MediaTypeEmbySeries MediaType = "emby_series"
	MediaTypeEmbyMovie  MediaType = "emby_movie"
)

// Service handles cleanup of fully watched media
type Service struct {
	sonarr  *sonarr.Client
	radarr  *radarr.Client
	emby    *emby.Client
	trakt   *trakt.Client
	apprise *apprise.Client

	cfgMgr *config.Manager
	queues map[MediaType]*Queue

	mu          sync.RWMutex
	lastRun     time.Time
	lastResults *ProcessingResult
}

// MediaResult holds the result for any media item (series, movie, etc.)
type MediaResult struct {
	Type       MediaType `json:"type"`
	Title      string    `json:"title"`
	ID         int       `json:"id"`
	Year       int       `json:"year,omitempty"` // For movies
	Action     string    `json:"action"`         // "queued", "removed", "skipped", "error"
	Reason     string    `json:"reason"`
	DaysUntil  int       `json:"days_until,omitempty"`
	SizeOnDisk string    `json:"size_on_disk,omitempty"`
}

// ProcessingResult holds the results of a cleanup run
type ProcessingResult struct {
	// Per-type stats
	Stats map[MediaType]*MediaStats `json:"stats"`

	// All results
	Results []MediaResult `json:"results"`

	// Total errors across all types
	Errors int `json:"errors"`
}

// MediaStats holds statistics for a single media type
type MediaStats struct {
	Scanned        int `json:"scanned"`
	MarkedForQueue int `json:"marked_for_queue"`
	Removed        int `json:"removed"`
	Skipped        int `json:"skipped"`
}

func NewService(sonarrClient *sonarr.Client, radarrClient *radarr.Client, embyClient *emby.Client, traktClient *trakt.Client, appriseClient *apprise.Client, cfgMgr *config.Manager) *Service {
	s := &Service{
		sonarr:  sonarrClient,
		radarr:  radarrClient,
		emby:    embyClient,
		trakt:   traktClient,
		apprise: appriseClient,
		cfgMgr:  cfgMgr,
		queues:  make(map[MediaType]*Queue),
	}

	s.queues[MediaTypeSeries] = NewQueueWithFile("data/cleanup_series_queue.json")
	s.queues[MediaTypeMovie] = NewQueueWithFile("data/cleanup_movie_queue.json")
	s.queues[MediaTypeEmbySeries] = NewQueueWithFile("data/cleanup_emby_series_queue.json")
	s.queues[MediaTypeEmbyMovie] = NewQueueWithFile("data/cleanup_emby_movie_queue.json")

	return s
}

// ProcessCleanup runs the cleanup logic for all media types
func (s *Service) ProcessCleanup(ctx context.Context) (*ProcessingResult, error) {
	// Get fresh config for this run (supports hot-reload)
	cfg := s.cfgMgr.Get()

	if !cfg.Cleanup.Enabled {
		logger.Debug("Cleanup is disabled, skipping")
		return nil, nil
	}

	startTime := time.Now()
	dryRun := cfg.Scheduler.DryRun

	logger.Info("")
	logger.Info("┌──────────────────────────────────────────────────────────────┐")
	logger.Info("│               CLEANUP PROCESSING STARTED                     │")
	logger.Info("└──────────────────────────────────────────────────────────────┘")

	if dryRun {
		logger.Warn("⚠️  DRY RUN MODE - No actual deletions will be made")
	}

	result := &ProcessingResult{
		Stats: make(map[MediaType]*MediaStats),
	}

	sonarrTvdbIDs := s.processSeries(ctx, result, cfg, dryRun)
	radarrTmdbIDs := s.processMovies(ctx, result, cfg, dryRun)

	if s.emby != nil && cfg.Emby.Enabled {
		libraries, excludedLibNames := s.resolveLibrariesAndExclusions(ctx, cfg)

		// Aggregate items by type from all libraries
		var allMovies []emby.Item
		var allSeries []emby.Item

		for _, lib := range libraries {
			if excludedLibNames[lib.Name] {
				logger.Infof("📚 Skipping excluded library %q (ID: %s)", lib.Name, lib.ItemID)
				continue
			}

			switch lib.CollectionType {
			case "movies":
				if radarrTmdbIDs == nil {
					logger.Warnf("⚠️  Skipping movie library %q - Radarr data unavailable", lib.Name)
					continue
				}
				movies, err := s.emby.GetMovies(ctx, lib.ItemID)
				if err != nil {
					logger.Errorf("❌ Failed to get movies from library %q: %v", lib.Name, err)
					continue
				}
				logger.Infof("🎬 Found %d movies in library %q", len(movies), lib.Name)
				allMovies = append(allMovies, movies...)

			case "tvshows":
				if sonarrTvdbIDs == nil {
					logger.Warnf("⚠️  Skipping TV library %q - Sonarr data unavailable", lib.Name)
					continue
				}
				series, err := s.emby.GetSeries(ctx, lib.ItemID)
				if err != nil {
					logger.Errorf("❌ Failed to get series from library %q: %v", lib.Name, err)
					continue
				}
				logger.Infof("📺 Found %d series in library %q", len(series), lib.Name)
				allSeries = append(allSeries, series...)

			default:
				if lib.CollectionType != "" {
					logger.Debugf("📚 Skipping library %q (unsupported type: %s)", lib.Name, lib.CollectionType)
				} else {
					logger.Debugf("📚 Skipping library %q (mixed content not supported)", lib.Name)
				}
			}
		}

		// Process aggregated items (fetches Trakt data once per type)
		if len(allMovies) > 0 {
			s.processEmbyMovieItems(ctx, result, cfg, dryRun, radarrTmdbIDs, allMovies)
		} else if radarrTmdbIDs != nil {
			logger.Info("🎬 No movies found in non-excluded movie libraries")
		}

		if len(allSeries) > 0 {
			s.processEmbySeriesItems(ctx, result, cfg, dryRun, sonarrTvdbIDs, allSeries)
		} else if sonarrTvdbIDs != nil {
			logger.Info("📺 No series found in non-excluded TV libraries")
		}
	}

	// Store results
	s.mu.Lock()
	s.lastRun = time.Now()
	s.lastResults = result
	s.mu.Unlock()

	// Print summary and send notification
	s.printSummary(result, startTime, dryRun)
	s.sendNotification(ctx, result, dryRun)

	return result, nil
}

// resolveLibrariesAndExclusions fetches Emby libraries and builds a map of excluded library names.
// Returns all libraries and a map of excluded names for filtering.
func (s *Service) resolveLibrariesAndExclusions(ctx context.Context, cfg *config.Config) ([]emby.VirtualFolder, map[string]bool) {
	libraries, err := s.emby.GetLibraries(ctx)
	if err != nil {
		logger.Warnf("⚠️  Failed to fetch Emby libraries: %v — proceeding without library filtering", err)
		return nil, nil
	}

	if len(cfg.Emby.ExcludedLibraries) == 0 {
		return libraries, nil
	}

	// Build map of excluded library names (case-insensitive)
	excludedNames := make(map[string]bool, len(cfg.Emby.ExcludedLibraries))
	for _, name := range cfg.Emby.ExcludedLibraries {
		excludedNames[name] = true
	}

	// Validate that excluded names exist and log exclusions
	libsByName := make(map[string]bool, len(libraries))
	for _, lib := range libraries {
		libsByName[lib.Name] = true
	}

	for _, name := range cfg.Emby.ExcludedLibraries {
		if !libsByName[name] {
			logger.Warnf("⚠️  Excluded library %q not found in Emby — check spelling", name)
		} else {
			logger.Infof("🚫 Excluding Emby library %q from cleanup", name)
		}
	}

	return libraries, excludedNames
}

// GetQueue returns the queue for a specific media type
func (s *Service) GetQueue(mediaType MediaType) *Queue {
	return s.queues[mediaType]
}

// GetAllQueues returns all queue items across all media types
func (s *Service) GetAllQueues() []*QueueItem {
	var all []*QueueItem
	for _, q := range s.queues {
		all = append(all, q.GetAll()...)
	}
	return all
}

// isExcluded checks if a title is in the exclusion list (shared across all types)
func isExcluded(title string, exclusions []string) bool {
	for _, exc := range exclusions {
		if strings.EqualFold(exc, title) {
			return true
		}
	}
	return false
}

// GetStats returns the current cleanup stats
func (s *Service) GetStats() *ProcessingResult {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.lastResults
}

// GetLastRun returns the last run time
func (s *Service) GetLastRun() time.Time {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.lastRun
}

// Helper to get or create stats for a media type
func (r *ProcessingResult) getStats(t MediaType) *MediaStats {
	if r.Stats[t] == nil {
		r.Stats[t] = &MediaStats{}
	}
	return r.Stats[t]
}

// AddResult adds a result and updates stats
func (r *ProcessingResult) AddResult(res MediaResult) {
	r.Results = append(r.Results, res)
	stats := r.getStats(res.Type)

	switch res.Action {
	case "queued":
		stats.MarkedForQueue++
	case "removed", "dry_run_remove":
		stats.Removed++
	case "skipped":
		stats.Skipped++
	case "error":
		r.Errors++
	}
}

// IncrementScanned increments the scanned count for a media type
func (r *ProcessingResult) IncrementScanned(t MediaType, count int) {
	r.getStats(t).Scanned = count
}
