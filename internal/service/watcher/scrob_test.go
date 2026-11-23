package watcher

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

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
				{"show_tmdb_id":4,"season_number":2,"episode_number":1,"air_date":"2026-01-12"},
				{"show_tmdb_id":5,"season_number":2,"episode_number":1,"air_date":"2026-01-12"},
				{"show_tmdb_id":6,"season_number":1,"episode_number":1,"air_date":"2026-01-12"}
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
					{"completed":true,"media":{"show_tmdb_id":3,"season_number":1,"episode_number":1}},
					{"completed":true,"media":{"show_tmdb_id":4,"season_number":2,"episode_number":1}},
					{"completed":false,"media":{"show_tmdb_id":5,"season_number":1,"episode_number":1}},
					{"completed":true,"media":{"show_tmdb_id":6,"season_number":0,"episode_number":1}}
				]}`)
			default:
				t.Errorf("unexpected history page: %s", r.URL.RawQuery)
				w.WriteHeader(http.StatusBadRequest)
			}
		case "/api/proxy/shows/1/season/1", "/api/proxy/shows/2/season/1":
			var tmdbID int
			if _, err := fmt.Sscanf(r.URL.Path, "/api/proxy/shows/%d/season/1", &tmdbID); err != nil {
				t.Errorf("invalid season path: %v", err)
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			fmt.Fprintf(w, `{"tmdb_id":%d,"season_number":1,"episode_order":"tmdb:aired",
				"show":{"seasons_meta":[{"season_number":1,"episode_count":2}]},"episodes":[
				{"canonical_season_number":1,"canonical_episode_number":1,"air_date":"2025-12-20"},
				{"canonical_season_number":1,"canonical_episode_number":2,"air_date":"2026-01-10"}
			]}`, tmdbID)
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
	service := NewService(NewScrobSource(client), seerr, nil, cfg)
	results, err := service.ProcessCalendar(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 4 {
		t.Fatalf("got %d results, want 4", len(results))
	}
	byShow := make(map[int]ProcessResult)
	for _, result := range results {
		byShow[result.ShowTMDB] = result
	}
	if got := byShow[1]; got.Action != "dry_run" || got.Route != "alternate" || got.Episode != 1 {
		t.Errorf("eligible show: got %+v", got)
	}
	if got := byShow[2]; got.Action != "skipped" || got.Reason != "S01 incomplete (1/2 episodes watched)" {
		t.Errorf("incomplete show: got %+v", got)
	}
	if got := byShow[4]; got.Action != "skipped" || got.Reason != "already watched episodes in S02" {
		t.Errorf("started show: got %+v", got)
	}
	if _, found := byShow[5]; found {
		t.Error("show with only incomplete history became a candidate")
	}
	if got := byShow[6]; got.Action != "skipped" || got.Reason != "no S01 watch history" {
		t.Errorf("specials-only show: got %+v", got)
	}

	filtered := cfg.Get()
	filtered.Watcher.AllowedLanguages = []string{"en"}
	service = newService(NewScrobSource(client), seerr, nil, &watcherConfigStub{cfg: *filtered})
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

func TestScrobSkipsSeasonWhenPreviousScheduleIsUnverified(t *testing.T) {
	const airedEpisode = `{"canonical_season_number":1,"canonical_episode_number":1,"air_date":"2026-01-01"}`
	tests := []struct {
		name     string
		order    string
		total    int
		episodes string
		want     string
	}{
		{
			name: "ongoing", total: 2,
			episodes: airedEpisode + `,{"canonical_season_number":1,"canonical_episode_number":2,"air_date":"2026-01-15"}`,
			want:     "S01 ongoing (1/2 episodes aired)",
		},
		{
			name: "unknown total", total: 0,
			want: "S01 total episode count unknown",
		},
		{
			name: "unknown air date", total: 2,
			episodes: airedEpisode + `,{"canonical_season_number":1,"canonical_episode_number":2,"air_date":null}`,
			want:     "S01E02 air date unknown",
		},
		{
			name: "missing episode", total: 2, episodes: airedEpisode,
			want: "S01 episode list incomplete (1/2 entries)",
		},
		{
			name: "duplicate episode", total: 2, episodes: airedEpisode + "," + airedEpisode,
			want: "S01 episode numbering inconsistent",
		},
		{
			name: "different canonical season", total: 1,
			episodes: `{"canonical_season_number":2,"canonical_episode_number":1,"air_date":"2026-01-01"}`,
			want:     "S01 episode numbering inconsistent",
		},
		{
			name: "alternate episode order", order: "tvdb:dvd", total: 1, episodes: airedEpisode,
			want: "S01 TMDB aired-order metadata unavailable",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			order := test.order
			if order == "" {
				order = "tmdb:aired"
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/api/proxy/shows/1/season/1" {
					t.Errorf("unexpected path: %s", r.URL.Path)
					w.WriteHeader(http.StatusNotFound)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				fmt.Fprintf(w, `{"tmdb_id":1,"season_number":1,"episode_order":%q,
					"show":{"seasons_meta":[{"season_number":1,"episode_count":%d}]},"episodes":[%s]}`,
					order, test.total, test.episodes)
			}))
			defer server.Close()

			client, err := scrob.NewClient(config.ScrobConfig{BaseURL: server.URL, APIKey: "fixture"})
			if err != nil {
				t.Fatal(err)
			}
			source := scrobSource{client: client}
			today := time.Date(2026, time.January, 10, 0, 0, 0, 0, time.UTC)
			allowed, reason, err := source.eligible(context.Background(), 1, 2, map[int]map[int]bool{1: {1: true}}, today)
			if err != nil || allowed || reason != test.want {
				t.Fatalf("allowed = %t, reason = %q, err = %v, want skip %q", allowed, reason, err, test.want)
			}
		})
	}
}
