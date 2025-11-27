package watcher

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/fusionn-air/internal/client/overseerr"
	"github.com/fusionn-air/internal/client/trakt"
	"github.com/fusionn-air/internal/config"
	"github.com/fusionn-air/pkg/logger"
)

// Service handles the core logic of checking calendar and requesting shows
type Service struct {
	trakt     *trakt.Client
	overseerr *overseerr.Client
	cfg       config.SchedulerConfig

	mu          sync.RWMutex
	lastRun     time.Time
	lastResults []ProcessResult
}

// ProcessResult holds the result of processing a single calendar item
type ProcessResult struct {
	ShowTitle string    `json:"show_title"`
	ShowTMDB  int       `json:"show_tmdb"`
	Season    int       `json:"season"`
	Episode   int       `json:"episode"`
	AirDate   time.Time `json:"air_date"`
	Action    string    `json:"action"` // "requested", "skipped", "error", "already_requested", "dry_run"
	Reason    string    `json:"reason,omitempty"`
	Error     string    `json:"error,omitempty"`
}

func NewService(traktClient *trakt.Client, overseerrClient *overseerr.Client, cfg config.SchedulerConfig) *Service {
	return &Service{
		trakt:     traktClient,
		overseerr: overseerrClient,
		cfg:       cfg,
	}
}

// ProcessCalendar checks the calendar and requests new seasons as needed
func (s *Service) ProcessCalendar(ctx context.Context) ([]ProcessResult, error) {
	startTime := time.Now()
	logger.Info("[watcher] ========================================")
	logger.Info("[watcher] Starting calendar processing")
	logger.Info("[watcher] ========================================")
	if s.cfg.DryRun {
		logger.Warn("[watcher] DRY RUN MODE - no actual requests will be made")
	}

	// Get upcoming shows from Trakt calendar
	calendarItems, err := s.trakt.GetMyShowsCalendar(ctx, s.cfg.CalendarDays)
	if err != nil {
		logger.Errorf("[watcher] Failed to get calendar: %v", err)
		return nil, fmt.Errorf("getting calendar: %w", err)
	}

	if len(calendarItems) == 0 {
		logger.Info("[watcher] No upcoming shows in calendar")
		return nil, nil
	}

	// Group by show to avoid duplicate processing
	showSeasons := s.groupByShowAndSeason(calendarItems)
	logger.Infof("[watcher] Found %d shows with upcoming episodes", len(showSeasons))

	var results []ProcessResult

	// Process each show/season silently
	for _, item := range showSeasons {
		result := s.processShow(ctx, item)
		results = append(results, result)
	}

	// Store results
	s.mu.Lock()
	s.lastRun = time.Now()
	s.lastResults = results
	s.mu.Unlock()

	// Print summary
	s.printSummary(results, startTime)

	return results, nil
}

// printSummary prints a grouped summary of results
func (s *Service) printSummary(results []ProcessResult, startTime time.Time) {
	var willRequest []string
	var willSkip []string
	var errors []string

	for _, r := range results {
		showInfo := fmt.Sprintf("%s S%02d", r.ShowTitle, r.Season)
		switch r.Action {
		case "requested", "dry_run":
			willRequest = append(willRequest, fmt.Sprintf("  • %s (%s)", showInfo, r.Reason))
		case "skipped", "already_requested":
			willSkip = append(willSkip, fmt.Sprintf("  • %s (%s)", showInfo, r.Reason))
		case "error":
			errors = append(errors, fmt.Sprintf("  • %s (%s)", showInfo, r.Error))
		}
	}

	logger.Info("[watcher] ========================================")
	logger.Info("[watcher] SUMMARY")
	logger.Info("[watcher] ========================================")

	if len(willRequest) > 0 {
		if s.cfg.DryRun {
			logger.Warnf("[watcher] WOULD REQUEST (%d):", len(willRequest))
		} else {
			logger.Infof("[watcher] REQUESTED (%d):", len(willRequest))
		}
		logger.Info(strings.Join(willRequest, "\n"))
	}

	if len(willSkip) > 0 {
		logger.Infof("[watcher] SKIPPED (%d):", len(willSkip))
		logger.Info(strings.Join(willSkip, "\n"))
	}

	if len(errors) > 0 {
		logger.Errorf("[watcher] ERRORS (%d):", len(errors))
		logger.Error(strings.Join(errors, "\n"))
	}

	logger.Info("[watcher] ----------------------------------------")
	logger.Infof("[watcher] Completed in %v", time.Since(startTime).Round(time.Millisecond))
	logger.Info("[watcher] ========================================")
}

type calendarItem struct {
	show    trakt.Show
	season  int
	episode int
	airDate time.Time
}

func (s *Service) groupByShowAndSeason(items []trakt.CalendarShow) map[string]calendarItem {
	result := make(map[string]calendarItem)

	for _, item := range items {
		key := fmt.Sprintf("%d-%d", item.Show.IDs.TMDB, item.Episode.Season)
		if _, exists := result[key]; !exists {
			result[key] = calendarItem{
				show:    item.Show,
				season:  item.Episode.Season,
				episode: item.Episode.Number,
				airDate: item.FirstAired,
			}
		}
	}

	return result
}

func (s *Service) processShow(ctx context.Context, item calendarItem) ProcessResult {
	result := ProcessResult{
		ShowTitle: item.show.Title,
		ShowTMDB:  item.show.IDs.TMDB,
		Season:    item.season,
		Episode:   item.episode,
		AirDate:   item.airDate,
	}

	// Skip if no TMDB ID (can't request without it)
	if item.show.IDs.TMDB == 0 {
		result.Action = "skipped"
		result.Reason = "no TMDB ID"
		return result
	}

	// Get watch progress from Trakt
	progress, err := s.trakt.GetShowProgress(ctx, item.show.IDs.Trakt)
	if err != nil {
		result.Action = "error"
		result.Error = fmt.Sprintf("failed to get progress: %v", err)
		return result
	}

	// Determine if we should request this season
	shouldRequest, reason := s.shouldRequestSeason(progress, item.season)
	if !shouldRequest {
		result.Action = "skipped"
		result.Reason = reason
		return result
	}

	// In dry-run mode, skip Overseerr entirely
	if s.cfg.DryRun {
		result.Action = "dry_run"
		result.Reason = reason
		return result
	}

	// Check Overseerr if already requested/available
	tvDetails, err := s.overseerr.GetTVByTMDB(ctx, item.show.IDs.TMDB)
	if err != nil {
		result.Action = "error"
		result.Error = fmt.Sprintf("Overseerr error: %v", err)
		return result
	}

	if s.overseerr.IsSeasonRequested(tvDetails, item.season) {
		result.Action = "already_requested"
		result.Reason = "already in Overseerr"
		return result
	}

	// Request the season
	_, err = s.overseerr.RequestTV(ctx, item.show.IDs.TMDB, []int{item.season})
	if err != nil {
		result.Action = "error"
		result.Error = fmt.Sprintf("request failed: %v", err)
		return result
	}

	result.Action = "requested"
	result.Reason = reason
	return result
}

// shouldRequestSeason determines if a season should be requested based on watch progress
func (s *Service) shouldRequestSeason(progress *trakt.ShowProgress, targetSeason int) (bool, string) {
	// Find the target season in progress
	var targetSeasonProgress *trakt.SeasonProgress
	for i := range progress.Seasons {
		if progress.Seasons[i].Number == targetSeason {
			targetSeasonProgress = &progress.Seasons[i]
			break
		}
	}

	// If user has already watched any episodes of target season, it's already available
	if targetSeasonProgress != nil && targetSeasonProgress.Completed > 0 {
		return false, fmt.Sprintf("already watching S%02d (%d/%d eps)",
			targetSeason, targetSeasonProgress.Completed, targetSeasonProgress.Aired)
	}

	// For season 1
	if targetSeason == 1 {
		// If target season exists in progress but 0 completed, user might have it but not started
		// This means S01 is likely already available
		if targetSeasonProgress != nil {
			return false, "S01 already available (0 watched)"
		}

		// No S01 in progress - check if they've only watched specials (S00)
		for _, sp := range progress.Seasons {
			if sp.Number == 0 && sp.Completed > 0 {
				return true, "watched specials only, need S01"
			}
		}

		// No watch history at all for this show
		return false, "no watch history"
	}

	// For season 2+, check if previous season is complete
	prevSeason := targetSeason - 1
	var prevSeasonProgress *trakt.SeasonProgress
	for i := range progress.Seasons {
		if progress.Seasons[i].Number == prevSeason {
			prevSeasonProgress = &progress.Seasons[i]
			break
		}
	}

	if prevSeasonProgress == nil {
		return false, fmt.Sprintf("S%02d not watched", prevSeason)
	}

	if prevSeasonProgress.Aired == 0 {
		return false, fmt.Sprintf("S%02d not aired yet", prevSeason)
	}

	if prevSeasonProgress.Completed < prevSeasonProgress.Aired {
		pct := float64(prevSeasonProgress.Completed) / float64(prevSeasonProgress.Aired) * 100
		return false, fmt.Sprintf("S%02d incomplete (%d/%d = %.0f%%)",
			prevSeason, prevSeasonProgress.Completed, prevSeasonProgress.Aired, pct)
	}

	return true, fmt.Sprintf("S%02d complete", prevSeason)
}

// GetLastRun returns the last run time and results
func (s *Service) GetLastRun() (time.Time, []ProcessResult) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.lastRun, s.lastResults
}

// Stats returns processing statistics
type Stats struct {
	LastRun    time.Time       `json:"last_run"`
	TotalShows int             `json:"total_shows"`
	Requested  int             `json:"requested"`
	Skipped    int             `json:"skipped"`
	Errors     int             `json:"errors"`
	Results    []ProcessResult `json:"results,omitempty"`
}

func (s *Service) GetStats() Stats {
	s.mu.RLock()
	defer s.mu.RUnlock()

	stats := Stats{
		LastRun:    s.lastRun,
		TotalShows: len(s.lastResults),
		Results:    s.lastResults,
	}

	for _, r := range s.lastResults {
		switch r.Action {
		case "requested", "dry_run":
			stats.Requested++
		case "skipped", "already_requested":
			stats.Skipped++
		case "error":
			stats.Errors++
		}
	}

	return stats
}
