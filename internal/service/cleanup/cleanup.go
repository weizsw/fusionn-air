package cleanup

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/fusionn-air/internal/client/apprise"
	"github.com/fusionn-air/internal/client/radarr"
	"github.com/fusionn-air/internal/client/sonarr"
	"github.com/fusionn-air/internal/client/trakt"
	"github.com/fusionn-air/internal/config"
	"github.com/fusionn-air/pkg/logger"
)

// MediaType identifies the type of media being processed
type MediaType string

const (
	MediaTypeSeries MediaType = "series"
	MediaTypeMovie  MediaType = "movie"
	// Add new types here: MediaTypeAudiobook, MediaTypeMusic, etc.
)

// Service handles cleanup of fully watched media
type Service struct {
	// Clients - add new clients here
	sonarr  *sonarr.Client
	radarr  *radarr.Client
	trakt   *trakt.Client
	apprise *apprise.Client

	cfg    config.CleanupConfig
	dryRun bool

	// Queues per media type - extensible
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

func NewService(sonarrClient *sonarr.Client, radarrClient *radarr.Client, traktClient *trakt.Client, appriseClient *apprise.Client, cfg config.CleanupConfig, dryRun bool) *Service {
	s := &Service{
		sonarr:  sonarrClient,
		radarr:  radarrClient,
		trakt:   traktClient,
		apprise: appriseClient,
		cfg:     cfg,
		dryRun:  dryRun,
		queues:  make(map[MediaType]*Queue),
	}

	// Register queues for each media type
	s.queues[MediaTypeSeries] = NewQueueWithFile("data/cleanup_series_queue.json")
	s.queues[MediaTypeMovie] = NewQueueWithFile("data/cleanup_movie_queue.json")
	// Add new queues here: s.queues[MediaTypeAudiobook] = NewQueueWithFile("data/cleanup_audiobook_queue.json")

	return s
}

// ProcessCleanup runs the cleanup logic for all media types
func (s *Service) ProcessCleanup(ctx context.Context) (*ProcessingResult, error) {
	if !s.cfg.Enabled {
		logger.Debug("Cleanup is disabled, skipping")
		return nil, nil
	}

	startTime := time.Now()

	logger.Info("")
	logger.Info("┌──────────────────────────────────────────────────────────────┐")
	logger.Info("│               CLEANUP PROCESSING STARTED                     │")
	logger.Info("└──────────────────────────────────────────────────────────────┘")

	if s.dryRun {
		logger.Warn("⚠️  DRY RUN MODE - No actual deletions will be made")
	}

	result := &ProcessingResult{
		Stats: make(map[MediaType]*MediaStats),
	}

	// Process each media type - add new processors here
	s.processSeries(ctx, result)
	s.processMovies(ctx, result)
	// Add new processors: s.processAudiobooks(ctx, result)

	// Store results
	s.mu.Lock()
	s.lastRun = time.Now()
	s.lastResults = result
	s.mu.Unlock()

	// Print summary and send notification
	s.printSummary(result, startTime)
	s.sendNotification(ctx, result)

	return result, nil
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
func (s *Service) isExcluded(title string) bool {
	for _, exc := range s.cfg.Exclusions {
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
