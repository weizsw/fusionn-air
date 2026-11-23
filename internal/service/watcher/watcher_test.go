package watcher

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/fusionn-air/internal/client/overseerr"
	"github.com/fusionn-air/internal/client/trakt"
	"github.com/fusionn-air/internal/config"
)

type watcherConfigStub struct{ cfg config.Config }

func (s *watcherConfigStub) Get() *config.Config { return &s.cfg }

type watcherTraktStub struct {
	calendar    []trakt.CalendarShow
	calendarErr error
	seasonsErr  error
	started     chan struct{}
	block       chan struct{}
}

func (s *watcherTraktStub) GetMyShowsCalendar(context.Context, int) ([]trakt.CalendarShow, error) {
	if s.started != nil {
		close(s.started)
	}
	if s.block != nil {
		<-s.block
	}
	return s.calendar, s.calendarErr
}
func (s *watcherTraktStub) GetShowProgress(context.Context, int) (*trakt.ShowProgress, error) {
	return &trakt.ShowProgress{Seasons: []trakt.SeasonProgress{{Number: 1, Aired: 10, Completed: 10}}}, nil
}
func (s *watcherTraktStub) GetShowSeasons(context.Context, int) ([]trakt.SeasonSummary, error) {
	return []trakt.SeasonSummary{{Number: 1, EpisodeCount: 10, AiredEpisodes: 10}}, s.seasonsErr
}

type overseerrStub struct {
	requestCalls int
	details      overseerr.TVDetails
}

func (s *overseerrStub) GetTVByTMDB(context.Context, int) (*overseerr.TVDetails, error) {
	return &s.details, nil
}
func (s *overseerrStub) GetSeasonRequestInfo(*overseerr.TVDetails, int) overseerr.SeasonRequestInfo {
	return overseerr.SeasonRequestInfo{}
}
func (s *overseerrStub) RequestTV(context.Context, int, []int, *int) (*overseerr.RequestResponse, error) {
	s.requestCalls++
	return &overseerr.RequestResponse{}, nil
}

func TestProcessCalendarUsesPlatformSeams(t *testing.T) {
	traktClient := &watcherTraktStub{calendar: []trakt.CalendarShow{{
		Show:    trakt.Show{Title: "Example", IDs: trakt.IDs{Trakt: 1, TMDB: 2}},
		Episode: trakt.Episode{Season: 2, Number: 1},
	}}}
	overseerrClient := &overseerrStub{}
	service := newService(&traktSource{client: traktClient}, overseerrClient, nil, &watcherConfigStub{cfg: config.Config{Watcher: config.WatcherConfig{CalendarDays: 7}}})

	results, err := service.ProcessCalendar(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if overseerrClient.requestCalls != 1 || len(results) != 1 || results[0].Action != "requested" {
		t.Fatalf("requests = %d, results = %#v; want one requested season", overseerrClient.requestCalls, results)
	}
}

func TestProcessCalendarRejectsOverlappingRun(t *testing.T) {
	traktClient := &watcherTraktStub{started: make(chan struct{}), block: make(chan struct{})}
	service := newService(&traktSource{client: traktClient}, &overseerrStub{}, nil, &watcherConfigStub{})
	firstDone := make(chan error, 1)
	go func() {
		_, err := service.ProcessCalendar(context.Background())
		firstDone <- err
	}()
	<-traktClient.started

	if _, err := service.ProcessCalendar(context.Background()); !errors.Is(err, ErrAlreadyRunning) {
		t.Fatalf("overlapping ProcessCalendar() error = %v, want ErrAlreadyRunning", err)
	}
	close(traktClient.block)
	if err := <-firstDone; err != nil {
		t.Fatal(err)
	}
}

func TestExclusionReasonRejectsConfiguredGenre(t *testing.T) {
	show := trakt.Show{Genres: []string{"Drama", "Animation"}, Language: "ja"}
	cfg := config.WatcherConfig{
		ExcludedGenres:   []string{"animation"},
		AllowedLanguages: []string{"en"},
	}

	if got := exclusionReason(show.Genres, show.Language, cfg); got != "excluded genre: Animation" {
		t.Fatalf("exclusionReason() = %q, want %q", got, "excluded genre: Animation")
	}

	show.Language = "en"
	cfg.ExcludedGenres = []string{"anim"}
	if got := exclusionReason(show.Genres, show.Language, cfg); got != "" {
		t.Fatalf("exclusionReason() with partial genre = %q, want eligible series", got)
	}
}

func TestExclusionReasonRejectsDisallowedOriginalLanguage(t *testing.T) {
	show := trakt.Show{Genres: []string{"drama"}, Language: "ko"}
	cfg := config.WatcherConfig{AllowedLanguages: []string{"en"}}

	if got := exclusionReason(show.Genres, show.Language, cfg); got != "language not allowed: ko" {
		t.Fatalf("exclusionReason() = %q, want %q", got, "language not allowed: ko")
	}

	show.Language = "EN"
	if got := exclusionReason(show.Genres, show.Language, cfg); got != "" {
		t.Fatalf("exclusionReason() = %q, want eligible English series", got)
	}
}

func TestExclusionReasonRejectsUnknownGenresWhenFilterEnabled(t *testing.T) {
	show := trakt.Show{Language: "en"}
	cfg := config.WatcherConfig{ExcludedGenres: []string{"animation"}}

	if got := exclusionReason(show.Genres, show.Language, cfg); got != "genres unknown" {
		t.Fatalf("exclusionReason() = %q, want %q", got, "genres unknown")
	}
	if got := exclusionReason(show.Genres, show.Language, config.WatcherConfig{}); got != "" {
		t.Fatalf("exclusionReason() with disabled filters = %q, want eligible series", got)
	}
}

func TestExclusionReasonRejectsUnknownLanguageWhenFilterEnabled(t *testing.T) {
	show := trakt.Show{Genres: []string{"drama"}}
	cfg := config.WatcherConfig{AllowedLanguages: []string{"en"}}

	if got := exclusionReason(show.Genres, show.Language, cfg); got != "language unknown" {
		t.Fatalf("exclusionReason() = %q, want %q", got, "language unknown")
	}
}

func TestTraktCandidateUsesOverseerrMetadataBeforeProgress(t *testing.T) {
	traktClient := &watcherTraktStub{calendar: []trakt.CalendarShow{{
		Show:    trakt.Show{IDs: trakt.IDs{TMDB: 2}, Language: "en"},
		Episode: trakt.Episode{Season: 2, Number: 1},
	}}, seasonsErr: errors.New("must not get season counts")}
	seerr := &overseerrStub{details: overseerr.TVDetails{OriginalLanguage: "ja"}}
	cfg := &watcherConfigStub{cfg: config.Config{Watcher: config.WatcherConfig{AllowedLanguages: []string{"en"}}}}
	service := newService(&traktSource{client: traktClient}, seerr, nil, cfg)
	results, err := service.ProcessCalendar(context.Background())
	if err != nil || len(results) != 1 || results[0].Action != "skipped" || results[0].Reason != "language not allowed: ja" {
		t.Fatalf("results=%+v, err=%v", results, err)
	}
}

func TestShouldRequestSeasonSkipsWhenPreviousSeasonStillAiring(t *testing.T) {
	progress := &trakt.ShowProgress{
		Seasons: []trakt.SeasonProgress{
			{Number: 2, Aired: 5, Completed: 5},
		},
	}
	seasons := []trakt.SeasonSummary{
		{Number: 2, EpisodeCount: 12, AiredEpisodes: 5},
	}

	shouldRequest, reason := shouldRequestSeason(progress, seasons, 3)

	if shouldRequest {
		t.Fatalf("shouldRequestSeason() should not request while previous season is still airing; reason=%q", reason)
	}
	wantReason := "S02 ongoing (5/12 eps, 5 aired)"
	if reason != wantReason {
		t.Fatalf("shouldRequestSeason() reason = %q, want %q", reason, wantReason)
	}
}

func TestShouldRequestSeasonRequestsWhenPreviousSeasonFullyWatched(t *testing.T) {
	progress := &trakt.ShowProgress{
		Seasons: []trakt.SeasonProgress{
			{Number: 1, Aired: 10, Completed: 10},
		},
	}
	seasons := []trakt.SeasonSummary{
		{Number: 1, EpisodeCount: 10, AiredEpisodes: 10},
	}

	shouldRequest, reason := shouldRequestSeason(progress, seasons, 2)

	if !shouldRequest {
		t.Fatalf("shouldRequestSeason() should request when previous season is fully watched; reason=%q", reason)
	}
	wantReason := "S01 complete"
	if reason != wantReason {
		t.Fatalf("shouldRequestSeason() reason = %q, want %q", reason, wantReason)
	}
}

func TestShouldRequestSeasonDoesNotMarkCaughtUpTargetSeasonComplete(t *testing.T) {
	progress := &trakt.ShowProgress{
		Seasons: []trakt.SeasonProgress{
			{Number: 2, Aired: 5, Completed: 5},
		},
	}
	seasons := []trakt.SeasonSummary{
		{Number: 2, EpisodeCount: 12, AiredEpisodes: 5},
	}

	shouldRequest, reason := shouldRequestSeason(progress, seasons, 2)

	if shouldRequest {
		t.Fatalf("shouldRequestSeason() should not request when target season is already being watched; reason=%q", reason)
	}
	wantReason := "watching S02 (5/12 eps, 5 aired)"
	if reason != wantReason {
		t.Fatalf("shouldRequestSeason() reason = %q, want %q", reason, wantReason)
	}
}

func TestShouldRequestSeasonSkipsWhenPreviousSeasonTotalUnknown(t *testing.T) {
	progress := &trakt.ShowProgress{
		Seasons: []trakt.SeasonProgress{
			{Number: 2, Aired: 5, Completed: 5},
		},
	}

	shouldRequest, reason := shouldRequestSeason(progress, nil, 3)

	if shouldRequest {
		t.Fatalf("shouldRequestSeason() should not request when previous season total is unknown; reason=%q", reason)
	}
	wantReason := "S02 total episode count unknown"
	if reason != wantReason {
		t.Fatalf("shouldRequestSeason() reason = %q, want %q", reason, wantReason)
	}
}

func TestShouldRequestSeasonDoesNotMarkTargetSeasonCompleteWhenTotalUnknown(t *testing.T) {
	progress := &trakt.ShowProgress{
		Seasons: []trakt.SeasonProgress{
			{Number: 2, Aired: 5, Completed: 5},
		},
	}

	shouldRequest, reason := shouldRequestSeason(progress, nil, 2)

	if shouldRequest {
		t.Fatalf("shouldRequestSeason() should not request when target season is already being watched; reason=%q", reason)
	}
	wantReason := "watching S02 (5 eps watched, total unknown, 5 aired)"
	if reason != wantReason {
		t.Fatalf("shouldRequestSeason() reason = %q, want %q", reason, wantReason)
	}
}

func TestShouldRequestSeasonSkipsS01WhenOnlySpecialsWatched(t *testing.T) {
	progress := &trakt.ShowProgress{
		Seasons: []trakt.SeasonProgress{
			{Number: 0, Aired: 1, Completed: 1},
		},
	}

	shouldRequest, reason := shouldRequestSeason(progress, nil, 1)

	if shouldRequest {
		t.Fatalf("shouldRequestSeason() should not request S01 from specials-only history; reason=%q", reason)
	}
	wantReason := "no S01 watch history"
	if reason != wantReason {
		t.Fatalf("shouldRequestSeason() reason = %q, want %q", reason, wantReason)
	}
}

func TestShouldRequestSeasonSkipsWhenPreviousSeasonHidden(t *testing.T) {
	progress := &trakt.ShowProgress{
		HiddenSeasons: []trakt.Season{
			{Number: 1},
		},
	}
	seasons := []trakt.SeasonSummary{
		{Number: 1, EpisodeCount: 10, AiredEpisodes: 10},
	}

	shouldRequest, reason := shouldRequestSeason(progress, seasons, 2)

	if shouldRequest {
		t.Fatalf("shouldRequestSeason() should not request when previous season is hidden; reason=%q", reason)
	}
	wantReason := "S01 hidden on Trakt"
	if reason != wantReason {
		t.Fatalf("shouldRequestSeason() reason = %q, want %q", reason, wantReason)
	}
}

func TestProcessCalendarReportsFailedAndEmptyRuns(t *testing.T) {
	upstream := &watcherTraktStub{calendar: []trakt.CalendarShow{{
		Show:    trakt.Show{Title: "Example", IDs: trakt.IDs{Trakt: 1, TMDB: 2}},
		Episode: trakt.Episode{Season: 2, Number: 1},
	}}}
	notifications := &notificationStub{}
	service := newService(&traktSource{client: upstream}, &overseerrStub{}, notifications, &watcherConfigStub{})
	ctx := context.Background()
	upstream.seasonsErr = errors.New("season lookup failed")
	results, err := service.ProcessCalendar(ctx)
	if err != nil || len(results) != 1 || results[0].Action != "error" || !strings.Contains(notifications.body, "season lookup failed") {
		t.Fatalf("season failure: results=%+v, err=%v, notification=%q", results, err, notifications.body)
	}

	upstream.calendarErr = errors.New("calendar unavailable")
	if _, err := service.ProcessCalendar(ctx); !errors.Is(err, upstream.calendarErr) {
		t.Fatalf("run error = %v", err)
	}
	if stats := service.GetStats(); stats.LastError == "" || stats.LastRun.IsZero() || len(stats.Results) != 0 || !strings.Contains(notifications.body, "calendar unavailable") {
		t.Fatalf("failed run: stats=%+v, notification=%q", stats, notifications.body)
	}

	upstream.calendarErr = nil
	upstream.calendar[0].Episode.Season = 0
	if results, err := service.ProcessCalendar(ctx); err != nil || len(results) != 0 {
		t.Fatalf("specials-only calendar: results=%+v, err=%v", results, err)
	}
	if stats := service.GetStats(); stats.LastError != "" || stats.LastRun.IsZero() || stats.TotalShows != 0 {
		t.Fatalf("empty run: stats=%+v", stats)
	}
}

type notificationStub struct{ body string }

func (*notificationStub) IsEnabled() bool { return true }

func (n *notificationStub) Notify(_ context.Context, _, body, _ string) error {
	n.body = body
	return nil
}
