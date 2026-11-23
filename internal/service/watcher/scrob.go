package watcher

import (
	"context"
	"fmt"
	"time"

	"github.com/fusionn-air/internal/client/scrob"
)

type scrobSource struct {
	client *scrob.Client
}

func NewScrobSource(client *scrob.Client) Source {
	return &scrobSource{client: client}
}

func (s *scrobSource) upcoming(ctx context.Context, days int) ([]calendarItem, error) {
	calendar, err := s.client.GetCalendar(ctx, days)
	if err != nil {
		return nil, err
	}
	today, err := time.Parse(time.DateOnly, calendar.Today)
	if err != nil {
		return nil, fmt.Errorf("scrob calendar: invalid date: %w", err)
	}

	var items []calendarItem
	for _, entry := range calendar.Entries {
		airDate, err := time.Parse(time.DateOnly, entry.AirDate)
		if err != nil || airDate.Before(today) || entry.ShowTMDBID <= 0 || entry.Season < 1 || entry.Episode < 1 {
			continue
		}
		items = append(items, calendarItem{
			tmdbID: entry.ShowTMDBID, title: entry.ShowTitle, season: entry.Season,
			episode: entry.Episode, airDate: airDate, routeWithOverseerr: true,
		})
	}
	if len(items) == 0 {
		return nil, nil
	}

	episodes, err := s.client.GetWatchedEpisodes(ctx)
	if err != nil {
		return nil, err
	}
	watched := make(map[int]map[int]map[int]bool)
	for _, episode := range episodes {
		seasons := watched[episode.ShowTMDBID]
		if seasons == nil {
			seasons = make(map[int]map[int]bool)
			watched[episode.ShowTMDBID] = seasons
		}
		if seasons[*episode.Season] == nil {
			seasons[*episode.Season] = make(map[int]bool)
		}
		seasons[*episode.Season][episode.Number] = true
	}
	candidates := items[:0]
	for _, item := range items {
		showHistory := watched[item.tmdbID]
		if len(showHistory) == 0 {
			continue
		}
		tmdbID, season := item.tmdbID, item.season
		item.eligible = func(ctx context.Context) (bool, string, error) {
			return s.eligible(ctx, tmdbID, season, showHistory, today)
		}
		candidates = append(candidates, item)
	}
	return candidates, nil
}

func (s *scrobSource) eligible(ctx context.Context, tmdbID, season int, watched map[int]map[int]bool, today time.Time) (bool, string, error) {
	if len(watched[season]) > 0 {
		return false, fmt.Sprintf("already watched episodes in S%02d", season), nil
	}
	if season == 1 {
		return false, "no S01 watch history", nil
	}
	previous := season - 1
	previousWatched := watched[previous]
	if len(previousWatched) == 0 {
		return false, fmt.Sprintf("S%02d not watched", previous), nil
	}

	details, err := s.client.GetSeason(ctx, tmdbID, previous)
	if err != nil {
		return false, "", err
	}
	// The season parameter can refer to a different grouping in other episode orders.
	if details.EpisodeOrder != "tmdb:aired" || details.ShowTMDBID != tmdbID || details.Number != previous {
		return false, fmt.Sprintf("S%02d TMDB aired-order metadata unavailable", previous), nil
	}

	total := 0
	for _, summary := range details.Show.Seasons {
		if summary.Number == previous {
			total = summary.EpisodeCount
			break
		}
	}
	if total <= 0 {
		return false, fmt.Sprintf("S%02d total episode count unknown", previous), nil
	}
	if len(details.Episodes) != total {
		return false, fmt.Sprintf("S%02d episode list incomplete (%d/%d entries)", previous, len(details.Episodes), total), nil
	}
	seen := make(map[int]bool, total)
	aired, completed := 0, 0
	for _, episode := range details.Episodes {
		if episode.Season != previous || episode.Number < 1 || episode.Number > total || seen[episode.Number] {
			return false, fmt.Sprintf("S%02d episode numbering inconsistent", previous), nil
		}
		seen[episode.Number] = true
		airDate, _ := time.Parse(time.DateOnly, episode.AirDate)
		if airDate.IsZero() {
			return false, fmt.Sprintf("S%02dE%02d air date unknown", previous, episode.Number), nil
		}
		if airDate.After(today) {
			continue
		}
		aired++
		if previousWatched[episode.Number] {
			completed++
		}
	}
	if aired < total {
		return false, fmt.Sprintf("S%02d ongoing (%d/%d episodes aired)", previous, aired, total), nil
	}
	if completed < total {
		return false, fmt.Sprintf("S%02d incomplete (%d/%d episodes watched)", previous, completed, total), nil
	}
	return true, fmt.Sprintf("S%02d complete", previous), nil
}
