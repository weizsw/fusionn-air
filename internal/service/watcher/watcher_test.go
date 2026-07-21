package watcher

import (
	"context"
	"testing"

	"github.com/fusionn-air/internal/client/trakt"
	"github.com/fusionn-air/internal/config"
)

func TestExclusionReasonRejectsConfiguredGenre(t *testing.T) {
	show := trakt.Show{Genres: []string{"Drama", "Animation"}, Language: "ja"}
	cfg := config.WatcherConfig{
		ExcludedGenres:   []string{"animation"},
		AllowedLanguages: []string{"en"},
	}

	if got := exclusionReason(show, cfg); got != "excluded genre: Animation" {
		t.Fatalf("exclusionReason() = %q, want %q", got, "excluded genre: Animation")
	}

	show.Language = "en"
	cfg.ExcludedGenres = []string{"anim"}
	if got := exclusionReason(show, cfg); got != "" {
		t.Fatalf("exclusionReason() with partial genre = %q, want eligible series", got)
	}
}

func TestExclusionReasonRejectsDisallowedOriginalLanguage(t *testing.T) {
	show := trakt.Show{Genres: []string{"drama"}, Language: "ko"}
	cfg := config.WatcherConfig{AllowedLanguages: []string{"en"}}

	if got := exclusionReason(show, cfg); got != "language not allowed: ko" {
		t.Fatalf("exclusionReason() = %q, want %q", got, "language not allowed: ko")
	}

	show.Language = "EN"
	if got := exclusionReason(show, cfg); got != "" {
		t.Fatalf("exclusionReason() = %q, want eligible English series", got)
	}
}

func TestExclusionReasonRejectsUnknownGenresWhenFilterEnabled(t *testing.T) {
	show := trakt.Show{Language: "en"}
	cfg := config.WatcherConfig{ExcludedGenres: []string{"animation"}}

	if got := exclusionReason(show, cfg); got != "genres unknown" {
		t.Fatalf("exclusionReason() = %q, want %q", got, "genres unknown")
	}
	if got := exclusionReason(show, config.WatcherConfig{}); got != "" {
		t.Fatalf("exclusionReason() with disabled filters = %q, want eligible series", got)
	}
}

func TestExclusionReasonRejectsUnknownLanguageWhenFilterEnabled(t *testing.T) {
	show := trakt.Show{Genres: []string{"drama"}}
	cfg := config.WatcherConfig{AllowedLanguages: []string{"en"}}

	if got := exclusionReason(show, cfg); got != "language unknown" {
		t.Fatalf("exclusionReason() = %q, want %q", got, "language unknown")
	}
}

func TestProcessShowSkipsExcludedSeriesBeforeAPIRequests(t *testing.T) {
	service := &Service{}
	item := calendarItem{show: trakt.Show{Genres: []string{"animation"}, Language: "en"}}
	cfg := config.WatcherConfig{ExcludedGenres: []string{"animation"}}

	result := service.processShow(context.Background(), item, false, cfg)

	if result.Action != "skipped" || result.Reason != "excluded genre: animation" {
		t.Fatalf("processShow() = action %q, reason %q", result.Action, result.Reason)
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
