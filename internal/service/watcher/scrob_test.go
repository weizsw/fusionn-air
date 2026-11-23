package watcher

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/fusionn-air/internal/client/overseerr"
	"github.com/fusionn-air/internal/client/scrob"
	"github.com/fusionn-air/internal/config"
)

func TestScrobWatcherDryRun(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.Header.Get("X-Api-Key") != "test-key" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/proxy/calendar":
			fmt.Fprint(w, `{"calendar":{"today":"2026-01-10","entries":[
				{"show_tmdb_id":1,"season_number":2,"episode_number":1,"air_date":"2026-01-11"},
				{"show_tmdb_id":1,"season_number":2,"episode_number":2,"air_date":"2026-01-12"},
				{"show_tmdb_id":2,"season_number":2,"episode_number":1,"air_date":"2026-01-12"},
				{"show_tmdb_id":3,"season_number":2,"episode_number":1,"air_date":"2026-01-09"},
				{"show_tmdb_id":4,"season_number":2,"episode_number":1,"air_date":"2026-01-12"}
			]}}`)
		case "/api/proxy/history":
			if r.URL.Query().Get("type") != "episode" {
				t.Errorf("history was not filtered to episodes")
			}
			switch r.URL.Query().Get("page") {
			case "1":
				fmt.Fprint(w, `{"page":1,"total_pages":2,"results":[
					{"completed":true,"source":"trakt","media":{"show_tmdb_id":1,"season_number":1,"episode_number":1}},
					{"completed":true,"media":{"show_tmdb_id":2,"season_number":1,"episode_number":1}},
					{"completed":true,"media":{"show_tmdb_id":2,"season_number":1,"episode_number":3}},
					{"completed":false,"media":{"show_tmdb_id":2,"season_number":1,"episode_number":2}},
					{"completed":true,"media":{"show_tmdb_id":2,"season_number":1,"episode_number":2,"tvdb_sourced":true}}
				]}`)
			case "2":
				fmt.Fprint(w, `{"page":2,"total_pages":2,"results":[
					{"completed":true,"source":"manual","media":{"show_tmdb_id":1,"season_number":1,"episode_number":2}},
					{"completed":true,"media":{"show_tmdb_id":4,"season_number":2,"episode_number":1}}
				]}`)
			default:
				t.Errorf("unexpected history page: %s", r.URL.RawQuery)
				w.WriteHeader(http.StatusBadRequest)
			}
		case "/api/v1/tv/1/season/1", "/api/v1/tv/2/season/1":
			fmt.Fprint(w, `{"episodes":[
				{"episodeNumber":1,"airDate":"2025-12-20"},
				{"episodeNumber":2,"airDate":"2026-01-10"},
				{"episodeNumber":3,"airDate":"2026-01-21"}
			]}`)
		case "/api/v1/tv/1":
			fmt.Fprint(w, `{"id":1,"genres":[{"name":"Animation"}],"originCountry":["JP"],"originalLanguage":"ja"}`)
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(`
scheduler:
  dry_run: true
watcher:
  calendar_days: 14
  routing:
    alternate_genres: [anime]
    alternate_server_id: 2
`), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.NewManager(path)
	if err != nil {
		t.Fatal(err)
	}
	defer cfg.Stop()

	client, err := scrob.NewClient(config.ScrobConfig{BaseURL: server.URL, APIKey: "test-key"})
	if err != nil {
		t.Fatal(err)
	}
	seerr := overseerr.NewClient(config.OverseerrConfig{BaseURL: server.URL, APIKey: "test-key"})
	service := NewService(NewScrobSource(client, seerr), seerr, nil, cfg)
	results, err := service.ProcessCalendar(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 3 {
		t.Fatalf("got %d results, want 3", len(results))
	}
	byShow := make(map[int]ProcessResult)
	for _, result := range results {
		byShow[result.ShowTMDB] = result
	}
	if got := byShow[1]; got.Action != "dry_run" || got.Route != "alternate" || got.Episode != 1 {
		t.Errorf("eligible show: got %+v", got)
	}
	if got := byShow[2]; got.Action != "skipped" || got.Reason != "S01 incomplete (1/2 aired episodes watched)" {
		t.Errorf("incomplete show: got %+v", got)
	}
	if got := byShow[4]; got.Action != "skipped" || got.Reason != "already watched episodes in S02" {
		t.Errorf("started show: got %+v", got)
	}

	filtered := cfg.Get()
	filtered.Watcher.AllowedLanguages = []string{"en"}
	service = newService(NewScrobSource(client, seerr), seerr, nil, &watcherConfigStub{cfg: *filtered})
	results, err = service.ProcessCalendar(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, result := range results {
		if result.ShowTMDB == 1 {
			if result.Action != "skipped" || result.Reason != "language not allowed: ja" {
				t.Fatalf("Scrob original language filter: got %+v", result)
			}
			return
		}
	}
	t.Fatal("Scrob candidate was missing from the filtered run")
}
