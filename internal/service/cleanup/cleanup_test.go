package cleanup

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/fusionn-air/internal/client/emby"
	"github.com/fusionn-air/internal/client/radarr"
	"github.com/fusionn-air/internal/client/trakt"
	"github.com/fusionn-air/internal/config"
	"github.com/fusionn-air/pkg/logger"
)

type cleanupConfigStub struct{ cfg config.Config }

func (s *cleanupConfigStub) Get() *config.Config { return &s.cfg }

type radarrStub struct {
	movies         []radarr.Movie
	unmonitorCalls int
	deleteCalls    int
	started        chan struct{}
	block          chan struct{}
}

func (s *radarrStub) GetAllMovies(context.Context) ([]radarr.Movie, error) {
	if s.started != nil {
		close(s.started)
	}
	if s.block != nil {
		<-s.block
	}
	return s.movies, nil
}
func (s *radarrStub) GetMovie(_ context.Context, id int) (*radarr.Movie, error) {
	for i := range s.movies {
		if s.movies[i].ID == id {
			movie := s.movies[i]
			return &movie, nil
		}
	}
	return nil, nil
}
func (s *radarrStub) DeleteMovie(context.Context, int, bool) error {
	s.deleteCalls++
	return nil
}
func (s *radarrStub) UnmonitorMovie(context.Context, int) error {
	s.unmonitorCalls++
	return nil
}

type embyStub struct {
	libraries   []emby.VirtualFolder
	movies      []emby.Item
	deleteCalls int
}

func (s *embyStub) GetLibraries(context.Context) ([]emby.VirtualFolder, error) {
	return s.libraries, nil
}
func (s *embyStub) GetMovies(context.Context, string) ([]emby.Item, error)  { return s.movies, nil }
func (s *embyStub) GetSeries(context.Context, string) ([]emby.Item, error)  { return nil, nil }
func (s *embyStub) GetSeasons(context.Context, string) ([]emby.Item, error) { return nil, nil }
func (s *embyStub) GetEpisodes(context.Context, string, string) ([]emby.Item, error) {
	return nil, nil
}
func (s *embyStub) DeleteItem(context.Context, string) error {
	s.deleteCalls++
	return nil
}

type cleanupTraktStub struct {
	watchedMovies    []trakt.WatchedMovie
	watchedMoviesErr error
}

func (s *cleanupTraktStub) GetWatchedShows(context.Context) ([]trakt.WatchedShow, error) {
	return nil, nil
}
func (s *cleanupTraktStub) GetWatchedMovies(context.Context) ([]trakt.WatchedMovie, error) {
	return s.watchedMovies, s.watchedMoviesErr
}
func (s *cleanupTraktStub) GetShowProgress(context.Context, int) (*trakt.ShowProgress, error) {
	return nil, nil
}
func (s *cleanupTraktStub) GetShowSeasons(context.Context, int) ([]trakt.SeasonSummary, error) {
	return nil, nil
}

func TestProcessCleanupRejectsOverlappingRun(t *testing.T) {
	logger.Init(true)
	radarrClient := &radarrStub{started: make(chan struct{}), block: make(chan struct{})}
	cfg := &cleanupConfigStub{cfg: config.Config{Cleanup: config.CleanupConfig{Enabled: true}}}
	service, err := newService(nil, radarrClient, nil, &cleanupTraktStub{}, nil, cfg, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	firstDone := make(chan error, 1)
	go func() {
		_, err := service.ProcessCleanup(context.Background())
		firstDone <- err
	}()
	<-radarrClient.started

	if _, err := service.ProcessCleanup(context.Background()); !errors.Is(err, ErrAlreadyRunning) {
		t.Fatalf("overlapping ProcessCleanup() error = %v, want ErrAlreadyRunning", err)
	}
	close(radarrClient.block)
	if err := <-firstDone; err != nil {
		t.Fatal(err)
	}
}

func TestProcessCleanupRetriesPendingUnmonitorWhenTraktFails(t *testing.T) {
	logger.Init(true)
	movie := radarr.Movie{ID: 7, TmdbID: 70, Title: "Example", Monitored: true, HasFile: true}
	radarrClient := &radarrStub{movies: []radarr.Movie{movie}}
	traktClient := &cleanupTraktStub{watchedMoviesErr: errors.New("trakt unavailable")}
	cfg := &cleanupConfigStub{cfg: config.Config{Cleanup: config.CleanupConfig{Enabled: true, DelayDays: 1}}}
	service, err := newService(nil, radarrClient, nil, traktClient, nil, cfg, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	queue := service.queues[MediaTypeMovie]
	if _, err := queue.Add(&QueueItem{ID: movie.ID, ExternalID: movie.TmdbID, Title: movie.Title, Reason: "watched"}); err != nil {
		t.Fatal(err)
	}

	if _, err := service.ProcessCleanup(context.Background()); err != nil {
		t.Fatal(err)
	}
	if radarrClient.unmonitorCalls != 1 || queue.Get(movie.ID).UnmonitoredAt == nil {
		t.Fatalf("unmonitor calls = %d, candidate = %#v; want retry despite Trakt failure", radarrClient.unmonitorCalls, queue.Get(movie.ID))
	}
}

func TestProcessCleanupStartsDelayAfterUnmonitoring(t *testing.T) {
	logger.Init(true)
	movie := radarr.Movie{ID: 7, TmdbID: 70, Title: "Example", Monitored: true, HasFile: true}
	radarrClient := &radarrStub{movies: []radarr.Movie{movie}}
	traktClient := &cleanupTraktStub{watchedMovies: []trakt.WatchedMovie{{
		LastWatchedAt: time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC),
		Movie:         trakt.Movie{IDs: trakt.IDs{TMDB: 70}},
	}}}
	cfg := &cleanupConfigStub{cfg: config.Config{Cleanup: config.CleanupConfig{Enabled: true, DelayDays: 1}}}
	service, err := newService(nil, radarrClient, nil, traktClient, nil, cfg, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	if _, err := service.ProcessCleanup(context.Background()); err != nil {
		t.Fatal(err)
	}
	item := service.queues[MediaTypeMovie].Get(movie.ID)
	if item == nil || item.UnmonitoredAt == nil {
		t.Fatalf("Removal Candidate = %#v, want successful unmonitor transition", item)
	}
	if item.UnmonitoredAt.Before(item.MarkedAt) {
		t.Fatalf("delay started at %v before scheduling at %v", item.UnmonitoredAt, item.MarkedAt)
	}
	if radarrClient.unmonitorCalls != 1 || radarrClient.deleteCalls != 0 {
		t.Fatalf("unmonitor calls = %d, delete calls = %d; want 1, 0", radarrClient.unmonitorCalls, radarrClient.deleteCalls)
	}
}

func TestProcessCleanupRemovesCandidateAfterUnmonitorDelay(t *testing.T) {
	logger.Init(true)
	movie := radarr.Movie{ID: 7, TmdbID: 70, Title: "Example", Monitored: false, HasFile: true}
	radarrClient := &radarrStub{movies: []radarr.Movie{movie}}
	cfg := &cleanupConfigStub{cfg: config.Config{Cleanup: config.CleanupConfig{Enabled: true, DelayDays: 1}}}
	service, err := newService(nil, radarrClient, nil, &cleanupTraktStub{}, nil, cfg, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	queue := service.queues[MediaTypeMovie]
	now := time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC)
	queue.now = func() time.Time { return now }
	if _, err := queue.Add(&QueueItem{ID: movie.ID, ExternalID: movie.TmdbID, Title: movie.Title, Reason: "watched"}); err != nil {
		t.Fatal(err)
	}
	if err := queue.MarkUnmonitored(movie.ID); err != nil {
		t.Fatal(err)
	}
	now = now.Add(24 * time.Hour)

	if _, err := service.ProcessCleanup(context.Background()); err != nil {
		t.Fatal(err)
	}
	if radarrClient.deleteCalls != 1 || queue.Get(movie.ID) != nil {
		t.Fatalf("delete calls = %d, candidate = %#v; want 1, nil", radarrClient.deleteCalls, queue.Get(movie.ID))
	}
}

func TestProcessCleanupDoesNotRemoveCandidateAddedToExclusions(t *testing.T) {
	logger.Init(true)
	movie := radarr.Movie{ID: 7, TmdbID: 70, Title: "Example", Monitored: false, HasFile: true}
	radarrClient := &radarrStub{movies: []radarr.Movie{movie}}
	cfg := &cleanupConfigStub{cfg: config.Config{Cleanup: config.CleanupConfig{Enabled: true, DelayDays: 1, Exclusions: []string{"example"}}}}
	service, err := newService(nil, radarrClient, nil, &cleanupTraktStub{}, nil, cfg, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	queue := service.queues[MediaTypeMovie]
	now := time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC)
	queue.now = func() time.Time { return now }
	if _, err := queue.Add(&QueueItem{ID: movie.ID, ExternalID: movie.TmdbID, Title: movie.Title, Reason: "watched"}); err != nil {
		t.Fatal(err)
	}
	if err := queue.MarkUnmonitored(movie.ID); err != nil {
		t.Fatal(err)
	}
	now = now.Add(24 * time.Hour)

	if _, err := service.ProcessCleanup(context.Background()); err != nil {
		t.Fatal(err)
	}
	if radarrClient.deleteCalls != 0 || queue.Get(movie.ID) != nil {
		t.Fatalf("delete calls = %d, candidate = %#v; want excluded candidate dequeued without deletion", radarrClient.deleteCalls, queue.Get(movie.ID))
	}
}

func TestProcessCleanupStopsWhenQueuePersistenceFails(t *testing.T) {
	logger.Init(true)
	movie := radarr.Movie{ID: 7, TmdbID: 70, Title: "Example", Monitored: true, HasFile: true}
	radarrClient := &radarrStub{movies: []radarr.Movie{movie}}
	traktClient := &cleanupTraktStub{watchedMovies: []trakt.WatchedMovie{{
		LastWatchedAt: time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC),
		Movie:         trakt.Movie{IDs: trakt.IDs{TMDB: 70}},
	}}}
	cfg := &cleanupConfigStub{cfg: config.Config{Cleanup: config.CleanupConfig{Enabled: true, DelayDays: 1}}}
	service, err := newService(nil, radarrClient, nil, traktClient, nil, cfg, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	service.queues[MediaTypeMovie].filePath = t.TempDir()

	if _, err := service.ProcessCleanup(context.Background()); err == nil {
		t.Fatal("ProcessCleanup() ignored queue persistence failure")
	}
	if radarrClient.unmonitorCalls != 0 || radarrClient.deleteCalls != 0 {
		t.Fatalf("persistence failure called unmonitor %d times and delete %d times", radarrClient.unmonitorCalls, radarrClient.deleteCalls)
	}
}

func TestProcessCleanupDryRunDoesNotUnmonitorPendingCandidate(t *testing.T) {
	logger.Init(true)
	movie := radarr.Movie{ID: 7, TmdbID: 70, Title: "Example", Monitored: true, HasFile: true}
	radarrClient := &radarrStub{movies: []radarr.Movie{movie}}
	cfg := &cleanupConfigStub{cfg: config.Config{
		Cleanup:   config.CleanupConfig{Enabled: true, DelayDays: 3},
		Scheduler: config.SchedulerConfig{DryRun: true},
	}}
	service, err := newService(nil, radarrClient, nil, &cleanupTraktStub{}, nil, cfg, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	queue := service.queues[MediaTypeMovie]
	if _, err := queue.Add(&QueueItem{ID: movie.ID, ExternalID: movie.TmdbID, Title: movie.Title, Reason: "watched"}); err != nil {
		t.Fatal(err)
	}

	if _, err := service.ProcessCleanup(context.Background()); err != nil {
		t.Fatal(err)
	}
	if radarrClient.unmonitorCalls != 0 || queue.Get(movie.ID).UnmonitoredAt != nil {
		t.Fatalf("dry run unmonitored %d times; candidate = %#v", radarrClient.unmonitorCalls, queue.Get(movie.ID))
	}
}

func TestProcessCleanupMatchesExcludedEmbyLibraryCaseInsensitively(t *testing.T) {
	logger.Init(true)
	radarrClient := &radarrStub{}
	embyClient := &embyStub{
		libraries: []emby.VirtualFolder{{Name: "Movies", ItemID: "library", CollectionType: "movies"}},
		movies:    []emby.Item{{ID: "7", Name: "Example", ProviderIDs: emby.ProviderIDs{Tmdb: "70"}}},
	}
	traktClient := &cleanupTraktStub{watchedMovies: []trakt.WatchedMovie{{Movie: trakt.Movie{IDs: trakt.IDs{TMDB: 70}}}}}
	cfg := &cleanupConfigStub{cfg: config.Config{
		Cleanup: config.CleanupConfig{Enabled: true, DelayDays: 1},
		Emby:    config.EmbyConfig{Enabled: true, ExcludedLibraries: []string{"movies"}},
	}}
	service, err := newService(nil, radarrClient, embyClient, traktClient, nil, cfg, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	if _, err := service.ProcessCleanup(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := len(service.GetAllQueues()); got != 0 {
		t.Fatalf("case-insensitive library exclusion created %d Removal Candidates", got)
	}
}

func TestProcessCleanupRemovesReadyEmbyCandidateWhenLibraryIsEmpty(t *testing.T) {
	logger.Init(true)
	radarrClient := &radarrStub{}
	embyClient := &embyStub{}
	cfg := &cleanupConfigStub{cfg: config.Config{
		Cleanup: config.CleanupConfig{Enabled: true, DelayDays: 1},
		Emby:    config.EmbyConfig{Enabled: true},
	}}
	service, err := newService(nil, radarrClient, embyClient, &cleanupTraktStub{}, nil, cfg, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	queue := service.queues[MediaTypeEmbyMovie]
	now := time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC)
	queue.now = func() time.Time { return now }
	if _, err := queue.Add(&QueueItem{ID: 7, ExternalID: 70, Title: "Example", Reason: "watched"}); err != nil {
		t.Fatal(err)
	}
	now = now.Add(24 * time.Hour)

	if _, err := service.ProcessCleanup(context.Background()); err != nil {
		t.Fatal(err)
	}
	if embyClient.deleteCalls != 1 || queue.Get(7) != nil {
		t.Fatalf("delete calls = %d, candidate = %#v; want ready candidate removed", embyClient.deleteCalls, queue.Get(7))
	}
}

func TestProcessCleanupDryRunDoesNotCreateEmbyRemovalCandidate(t *testing.T) {
	logger.Init(true)
	radarrClient := &radarrStub{}
	embyClient := &embyStub{
		libraries: []emby.VirtualFolder{{Name: "Movies", ItemID: "library", CollectionType: "movies"}},
		movies:    []emby.Item{{ID: "7", Name: "Example", ProviderIDs: emby.ProviderIDs{Tmdb: "70"}}},
	}
	traktClient := &cleanupTraktStub{watchedMovies: []trakt.WatchedMovie{{
		LastWatchedAt: time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC),
		Movie:         trakt.Movie{IDs: trakt.IDs{TMDB: 70}},
	}}}
	cfg := &cleanupConfigStub{cfg: config.Config{
		Cleanup:   config.CleanupConfig{Enabled: true, DelayDays: 3},
		Scheduler: config.SchedulerConfig{DryRun: true},
		Emby:      config.EmbyConfig{Enabled: true},
	}}
	service, err := newService(nil, radarrClient, embyClient, traktClient, nil, cfg, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	if _, err := service.ProcessCleanup(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := len(service.GetAllQueues()); got != 0 {
		t.Fatalf("dry run created %d Removal Candidates, want 0", got)
	}
	if embyClient.deleteCalls != 0 {
		t.Fatalf("dry run deleted %d Emby items", embyClient.deleteCalls)
	}
}

func TestProcessCleanupDryRunDoesNotCreateRemovalCandidate(t *testing.T) {
	logger.Init(true)
	movie := radarr.Movie{ID: 7, TmdbID: 70, Title: "Example", Monitored: true, HasFile: true}
	radarrClient := &radarrStub{movies: []radarr.Movie{movie}}
	traktClient := &cleanupTraktStub{watchedMovies: []trakt.WatchedMovie{{
		LastWatchedAt: time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC),
		Movie:         trakt.Movie{IDs: trakt.IDs{TMDB: 70}},
	}}}
	cfg := &cleanupConfigStub{cfg: config.Config{
		Cleanup:   config.CleanupConfig{Enabled: true, DelayDays: 3},
		Scheduler: config.SchedulerConfig{DryRun: true},
	}}
	service, err := newService(nil, radarrClient, nil, traktClient, nil, cfg, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	result, err := service.ProcessCleanup(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got := len(service.GetAllQueues()); got != 0 {
		t.Fatalf("dry run created %d Removal Candidates, want 0", got)
	}
	if radarrClient.unmonitorCalls != 0 || radarrClient.deleteCalls != 0 {
		t.Fatalf("dry run called unmonitor %d times and delete %d times", radarrClient.unmonitorCalls, radarrClient.deleteCalls)
	}
	if len(result.Results) != 1 || result.Results[0].Action != "queued" {
		t.Fatalf("dry-run result = %#v, want one would-queue result", result.Results)
	}
}
