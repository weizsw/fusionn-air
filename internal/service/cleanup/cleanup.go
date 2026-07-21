package cleanup

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
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

var ErrAlreadyRunning = errors.New("cleanup is already running")

type sonarrAdapter interface {
	GetAllSeries(context.Context) ([]sonarr.Series, error)
	GetSeries(context.Context, int) (*sonarr.Series, error)
	DeleteSeries(context.Context, int, bool) error
	UnmonitorSeries(context.Context, int) error
}

type radarrAdapter interface {
	GetAllMovies(context.Context) ([]radarr.Movie, error)
	GetMovie(context.Context, int) (*radarr.Movie, error)
	DeleteMovie(context.Context, int, bool) error
	UnmonitorMovie(context.Context, int) error
}

type embyAdapter interface {
	GetLibraries(context.Context) ([]emby.VirtualFolder, error)
	GetMovies(context.Context, string) ([]emby.Item, error)
	GetSeries(context.Context, string) ([]emby.Item, error)
	GetSeasons(context.Context, string) ([]emby.Item, error)
	GetEpisodes(context.Context, string, string) ([]emby.Item, error)
	DeleteItem(context.Context, string) error
}

type traktAdapter interface {
	GetWatchedShows(context.Context) ([]trakt.WatchedShow, error)
	GetWatchedMovies(context.Context) ([]trakt.WatchedMovie, error)
	GetShowProgress(context.Context, int) (*trakt.ShowProgress, error)
	GetShowSeasons(context.Context, int) ([]trakt.SeasonSummary, error)
}

type notificationAdapter interface {
	IsEnabled() bool
	Notify(context.Context, string, string, string) error
}

type configSource interface {
	Get() *config.Config
}

// Service handles cleanup of fully watched media.
type Service struct {
	sonarr  sonarrAdapter
	radarr  radarrAdapter
	emby    embyAdapter
	trakt   traktAdapter
	apprise notificationAdapter
	cfgMgr  configSource
	queues  map[MediaType]*Queue

	runMu       sync.Mutex
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

func NewService(sonarrClient *sonarr.Client, radarrClient *radarr.Client, embyClient *emby.Client, traktClient *trakt.Client, appriseClient *apprise.Client, cfgMgr *config.Manager) (*Service, error) {
	var sonarrDep sonarrAdapter
	var radarrDep radarrAdapter
	var embyDep embyAdapter
	var appriseDep notificationAdapter
	if sonarrClient != nil {
		sonarrDep = sonarrClient
	}
	if radarrClient != nil {
		radarrDep = radarrClient
	}
	if embyClient != nil {
		embyDep = embyClient
	}
	if appriseClient != nil {
		appriseDep = appriseClient
	}
	return newService(sonarrDep, radarrDep, embyDep, traktClient, appriseDep, cfgMgr, "data")
}

func newService(sonarrClient sonarrAdapter, radarrClient radarrAdapter, embyClient embyAdapter, traktClient traktAdapter, appriseClient notificationAdapter, cfgMgr configSource, queueDir string) (*Service, error) {
	s := &Service{
		sonarr:  sonarrClient,
		radarr:  radarrClient,
		emby:    embyClient,
		trakt:   traktClient,
		apprise: appriseClient,
		cfgMgr:  cfgMgr,
		queues:  make(map[MediaType]*Queue),
	}

	queues := []struct {
		mediaType         MediaType
		fileName          string
		requiresUnmonitor bool
	}{
		{MediaTypeSeries, "cleanup_series_queue.json", true},
		{MediaTypeMovie, "cleanup_movie_queue.json", true},
		{MediaTypeEmbySeries, "cleanup_emby_series_queue.json", false},
		{MediaTypeEmbyMovie, "cleanup_emby_movie_queue.json", false},
	}
	for _, definition := range queues {
		queue, err := NewQueueWithFile(filepath.Join(queueDir, definition.fileName), definition.requiresUnmonitor)
		if err != nil {
			return nil, fmt.Errorf("initialize %s queue: %w", definition.mediaType, err)
		}
		s.queues[definition.mediaType] = queue
	}
	return s, nil
}

// ProcessCleanup runs the cleanup logic for all media types
func (s *Service) ProcessCleanup(ctx context.Context) (*ProcessingResult, error) {
	if !s.runMu.TryLock() {
		return nil, ErrAlreadyRunning
	}
	defer s.runMu.Unlock()

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

	result := &ProcessingResult{Stats: make(map[MediaType]*MediaStats)}
	defer func() {
		s.mu.Lock()
		s.lastRun = time.Now()
		s.lastResults = result
		s.mu.Unlock()
		s.printSummary(result, startTime, dryRun)
		s.sendNotification(ctx, result, dryRun)
	}()

	sonarrTvdbIDs, err := s.processSeries(ctx, result, cfg, dryRun)
	if err != nil {
		return result, err
	}
	radarrTmdbIDs, err := s.processMovies(ctx, result, cfg, dryRun)
	if err != nil {
		return result, err
	}

	if s.emby != nil && cfg.Emby.Enabled {
		libraries, excludedLibNames := s.resolveLibrariesAndExclusions(ctx, cfg)

		// Aggregate items by type from all libraries
		var allMovies []emby.Item
		var allSeries []emby.Item

		for _, lib := range libraries {
			if excludedLibNames[strings.ToLower(lib.Name)] {
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

		// Process new orphans and drain durable queues independently of discovery.
		if err := s.processEmbyMovieItems(ctx, result, cfg, dryRun, radarrTmdbIDs, allMovies); err != nil {
			return result, err
		}
		if len(allMovies) == 0 && radarrTmdbIDs != nil {
			logger.Info("🎬 No movies found in non-excluded movie libraries")
		}

		if err := s.processEmbySeriesItems(ctx, result, cfg, dryRun, sonarrTvdbIDs, allSeries); err != nil {
			return result, err
		}
		if len(allSeries) == 0 && sonarrTvdbIDs != nil {
			logger.Info("📺 No series found in non-excluded TV libraries")
		}
	}

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
		return libraries, make(map[string]bool)
	}

	// Build map of excluded library names (case-insensitive)
	excludedNames := make(map[string]bool, len(cfg.Emby.ExcludedLibraries))
	for _, name := range cfg.Emby.ExcludedLibraries {
		excludedNames[strings.ToLower(name)] = true
	}

	// Validate that excluded names exist and log exclusions
	libsByName := make(map[string]bool, len(libraries))
	for _, lib := range libraries {
		libsByName[strings.ToLower(lib.Name)] = true
	}

	for _, name := range cfg.Emby.ExcludedLibraries {
		if !libsByName[strings.ToLower(name)] {
			logger.Warnf("⚠️  Excluded library %q not found in Emby — check spelling", name)
		} else {
			logger.Infof("🚫 Excluding Emby library %q from cleanup", name)
		}
	}

	return libraries, excludedNames
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
