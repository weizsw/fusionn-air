package watcher

import (
	"context"
	"fmt"
	"time"

	"github.com/fusionn-air/internal/client/overseerr"
	"github.com/fusionn-air/internal/client/scrob"
)

type scrobSource struct {
	client    *scrob.Client
	overseerr *overseerr.Client
}

func NewScrobSource(client *scrob.Client, overseerrClient *overseerr.Client) Source {
	return &scrobSource{client: client, overseerr: overseerrClient}
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
	for i, item := range items {
		items[i].eligible = func(ctx context.Context) (bool, string, error) {
			return s.eligible(ctx, item, watched[item.tmdbID], today)
		}
	}
	return items, nil
}

func (s *scrobSource) eligible(ctx context.Context, item calendarItem, watched map[int]map[int]bool, today time.Time) (bool, string, error) {
	if len(watched[item.season]) > 0 {
		return false, fmt.Sprintf("already watched episodes in S%02d", item.season), nil
	}
	if item.season == 1 {
		if len(watched[0]) > 0 {
			return true, "watched specials only", nil
		}
		return false, "no watch history", nil
	}
	previous := item.season - 1
	if len(watched[previous]) == 0 {
		return false, fmt.Sprintf("S%02d not watched", previous), nil
	}

	episodes, err := s.overseerr.GetTVSeasonEpisodes(ctx, item.tmdbID, previous)
	if err != nil {
		return false, "", err
	}
	aired, completed := 0, 0
	for _, episode := range episodes {
		airDate, err := time.Parse(time.DateOnly, episode.AirDate)
		if err != nil || airDate.After(today) || episode.Number < 1 {
			continue
		}
		aired++
		if watched[previous][episode.Number] {
			completed++
		}
	}
	if aired == 0 {
		return false, fmt.Sprintf("S%02d not aired", previous), nil
	}
	if completed < aired {
		return false, fmt.Sprintf("S%02d incomplete (%d/%d aired episodes watched)", previous, completed, aired), nil
	}
	return true, fmt.Sprintf("S%02d complete", previous), nil
}
