package watcher

import (
	"context"
	"errors"
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

var ErrAlreadyRunning = errors.New("watcher is already running")

type traktAdapter interface {
	GetMyShowsCalendar(context.Context, int) ([]trakt.CalendarShow, error)
	GetShowProgress(context.Context, int) (*trakt.ShowProgress, error)
	GetShowSeasons(context.Context, int) ([]trakt.SeasonSummary, error)
}

type overseerrAdapter interface {
	GetTVByTMDB(context.Context, int) (*overseerr.TVDetails, error)
	GetSeasonRequestInfo(*overseerr.TVDetails, int) overseerr.SeasonRequestInfo
	RequestTV(context.Context, int, []int, *int) (*overseerr.RequestResponse, error)
}

type notificationAdapter interface {
	IsEnabled() bool
	Notify(context.Context, string, string, string) error
}

type configSource interface {
	Get() *config.Config
}

// Service handles the core logic of checking calendar and requesting shows.
type Service struct {
	trakt     traktAdapter
	overseerr overseerrAdapter
	apprise   notificationAdapter
	cfgMgr    configSource

	runMu       sync.Mutex
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
	Route     string    `json:"route,omitempty"` // "default", "alternate", or "" (no routing configured)
}

func NewService(traktClient *trakt.Client, overseerrClient *overseerr.Client, appriseClient *apprise.Client, cfgMgr *config.Manager) *Service {
	var notification notificationAdapter
	if appriseClient != nil {
		notification = appriseClient
	}
	return newService(traktClient, overseerrClient, notification, cfgMgr)
}

func newService(traktClient traktAdapter, overseerrClient overseerrAdapter, appriseClient notificationAdapter, cfgMgr configSource) *Service {
	return &Service{trakt: traktClient, overseerr: overseerrClient, apprise: appriseClient, cfgMgr: cfgMgr}
}

// ProcessCalendar checks the calendar and requests new seasons as needed
func (s *Service) ProcessCalendar(ctx context.Context) ([]ProcessResult, error) {
	if !s.runMu.TryLock() {
		return nil, ErrAlreadyRunning
	}
	defer s.runMu.Unlock()

	// Get fresh config for this run (supports hot-reload)
	cfg := s.cfgMgr.Get()
	dryRun := cfg.Scheduler.DryRun
	calendarDays := cfg.Watcher.CalendarDays

	startTime := time.Now()

	logger.Info("")
	logger.Info("╔══════════════════════════════════════════════════════════════╗")
	logger.Info("║              CALENDAR PROCESSING STARTED                     ║")
	logger.Info("╚══════════════════════════════════════════════════════════════╝")

	if dryRun {
		logger.Warn("⚠️  DRY RUN MODE - No actual requests will be made")
	}

	// Get upcoming shows from Trakt calendar
	logger.Infof("📅 Fetching calendar for next %d days...", calendarDays)
	calendarItems, err := s.trakt.GetMyShowsCalendar(ctx, calendarDays)
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
		result := s.processShow(ctx, item, dryRun, cfg.Watcher)
		results = append(results, result)
	}

	// Store results
	s.mu.Lock()
	s.lastRun = time.Now()
	s.lastResults = results
	s.mu.Unlock()

	// Print summary
	s.printSummary(results, startTime, dryRun)

	// Send notification
	s.sendNotification(ctx, results, dryRun)

	return results, nil
}

// printSummary prints a grouped summary of results
func (s *Service) printSummary(results []ProcessResult, startTime time.Time, dryRun bool) {
	var willRequest []string
	var willSkip []string
	var errors []string

	for _, r := range results {
		showInfo := fmt.Sprintf("%s S%02d", r.ShowTitle, r.Season)
		routeTag := ""
		if r.Route != "" {
			routeTag = fmt.Sprintf(" [→ %s]", r.Route)
		}
		switch r.Action {
		case "requested", "dry_run":
			willRequest = append(willRequest, fmt.Sprintf("   • %-35s  ← %s%s", showInfo, r.Reason, routeTag))
		case "skipped", "already_requested":
			willSkip = append(willSkip, fmt.Sprintf("   • %-35s  ← %s", showInfo, r.Reason))
		case "error":
			errors = append(errors, fmt.Sprintf("   • %-35s  ← %s", showInfo, r.Error))
		}
	}

	logger.Info("┌──────────────────────────────────────────────────────────────┐")
	logger.Info("│                         RESULTS                              │")
	logger.Info("└──────────────────────────────────────────────────────────────┘")

	if len(willRequest) > 0 {
		logger.Info("")
		if dryRun {
			logger.Warnf("📥 WOULD REQUEST (%d):", len(willRequest))
		} else {
			logger.Infof("📥 REQUESTED (%d):", len(willRequest))
		}
		for _, line := range willRequest {
			if dryRun {
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
func (s *Service) sendNotification(ctx context.Context, results []ProcessResult, dryRun bool) {
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
			Route:     r.Route,
		})
	}

	title := "📺 Watcher Results"
	if dryRun {
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

func exclusionReason(show trakt.Show, cfg config.WatcherConfig) string {
	if len(cfg.ExcludedGenres) > 0 && len(show.Genres) == 0 {
		return "genres unknown"
	}
	for _, genre := range show.Genres {
		for _, excluded := range cfg.ExcludedGenres {
			if strings.EqualFold(genre, excluded) {
				return "excluded genre: " + genre
			}
		}
	}
	if len(cfg.AllowedLanguages) > 0 {
		if show.Language == "" {
			return "language unknown"
		}
		for _, allowed := range cfg.AllowedLanguages {
			if strings.EqualFold(show.Language, allowed) {
				return ""
			}
		}
		return "language not allowed: " + show.Language
	}
	return ""
}

func (s *Service) processShow(ctx context.Context, item calendarItem, dryRun bool, cfg config.WatcherConfig) ProcessResult {
	result := ProcessResult{
		ShowTitle: item.show.Title,
		ShowTMDB:  item.show.IDs.TMDB,
		Season:    item.season,
		Episode:   item.episode,
		AirDate:   item.airDate,
	}

	if reason := exclusionReason(item.show, cfg); reason != "" {
		result.Action = "skipped"
		result.Reason = reason
		return result
	}

	// Determine routing
	serverID, route := determineServerID(item.show.Genres, item.show.Country, cfg.Routing)
	result.Route = route

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
	shouldRequest, reason := shouldRequestSeason(progress, seasons, item.season)
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
	if dryRun {
		result.Action = "dry_run"
		result.Reason = reason
		return result
	}

	// Request the season with routing
	_, err = s.overseerr.RequestTV(ctx, item.show.IDs.TMDB, []int{item.season}, serverID)
	if err != nil {
		result.Action = "error"
		result.Error = fmt.Sprintf("request failed: %v", err)
		return result
	}

	result.Action = "requested"
	result.Reason = reason
	return result
}

// determineServerID checks show genres and country against routing config
// to decide which Overseerr backend server should handle the request.
func determineServerID(genres []string, country string, routing config.RoutingConfig) (*int, string) {
	// If no routing rules are configured, don't set a server ID
	if len(routing.AlternateGenres) == 0 && len(routing.AlternateCountries) == 0 {
		return nil, ""
	}

	// Check genre match
	for _, showGenre := range genres {
		for _, altGenre := range routing.AlternateGenres {
			if strings.EqualFold(showGenre, altGenre) {
				id := routing.AlternateServerID
				return &id, "alternate"
			}
		}
	}

	// Check country match
	for _, altCountry := range routing.AlternateCountries {
		if strings.EqualFold(country, altCountry) {
			id := routing.AlternateServerID
			return &id, "alternate"
		}
	}

	// Default server
	id := routing.DefaultServerID
	return &id, "default"
}

// shouldRequestSeason determines if a season should be requested based on watch progress
func shouldRequestSeason(progress *trakt.ShowProgress, seasons []trakt.SeasonSummary, targetSeason int) (bool, string) {
	// Build map of total episode counts per season
	totalEps := make(map[int]int)
	for _, s := range seasons {
		if s.EpisodeCount > 0 {
			totalEps[s.Number] = s.EpisodeCount
		}
	}

	// Find the target season in progress
	var targetSeasonProgress *trakt.SeasonProgress
	for i := range progress.Seasons {
		if progress.Seasons[i].Number == targetSeason {
			targetSeasonProgress = &progress.Seasons[i]
			break
		}
	}

	if isHiddenSeason(progress, targetSeason) {
		return false, fmt.Sprintf("S%02d hidden on Trakt", targetSeason)
	}

	// If user has already watched any episodes of target season, it's already available
	if targetSeasonProgress != nil && targetSeasonProgress.Completed > 0 {
		total, totalKnown := totalEps[targetSeason]
		if !totalKnown {
			return false, fmt.Sprintf("watching S%02d (%d eps watched, total unknown, %d aired)",
				targetSeason, targetSeasonProgress.Completed, targetSeasonProgress.Aired)
		}
		if targetSeasonProgress.Completed >= total {
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
				return false, "no S01 watch history"
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

	if isHiddenSeason(progress, prevSeason) {
		return false, fmt.Sprintf("S%02d hidden on Trakt", prevSeason)
	}

	if prevSeasonProgress == nil {
		return false, fmt.Sprintf("S%02d not watched", prevSeason)
	}

	if prevSeasonProgress.Aired == 0 {
		return false, fmt.Sprintf("S%02d not aired", prevSeason)
	}

	total, totalKnown := totalEps[prevSeason]
	if !totalKnown {
		return false, fmt.Sprintf("S%02d total episode count unknown", prevSeason)
	}

	if prevSeasonProgress.Completed < prevSeasonProgress.Aired {
		return false, fmt.Sprintf("S%02d incomplete (%d/%d eps, %d aired)",
			prevSeason, prevSeasonProgress.Completed, total, prevSeasonProgress.Aired)
	}

	if total > prevSeasonProgress.Aired {
		return false, fmt.Sprintf("S%02d ongoing (%d/%d eps, %d aired)",
			prevSeason, prevSeasonProgress.Completed, total, prevSeasonProgress.Aired)
	}

	return true, fmt.Sprintf("S%02d complete", prevSeason)
}

func isHiddenSeason(progress *trakt.ShowProgress, seasonNum int) bool {
	for _, season := range progress.HiddenSeasons {
		if season.Number == seasonNum {
			return true
		}
	}
	return false
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
