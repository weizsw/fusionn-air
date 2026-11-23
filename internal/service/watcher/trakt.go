package watcher

import (
	"context"
	"fmt"

	"github.com/fusionn-air/internal/client/trakt"
)

type traktAdapter interface {
	GetMyShowsCalendar(context.Context, int) ([]trakt.CalendarShow, error)
	GetShowProgress(context.Context, int) (*trakt.ShowProgress, error)
	GetShowSeasons(context.Context, int) ([]trakt.SeasonSummary, error)
}

type traktSource struct {
	client traktAdapter
}

func NewTraktSource(client *trakt.Client) Source {
	return &traktSource{client: client}
}

func (s *traktSource) upcoming(ctx context.Context, days int) ([]calendarItem, error) {
	entries, err := s.client.GetMyShowsCalendar(ctx, days)
	if err != nil {
		return nil, err
	}
	items := make([]calendarItem, 0, len(entries))
	for _, entry := range entries {
		if entry.Episode.Season < 1 || entry.Episode.Number < 1 {
			continue
		}
		items = append(items, calendarItem{
			tmdbID: entry.Show.IDs.TMDB, title: entry.Show.Title, season: entry.Episode.Season,
			episode: entry.Episode.Number, airDate: entry.FirstAired, metadataBeforeProgress: true,
			eligible: func(ctx context.Context) (bool, string, error) {
				progress, err := s.client.GetShowProgress(ctx, entry.Show.IDs.Trakt)
				if err != nil {
					return false, "", err
				}
				seasons, err := s.client.GetShowSeasons(ctx, entry.Show.IDs.Trakt)
				if err != nil {
					return false, "", fmt.Errorf("getting Trakt season counts: %w", err)
				}
				ok, reason := shouldRequestSeason(progress, seasons, entry.Episode.Season)
				return ok, reason, nil
			},
		})
	}
	return items, nil
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
