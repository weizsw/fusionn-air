package watcher

import (
	"testing"

	"github.com/fusionn-air/internal/client/trakt"
)

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
