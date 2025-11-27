package trakt

import (
	"context"
	"fmt"
	"time"

	"github.com/fusionn-air/internal/config"
	"github.com/fusionn-air/pkg/logger"
	"github.com/go-resty/resty/v2"
)

type Client struct {
	client   *resty.Client
	auth     *AuthManager
	clientID string
}

func NewClient(cfg config.TraktConfig) *Client {
	client := resty.New().
		SetBaseURL(cfg.BaseURL).
		SetTimeout(30*time.Second).
		SetHeader("Content-Type", "application/json").
		SetHeader("trakt-api-version", "2").
		SetHeader("trakt-api-key", cfg.ClientID).
		SetRetryCount(3).
		SetRetryWaitTime(1 * time.Second).
		SetRetryMaxWaitTime(5 * time.Second).
		AddRetryCondition(func(r *resty.Response, err error) bool {
			return err != nil || r.StatusCode() >= 500
		})

	auth := NewAuthManager(cfg.ClientID, cfg.ClientSecret, cfg.BaseURL)

	return &Client{
		client:   client,
		auth:     auth,
		clientID: cfg.ClientID,
	}
}

// Initialize performs OAuth authentication if needed
func (c *Client) Initialize(ctx context.Context) error {
	if err := c.auth.Initialize(ctx); err != nil {
		return fmt.Errorf("trakt auth: %w", err)
	}

	c.client.SetAuthToken(c.auth.GetAccessToken())
	return nil
}

// ensureAuth checks and refreshes token before requests
func (c *Client) ensureAuth(ctx context.Context) error {
	if err := c.auth.EnsureValidToken(ctx); err != nil {
		return err
	}
	c.client.SetAuthToken(c.auth.GetAccessToken())
	return nil
}

// GetMyShowsCalendar returns upcoming episodes for shows the user has watched
func (c *Client) GetMyShowsCalendar(ctx context.Context, days int) ([]CalendarShow, error) {
	if err := c.ensureAuth(ctx); err != nil {
		return nil, fmt.Errorf("auth: %w", err)
	}

	if days <= 0 {
		days = 7
	}
	if days > 33 {
		days = 33
	}

	today := time.Now().Format("2006-01-02")
	path := fmt.Sprintf("/calendars/my/shows/%s/%d", today, days)

	var shows []CalendarShow
	resp, err := c.client.R().
		SetContext(ctx).
		SetResult(&shows).
		Get(path)

	if err != nil {
		return nil, fmt.Errorf("getting calendar: %w", err)
	}

	if resp.IsError() {
		return nil, fmt.Errorf("API error: status=%d", resp.StatusCode())
	}

	return shows, nil
}

// GetShowProgress returns detailed watch progress for a specific show
func (c *Client) GetShowProgress(ctx context.Context, showID int) (*ShowProgress, error) {
	if err := c.ensureAuth(ctx); err != nil {
		return nil, fmt.Errorf("auth: %w", err)
	}

	path := fmt.Sprintf("/shows/%d/progress/watched", showID)

	var progress ShowProgress
	resp, err := c.client.R().
		SetContext(ctx).
		SetResult(&progress).
		Get(path)

	if err != nil {
		return nil, fmt.Errorf("getting show progress: %w", err)
	}

	if resp.IsError() {
		return nil, fmt.Errorf("API error: status=%d", resp.StatusCode())
	}

	return &progress, nil
}

// GetWatchedShows returns all shows the user has watched with episode details
func (c *Client) GetWatchedShows(ctx context.Context) ([]WatchedShow, error) {
	if err := c.ensureAuth(ctx); err != nil {
		return nil, fmt.Errorf("auth: %w", err)
	}

	var shows []WatchedShow
	resp, err := c.client.R().
		SetContext(ctx).
		SetResult(&shows).
		Get("/users/me/watched/shows")

	if err != nil {
		return nil, fmt.Errorf("getting watched shows: %w", err)
	}

	if resp.IsError() {
		return nil, fmt.Errorf("API error: status=%d", resp.StatusCode())
	}

	logger.Debugf("Fetched %d watched shows from Trakt", len(shows))
	return shows, nil
}

// IsSeasonComplete checks if all aired episodes in a season have been watched
func (c *Client) IsSeasonComplete(progress *ShowProgress, seasonNum int) bool {
	for _, season := range progress.Seasons {
		if season.Number == seasonNum {
			return season.Aired > 0 && season.Completed == season.Aired
		}
	}
	return false
}

// GetLastCompletedSeason returns the highest season number that's fully watched
func (c *Client) GetLastCompletedSeason(progress *ShowProgress) int {
	lastCompleted := 0
	for _, season := range progress.Seasons {
		if season.Number == 0 {
			continue
		}
		if season.Aired > 0 && season.Completed == season.Aired {
			if season.Number > lastCompleted {
				lastCompleted = season.Number
			}
		}
	}
	return lastCompleted
}
