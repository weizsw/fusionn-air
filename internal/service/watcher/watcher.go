package watcher

import (
	"cmp"
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

	mu          sync.RWMutex
	running     bool
	lastRun     time.Time
	lastError   string
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
	if !s.beginRun() {
		return nil, ErrAlreadyRunning
	}
	defer s.endRun()
	return s.processCalendar(ctx)
}

// StartCalendar accepts one asynchronous run and returns a channel that closes when it ends.
func (s *Service) StartCalendar(ctx context.Context) (<-chan struct{}, error) {
	if !s.beginRun() {
		return nil, ErrAlreadyRunning
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer s.endRun()
		runCtx, cancel := context.WithTimeout(ctx, 15*time.Minute)
		defer cancel()
		if _, err := s.processCalendar(runCtx); err != nil {
			log.Error(runCtx, "watcher_failed", "Watcher run failed", zap.Error(err))
		}
	}()
	return done, nil
}

func (s *Service) beginRun() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.running {
		return false
	}
	s.running = true
	return true
}

func (s *Service) endRun() {
	s.mu.Lock()
	s.running = false
	s.mu.Unlock()
}

func (s *Service) processCalendar(ctx context.Context) ([]ProcessResult, error) {
	// Get fresh config for this run (supports hot-reload)
	cfg := s.cfgMgr.Get()
	dryRun := cfg.Scheduler.DryRun
	calendarDays := cfg.Watcher.CalendarDays

	startTime := time.Now()

	log.Info(ctx, "watcher_started", "Checking upcoming seasons",
		zap.Int("calendar_days", calendarDays), zap.Bool("dry_run", dryRun))
	calendarItems, err := s.source.upcoming(ctx, calendarDays)
	if err != nil {
		err = fmt.Errorf("getting calendar: %w", err)
		s.failRun(ctx, nil, err)
		return nil, err
	}

	var results []ProcessResult
	for _, item := range groupByShowAndSeason(calendarItems) {
		if err := ctx.Err(); err != nil {
			s.failRun(ctx, results, err)
			return results, err
		}
		results = append(results, s.processShow(ctx, item, dryRun, cfg.Watcher))
	}
	if err := ctx.Err(); err != nil {
		s.failRun(ctx, results, err)
		return results, err
	}

	s.recordRun(results, nil)
	logResults(ctx, results, startTime, dryRun)
	if len(results) > 0 {
		s.sendNotification(ctx, results, dryRun)
	}
	return results, nil
}

func (s *Service) failRun(ctx context.Context, results []ProcessResult, err error) {
	s.recordRun(results, err)
	if errors.Is(ctx.Err(), context.Canceled) {
		return
	}
	notifyCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	s.notify(notifyCtx, "Watcher run failed", logger.Redact(err.Error()), "failure")
}

func (s *Service) recordRun(results []ProcessResult, err error) {
	message := ""
	if err != nil {
		message = logger.Redact(err.Error())
	}
	s.mu.Lock()
	s.lastRun = time.Now()
	s.lastError = message
	s.lastResults = slices.Clone(results)
	s.mu.Unlock()
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

	var requested, skipped, errCount int
	var details []apprise.WatcherDetail
	for _, r := range results {
		reason := r.Reason
		switch r.Action {
		case "requested", "dry_run":
			requested++
		case "error":
			errCount++
			reason = logger.Redact(r.Error)
		default:
			skipped++
			continue
		}
		details = append(details, apprise.WatcherDetail{
			ShowTitle: r.ShowTitle,
			Season:    r.Season,
			Action:    r.Action,
			Reason:    reason,
			Route:     r.Route,
		})
	}

	title := "📺 Watcher Results"
	if dryRun {
		title = "📺 Watcher Results (DRY RUN)"
	}

	body := apprise.FormatWatcherResults(requested, skipped, errCount, details)

	notifyType := "info"
	if requested > 0 {
		notifyType = "success"
	}
	if errCount > 0 {
		notifyType = "warning"
	}

	s.notify(ctx, title, body, notifyType)
}

func (s *Service) notify(ctx context.Context, title, body, notifyType string) {
	if s.apprise == nil || !s.apprise.IsEnabled() {
		return
	}
	if err := s.apprise.Notify(ctx, title, apprise.LimitNotification(body), notifyType); err != nil {
		log.Warn(ctx, "notification_failed", "Could not send watcher results", zap.Error(err))
	} else {
		log.Info(ctx, "notification_sent", "Watcher results sent")
	}
}

type Source interface {
	upcoming(ctx context.Context, days int) ([]calendarItem, error)
}

type calendarItem struct {
	tmdbID                 int
	title                  string
	season                 int
	episode                int
	airDate                time.Time
	metadataBeforeProgress bool
	eligible               func(context.Context) (bool, string, error)
}

func groupByShowAndSeason(items []calendarItem) []calendarItem {
	slices.SortFunc(items, func(a, b calendarItem) int {
		return cmp.Or(a.airDate.Compare(b.airDate), cmp.Compare(a.tmdbID, b.tmdbID),
			cmp.Compare(a.season, b.season), cmp.Compare(a.episode, b.episode), cmp.Compare(a.title, b.title))
	})
	seen := make(map[[2]int]bool)
	unique := items[:0]
	for _, item := range items {
		key := [2]int{item.tmdbID, item.season}
		if !seen[key] {
			seen[key] = true
			unique = append(unique, item)
		}
	}
	return unique
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

	if item.tmdbID <= 0 {
		result.Action = "skipped"
		result.Reason = "no TMDB ID"
		return result
	}

	var serverID *int
	if item.metadataBeforeProgress {
		serverID = s.checkOverseerr(ctx, item, cfg, &result)
		if result.Action != "" {
			return result
		}
	}

	shouldRequest, reason, err := item.eligible(ctx)
	if err != nil {
		result.Action = "error"
		result.Error = logger.Redact(fmt.Sprintf("failed to check season eligibility: %v", err))
		return result
	}
	if !shouldRequest {
		result.Action = "skipped"
		result.Reason = reason
		return result
	}

	if !item.metadataBeforeProgress {
		serverID = s.checkOverseerr(ctx, item, cfg, &result)
		if result.Action != "" {
			return result
		}
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
		result.Error = logger.Redact(fmt.Sprintf("request failed: %v", err))
		return result
	}

	result.Action = "requested"
	result.Reason = reason
	return result
}

func (s *Service) checkOverseerr(ctx context.Context, item calendarItem, cfg config.WatcherConfig, result *ProcessResult) *int {
	details, err := s.overseerr.GetTVByTMDB(ctx, item.tmdbID)
	if err != nil {
		result.Action = "error"
		result.Error = logger.Redact(fmt.Sprintf("Overseerr error: %v", err))
		return nil
	}

	var genres []string
	for _, genre := range details.Genres {
		genres = append(genres, genre.Name)
		if strings.EqualFold(genre.Name, "Animation") {
			for _, country := range details.OriginCountry {
				switch strings.ToUpper(country) {
				case "JP":
					genres = append(genres, "anime")
				case "CN":
					genres = append(genres, "donghua")
				}
			}
		}
	}
	if reason := exclusionReason(genres, details.OriginalLanguage, cfg); reason != "" {
		result.Action = "skipped"
		result.Reason = reason
		return nil
	}
	serverID, route := determineServerID(genres, details.OriginCountry, cfg.Routing)
	result.Route = route

	request := s.overseerr.GetSeasonRequestInfo(details, item.season)
	switch request.State {
	case overseerr.SeasonRequestActive:
		result.Action = "already_requested"
		result.Reason = "already requested in Overseerr"
		if request.RequestedBy != "" {
			result.Reason = "already requested by " + request.RequestedBy
		}
	case overseerr.SeasonRequestAvailable:
		result.Action = "already_requested"
		result.Reason = "already available in Overseerr"
	case overseerr.SeasonRequestFailed:
		result.Action = "error"
		result.Error = fmt.Sprintf("Overseerr request %d failed. Check the request in Seerr or Overseerr.", request.RequestID)
	case overseerr.SeasonRequestDeclined:
		result.Action = "skipped"
		result.Reason = "request declined in Overseerr"
	case overseerr.SeasonRequestBlocklisted:
		result.Action = "skipped"
		result.Reason = "blocklisted in Overseerr"
	case overseerr.SeasonRequestDeleted:
		result.Action = "skipped"
		result.Reason = "marked deleted in Overseerr"
	}
	return serverID
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

// Stats returns processing statistics
type Stats struct {
	LastRun    time.Time       `json:"last_run"`
	Running    bool            `json:"running"`
	LastError  string          `json:"last_error,omitempty"`
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
		Running:    s.running,
		LastError:  s.lastError,
		TotalShows: len(s.lastResults),
		Results:    slices.Clone(s.lastResults),
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
