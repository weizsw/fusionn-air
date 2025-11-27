package watcher

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/fusionn-air/internal/client/apprise"
	"github.com/fusionn-air/internal/client/overseerr"
	"github.com/fusionn-air/internal/client/trakt"
	"github.com/fusionn-air/internal/config"
	"github.com/fusionn-air/pkg/logger"
)

// Service handles the core logic of checking calendar and requesting shows
type Service struct {
	trakt     *trakt.Client
	overseerr *overseerr.Client
	apprise   *apprise.Client
	cfg       config.WatcherConfig
	dryRun    bool

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

func NewService(traktClient *trakt.Client, overseerrClient *overseerr.Client, appriseClient *apprise.Client, cfg config.WatcherConfig, dryRun bool) *Service {
	return &Service{
		trakt:     traktClient,
		overseerr: overseerrClient,
		apprise:   appriseClient,
		cfg:       cfg,
		dryRun:    dryRun,
	}
}

// ProcessCalendar checks the calendar and requests new seasons as needed
func (s *Service) ProcessCalendar(ctx context.Context) ([]ProcessResult, error) {
	startTime := time.Now()

	logger.Info("")
	logger.Info("╔══════════════════════════════════════════════════════════════╗")
	logger.Info("║              CALENDAR PROCESSING STARTED                     ║")
	logger.Info("╚══════════════════════════════════════════════════════════════╝")

	if s.dryRun {
		logger.Warn("⚠️  DRY RUN MODE - No actual requests will be made")
	}

	// Get upcoming shows from Trakt calendar
	logger.Infof("📅 Fetching calendar for next %d days...", s.cfg.CalendarDays)
	calendarItems, err := s.trakt.GetMyShowsCalendar(ctx, s.cfg.CalendarDays)
	if err != nil {
		logger.Errorf("❌ Failed to get calendar: %v", err)
		return nil, fmt.Errorf("getting calendar: %w", err)
	}

	if len(calendarItems) == 0 {
		logger.Info("📭 No upcoming shows in calendar")
		return nil, nil
	}

	// Group by show to avoid duplicate processing
	showSeasons := s.groupByShowAndSeason(calendarItems)
	logger.Infof("📺 Found %d shows with upcoming episodes", len(showSeasons))
	logger.Info("")

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

	// Send notification
	s.sendNotification(ctx, results)

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
			willRequest = append(willRequest, fmt.Sprintf("   ✅ %-35s  ← %s", showInfo, r.Reason))
		case "skipped", "already_requested":
			willSkip = append(willSkip, fmt.Sprintf("   ⏭️  %-35s  ← %s", showInfo, r.Reason))
		case "error":
			errors = append(errors, fmt.Sprintf("   ❌ %-35s  ← %s", showInfo, r.Error))
		}
	}

	logger.Info("┌──────────────────────────────────────────────────────────────┐")
	logger.Info("│                         RESULTS                              │")
	logger.Info("└──────────────────────────────────────────────────────────────┘")

	if len(willRequest) > 0 {
		logger.Info("")
		if s.dryRun {
			logger.Warnf("📥 WOULD REQUEST (%d):", len(willRequest))
		} else {
			logger.Infof("📥 REQUESTED (%d):", len(willRequest))
		}
		for _, line := range willRequest {
			if s.dryRun {
				logger.Warn(line)
			} else {
				logger.Info(line)
			}
		}
	}

	if len(willSkip) > 0 {
		logger.Info("")
		logger.Infof("⏭️  SKIPPED (%d):", len(willSkip))
		for _, line := range willSkip {
			logger.Info(line)
		}
	}

	if len(errors) > 0 {
		logger.Info("")
		logger.Errorf("❌ ERRORS (%d):", len(errors))
		for _, line := range errors {
			logger.Error(line)
		}
	}

	logger.Info("")
	logger.Info("────────────────────────────────────────────────────────────────")
	logger.Infof("⏱️  Completed in %v", time.Since(startTime).Round(time.Millisecond))
	logger.Info("")
}

// sendNotification sends a notification with watcher results
func (s *Service) sendNotification(ctx context.Context, results []ProcessResult) {
	if s.apprise == nil || !s.apprise.IsEnabled() {
		return
	}

	// Count results
	var requested, skipped, errCount int
	for _, r := range results {
		switch r.Action {
		case "requested", "dry_run":
			requested++
		case "skipped", "already_requested":
			skipped++
		case "error":
			errCount++
		}
	}

	// Build notification
	logger.Info("🔔 Sending notification...")
	formatter := &apprise.SlackFormatter{}
	var details []apprise.WatcherDetail
	for _, r := range results {
		details = append(details, apprise.WatcherDetail{
			ShowTitle: r.ShowTitle,
			Season:    r.Season,
			Action:    r.Action,
			Reason:    r.Reason,
		})
	}

	title := "📺 Watcher Results"
	if s.dryRun {
		title = "📺 Watcher Results (DRY RUN)"
	}

	body := formatter.FormatWatcherResults(requested, skipped, errCount, details)

	notifyType := "info"
	if requested > 0 {
		notifyType = "success"
	}
	if errCount > 0 {
		notifyType = "warning"
	}

	if err := s.apprise.Notify(ctx, title, body, notifyType); err != nil {
		logger.Warnf("🔔 Failed to send notification: %v", err)
	} else {
		logger.Info("🔔 Notification sent successfully")
	}
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

	// Get season info for total episode counts
	seasons, err := s.trakt.GetShowSeasons(ctx, item.show.IDs.Trakt)
	if err != nil {
		// Non-fatal, just use aired count if we can't get total
		seasons = nil
	}

	// Determine if we should request this season based on watch progress
	shouldRequest, reason := s.shouldRequestSeason(progress, seasons, item.season)
	if !shouldRequest {
		result.Action = "skipped"
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

	requestInfo := s.overseerr.GetSeasonRequestInfo(tvDetails, item.season)
	if requestInfo.Requested {
		result.Action = "already_requested"
		if requestInfo.RequestedBy != "" {
			result.Reason = fmt.Sprintf("already requested by %s", requestInfo.RequestedBy)
		} else if requestInfo.Status >= 4 { // Available or partially available
			result.Reason = "already available in Overseerr"
		} else {
			result.Reason = "already requested in Overseerr"
		}
		return result
	}

	// In dry-run mode, don't actually request
	if s.dryRun {
		result.Action = "dry_run"
		result.Reason = reason
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
func (s *Service) shouldRequestSeason(progress *trakt.ShowProgress, seasons []trakt.SeasonSummary, targetSeason int) (bool, string) {
	// Build map of total episode counts per season
	totalEps := make(map[int]int)
	for _, s := range seasons {
		totalEps[s.Number] = s.EpisodeCount
	}

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
		total := totalEps[targetSeason]
		if total == 0 {
			total = targetSeasonProgress.Aired // fallback to aired if no total
		}
		if targetSeasonProgress.Completed >= targetSeasonProgress.Aired {
			return false, fmt.Sprintf("S%02d complete (%d/%d eps, %d aired)",
				targetSeason, targetSeasonProgress.Completed, total, targetSeasonProgress.Aired)
		}
		return false, fmt.Sprintf("watching S%02d (%d/%d eps, %d aired)",
			targetSeason, targetSeasonProgress.Completed, total, targetSeasonProgress.Aired)
	}

	// For season 1
	if targetSeason == 1 {
		// If target season exists in progress but 0 completed, user might have it but not started
		// This means S01 is likely already available
		if targetSeasonProgress != nil {
			return false, "S01 available (not started)"
		}

		// No S01 in progress - check if they've only watched specials (S00)
		for _, sp := range progress.Seasons {
			if sp.Number == 0 && sp.Completed > 0 {
				return true, "watched specials only"
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
		return false, fmt.Sprintf("S%02d not aired", prevSeason)
	}

	if prevSeasonProgress.Completed < prevSeasonProgress.Aired {
		total := totalEps[prevSeason]
		if total == 0 {
			total = prevSeasonProgress.Aired
		}
		return false, fmt.Sprintf("S%02d incomplete (%d/%d eps, %d aired)",
			prevSeason, prevSeasonProgress.Completed, total, prevSeasonProgress.Aired)
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

// unused but keeping for reference
var _ = strings.Join
