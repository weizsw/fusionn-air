package scrob

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/go-resty/resty/v2"

	httpclient "github.com/fusionn-air/internal/client"
	"github.com/fusionn-air/internal/config"
)

type Client struct {
	http *resty.Client
}

func NewClient(cfg config.ScrobConfig) (*Client, error) {
	baseURL, err := url.Parse(strings.TrimSpace(cfg.BaseURL))
	if err != nil || (baseURL.Scheme != "http" && baseURL.Scheme != "https") || baseURL.Hostname() == "" || baseURL.User != nil || baseURL.RawQuery != "" || baseURL.Fragment != "" {
		return nil, fmt.Errorf("scrob.base_url must be a valid HTTP or HTTPS URL")
	}
	key := strings.TrimSpace(cfg.APIKey)
	if key == "" {
		return nil, fmt.Errorf("scrob.api_key is required")
	}
	return &Client{http: httpclient.NewHTTP().
		SetBaseURL(strings.TrimRight(baseURL.String(), "/")+"/api/proxy").
		SetHeader("X-Api-Key", key).
		SetHeader("Accept", "application/json").
		SetRedirectPolicy(resty.NoRedirectPolicy()).
		SetTimeout(30 * time.Second)}, nil
}

type Calendar struct {
	Today   string          `json:"today"`
	Entries []CalendarEntry `json:"entries"`
}

type CalendarEntry struct {
	ShowTMDBID int    `json:"show_tmdb_id"`
	ShowTitle  string `json:"show_title"`
	Season     int    `json:"season_number"`
	Episode    int    `json:"episode_number"`
	AirDate    string `json:"air_date"`
}

func (c *Client) GetCalendar(ctx context.Context, days int) (Calendar, error) {
	if days <= 0 {
		days = 7
	}
	var result struct {
		Calendar Calendar `json:"calendar"`
	}
	resp, err := c.http.R().SetContext(ctx).
		SetQueryParam("days", strconv.Itoa(min(days, 90))).
		SetResult(&result).Get("/calendar")
	if err != nil {
		return Calendar{}, fmt.Errorf("scrob calendar: %w", err)
	}
	if !resp.IsSuccess() {
		return Calendar{}, fmt.Errorf("scrob calendar: HTTP %d", resp.StatusCode())
	}
	if result.Calendar.Entries == nil {
		return Calendar{}, fmt.Errorf("scrob calendar: missing entries")
	}
	return result.Calendar, nil
}

type Season struct {
	ShowTMDBID   int    `json:"tmdb_id"`
	Number       int    `json:"season_number"`
	EpisodeOrder string `json:"episode_order"`
	Episodes     []struct {
		Season  int    `json:"canonical_season_number"`
		Number  int    `json:"canonical_episode_number"`
		AirDate string `json:"air_date"`
	} `json:"episodes"`
	Show struct {
		Seasons []struct {
			Number       int `json:"season_number"`
			EpisodeCount int `json:"episode_count"`
		} `json:"seasons_meta"`
	} `json:"show"`
}

// GetSeason reads Scrob's season schedule and show metadata.
func (c *Client) GetSeason(ctx context.Context, tmdbID, season int) (Season, error) {
	var result Season
	resp, err := c.http.R().SetContext(ctx).SetResult(&result).
		Get(fmt.Sprintf("/shows/%d/season/%d", tmdbID, season))
	if err != nil {
		return Season{}, fmt.Errorf("scrob season %d: %w", season, err)
	}
	if !resp.IsSuccess() {
		return Season{}, fmt.Errorf("scrob season %d: HTTP %d", season, resp.StatusCode())
	}
	return result, nil
}

type Episode struct {
	ShowTMDBID  int  `json:"show_tmdb_id"`
	Season      *int `json:"season_number"`
	Number      int  `json:"episode_number"`
	TVDBSourced bool `json:"tvdb_sourced"`
}

func (c *Client) GetWatchedEpisodes(ctx context.Context) ([]Episode, error) {
	var watched []Episode
	for page := 1; ; page++ {
		var history struct {
			Page       int `json:"page"`
			TotalPages int `json:"total_pages"`
			Results    []struct {
				Completed bool    `json:"completed"`
				Media     Episode `json:"media"`
			} `json:"results"`
		}
		resp, err := c.http.R().SetContext(ctx).
			SetQueryParams(map[string]string{"type": "episode", "page": strconv.Itoa(page), "page_size": "100"}).
			SetResult(&history).Get("/history")
		if err != nil {
			return nil, fmt.Errorf("scrob history page %d: %w", page, err)
		}
		if !resp.IsSuccess() {
			return nil, fmt.Errorf("scrob history page %d: HTTP %d", page, resp.StatusCode())
		}
		if history.Page != page || history.TotalPages < 1 || history.Results == nil {
			return nil, fmt.Errorf("scrob history page %d: invalid page response", page)
		}
		for _, event := range history.Results {
			episode := event.Media
			// Unmapped TVDB episodes can have different season and episode numbers.
			if event.Completed && !episode.TVDBSourced && episode.ShowTMDBID > 0 && episode.Season != nil && *episode.Season >= 0 && episode.Number > 0 {
				watched = append(watched, episode)
			}
		}
		if page >= history.TotalPages {
			return watched, nil
		}
	}
}
