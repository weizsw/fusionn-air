package watcher

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/fusionn-air/internal/client/apprise"
	"github.com/fusionn-air/internal/client/overseerr"
	"github.com/fusionn-air/internal/config"
	"github.com/fusionn-air/pkg/logger"
)

var log = logger.Component("watcher")

// ErrAlreadyRunning is returned when another watcher run is active.
var ErrAlreadyRunning = errors.New("watcher is already running")

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

// Service handles the core logic of checking calendar and requesting shows
type Service struct {
	source    Source
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

func NewService(source Source, overseerrClient *overseerr.Client, appriseClient *apprise.Client, cfgMgr *config.Manager) *Service {
	var notification notificationAdapter
	if appriseClient != nil {
		notification = appriseClient
	}
	return newService(source, overseerrClient, notification, cfgMgr)
}

func newService(source Source, overseerrClient overseerrAdapter, notification notificationAdapter, cfgMgr configSource) *Service {
	return &Service{source: source, overseerr: overseerrClient, apprise: notification, cfgMgr: cfgMgr}
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

	log.Info(ctx, "watcher_started", "Checking upcoming seasons",
		zap.Int("calendar_days", calendarDays), zap.Bool("dry_run", dryRun))
	calendarItems, err := s.source.upcoming(ctx, calendarDays)
	if err != nil {
		return nil, fmt.Errorf("getting calendar: %w", err)
	}

	if len(calendarItems) == 0 {
		log.Info(ctx, "watcher_completed", "No upcoming episodes", zap.Int("candidates", 0), logger.DurMs(time.Since(startTime)))
		return nil, nil
	}

	showSeasons := s.groupByShowAndSeason(calendarItems)

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
	logResults(ctx, results, startTime, dryRun)

	// Send notification
	s.sendNotification(ctx, results, dryRun)

	return results, nil
}

func logResults(ctx context.Context, results []ProcessResult, startTime time.Time, dryRun bool) {
	counts := make(map[string]int)
	for _, result := range results {
		counts[result.Action]++
		fields := []zap.Field{
			zap.String("title", result.ShowTitle), zap.Int("tmdb_id", result.ShowTMDB),
			zap.Int("season", result.Season), zap.String("action", result.Action),
		}
		if result.Route != "" {
			fields = append(fields, zap.String("route", result.Route))
		}
		if result.Action == "error" {
			log.Error(ctx, "season_evaluated", "Season check failed", append(fields, zap.String("error", result.Error))...)
			continue
		}
		log.Info(ctx, "season_evaluated", "Season checked", append(fields, zap.String("reason", result.Reason))...)
	}
	log.Info(ctx, "watcher_completed", "Upcoming seasons checked",
		zap.Int("candidates", len(results)), zap.Int("requested", counts["requested"]),
		zap.Int("would_request", counts["dry_run"]), zap.Int("skipped", counts["skipped"]+counts["already_requested"]),
		zap.Int("errors", counts["error"]), zap.Bool("dry_run", dryRun), logger.DurMs(time.Since(startTime)))
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
		log.Warn(ctx, "notification_failed", "Could not send watcher results", zap.Error(err))
	} else {
		log.Info(ctx, "notification_sent", "Watcher results sent")
	}
}

type Source interface {
	upcoming(ctx context.Context, days int) ([]calendarItem, error)
}

type calendarItem struct {
	tmdbID             int
	title              string
	season             int
	episode            int
	airDate            time.Time
	genres             []string
	countries          []string
	language           string
	routeWithOverseerr bool
	eligible           func(context.Context) (bool, string, error)
}

func (s *Service) groupByShowAndSeason(items []calendarItem) map[[2]int]calendarItem {
	result := make(map[[2]int]calendarItem)
	for _, item := range items {
		key := [2]int{item.tmdbID, item.season}
		if _, exists := result[key]; !exists {
			result[key] = item
		}
	}
	return result
}

func exclusionReason(genres []string, language string, cfg config.WatcherConfig) string {
	if len(cfg.ExcludedGenres) > 0 && len(genres) == 0 {
		return "genres unknown"
	}
	for _, genre := range genres {
		for _, excluded := range cfg.ExcludedGenres {
			if strings.EqualFold(genre, excluded) {
				return "excluded genre: " + genre
			}
		}
	}
	if len(cfg.AllowedLanguages) > 0 {
		if language == "" {
			return "language unknown"
		}
		for _, allowed := range cfg.AllowedLanguages {
			if strings.EqualFold(language, allowed) {
				return ""
			}
		}
		return "language not allowed: " + language
	}
	return ""
}

func (s *Service) processShow(ctx context.Context, item calendarItem, dryRun bool, cfg config.WatcherConfig) ProcessResult {
	result := ProcessResult{
		ShowTitle: item.title,
		ShowTMDB:  item.tmdbID,
		Season:    item.season,
		Episode:   item.episode,
		AirDate:   item.airDate,
	}

	// Determine routing
	var serverID *int
	if !item.routeWithOverseerr {
		if reason := exclusionReason(item.genres, item.language, cfg); reason != "" {
			result.Action = "skipped"
			result.Reason = reason
			return result
		}
		serverID, result.Route = determineServerID(item.genres, item.countries, cfg.Routing)
	}

	// Skip if no TMDB ID (can't request without it)
	if item.tmdbID == 0 {
		result.Action = "skipped"
		result.Reason = "no TMDB ID"
		return result
	}

	shouldRequest, reason, err := item.eligible(ctx)
	if err != nil {
		result.Action = "error"
		result.Error = fmt.Sprintf("failed to get progress: %v", err)
		return result
	}
	if !shouldRequest {
		result.Action = "skipped"
		result.Reason = reason
		return result
	}

	// Check Overseerr if already requested/available
	tvDetails, err := s.overseerr.GetTVByTMDB(ctx, item.tmdbID)
	if err != nil {
		result.Action = "error"
		result.Error = fmt.Sprintf("Overseerr error: %v", err)
		return result
	}

	if item.routeWithOverseerr {
		var genres []string
		for _, genre := range tvDetails.Genres {
			genres = append(genres, genre.Name)
			if strings.EqualFold(genre.Name, "Animation") && slices.ContainsFunc(tvDetails.OriginCountry, func(country string) bool {
				return strings.EqualFold(country, "JP")
			}) {
				genres = append(genres, "anime")
			}
		}
		if reason := exclusionReason(genres, tvDetails.OriginalLanguage, cfg); reason != "" {
			result.Action = "skipped"
			result.Reason = reason
			return result
		}
		serverID, result.Route = determineServerID(genres, tvDetails.OriginCountry, cfg.Routing)
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
	_, err = s.overseerr.RequestTV(ctx, item.tmdbID, []int{item.season}, serverID)
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
func determineServerID(genres, countries []string, routing config.RoutingConfig) (*int, string) {
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
	for _, country := range countries {
		for _, altCountry := range routing.AlternateCountries {
			if strings.EqualFold(country, altCountry) {
				return &routing.AlternateServerID, "alternate"
			}
		}
	}

	// Default server
	id := routing.DefaultServerID
	return &id, "default"
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
