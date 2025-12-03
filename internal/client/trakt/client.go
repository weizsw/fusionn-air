package trakt

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/go-resty/resty/v2"
	"golang.org/x/time/rate"

	"github.com/fusionn-air/internal/config"
	"github.com/fusionn-air/pkg/logger"
)

// Rate limits per Trakt API docs:
// - POST/PUT/DELETE: 1 call per second
// - GET: 1000 calls per 5 minutes (~3.33/sec)
// After token refresh, stricter limits may apply temporarily
const (
	defaultGetRate  = 3                      // Conservative GET rate (3/sec vs 3.33 limit)
	burstSize       = 3                      // Conservative burst
	minRequestDelay = 350 * time.Millisecond // Min delay between requests
)

type Client struct {
	client   *resty.Client
	auth     *AuthManager
	clientID string

	// Rate limiter
	getLimiter *rate.Limiter

	// Adaptive rate limiting based on headers
	mu          sync.Mutex
	retryAfter  time.Time // When we can make requests again after 429
	lastRequest time.Time
}

func NewClient(cfg config.TraktConfig) *Client {
	c := &Client{
		clientID:   cfg.ClientID,
		getLimiter: rate.NewLimiter(rate.Limit(defaultGetRate), burstSize),
	}

	client := resty.New().
		SetBaseURL(cfg.BaseURL).
		SetTimeout(30*time.Second).
		SetHeader("Content-Type", "application/json").
		SetHeader("trakt-api-version", "2").
		SetHeader("trakt-api-key", cfg.ClientID).
		SetRetryCount(3).
		SetRetryWaitTime(2 * time.Second).
		SetRetryMaxWaitTime(30 * time.Second).
		AddRetryCondition(func(r *resty.Response, err error) bool {
			// Retry on 5xx errors only; handle 429 ourselves
			return err != nil || r.StatusCode() >= 500
		}).
		OnAfterResponse(func(_ *resty.Client, resp *resty.Response) error {
			// Handle rate limit headers
			c.handleRateLimitHeaders(resp)
			return nil
		})

	c.client = client
	c.auth = NewAuthManager(cfg.ClientID, cfg.ClientSecret, cfg.BaseURL)

	return c
}

// handleRateLimitHeaders processes rate limit info from response
func (c *Client) handleRateLimitHeaders(resp *resty.Response) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Check for Retry-After header (seconds until we can retry)
	if retryAfter := resp.Header().Get("Retry-After"); retryAfter != "" {
		if seconds, err := strconv.Atoi(retryAfter); err == nil {
			c.retryAfter = time.Now().Add(time.Duration(seconds) * time.Second)
			logger.Warnf("Trakt rate limit: retry after %d seconds", seconds)
		}
	}

	// Log remaining requests if available
	if remaining := resp.Header().Get("X-Ratelimit-Remaining"); remaining != "" {
		if rem, err := strconv.Atoi(remaining); err == nil && rem < 50 {
			logger.Debugf("Trakt rate limit: %d requests remaining", rem)
		}
	}
}

// waitForRate waits for rate limiter and any retry-after period
func (c *Client) waitForRate(ctx context.Context, _ bool) error {
	c.mu.Lock()
	retryAfter := c.retryAfter
	lastReq := c.lastRequest
	c.mu.Unlock()

	// Wait for retry-after period if we hit 429
	if time.Now().Before(retryAfter) {
		waitTime := time.Until(retryAfter)
		logger.Debugf("Waiting %v for rate limit reset", waitTime.Round(time.Second))
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(waitTime):
		}
	}

	// Ensure minimum delay between requests
	if elapsed := time.Since(lastReq); elapsed < minRequestDelay {
		time.Sleep(minRequestDelay - elapsed)
	}

	// Use token bucket rate limiter (GET only for now)
	if err := c.getLimiter.Wait(ctx); err != nil {
		return err
	}

	c.mu.Lock()
	c.lastRequest = time.Now()
	c.mu.Unlock()

	return nil
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

	if err := c.waitForRate(ctx, false); err != nil {
		return nil, fmt.Errorf("rate limit: %w", err)
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

	if err := c.waitForRate(ctx, false); err != nil {
		return nil, fmt.Errorf("rate limit: %w", err)
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

	if err := c.waitForRate(ctx, false); err != nil {
		return nil, fmt.Errorf("rate limit: %w", err)
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

// GetShowSeasons returns season summaries including total episode counts
func (c *Client) GetShowSeasons(ctx context.Context, showID int) ([]SeasonSummary, error) {
	if err := c.ensureAuth(ctx); err != nil {
		return nil, fmt.Errorf("auth: %w", err)
	}

	if err := c.waitForRate(ctx, false); err != nil {
		return nil, fmt.Errorf("rate limit: %w", err)
	}

	path := fmt.Sprintf("/shows/%d/seasons?extended=full", showID)

	var seasons []SeasonSummary
	resp, err := c.client.R().
		SetContext(ctx).
		SetResult(&seasons).
		Get(path)

	if err != nil {
		return nil, fmt.Errorf("getting show seasons: %w", err)
	}

	if resp.IsError() {
		return nil, fmt.Errorf("API error: status=%d", resp.StatusCode())
	}

	return seasons, nil
}

// GetWatchedMovies returns all movies the user has watched
func (c *Client) GetWatchedMovies(ctx context.Context) ([]WatchedMovie, error) {
	if err := c.ensureAuth(ctx); err != nil {
		return nil, fmt.Errorf("auth: %w", err)
	}

	if err := c.waitForRate(ctx, false); err != nil {
		return nil, fmt.Errorf("rate limit: %w", err)
	}

	var movies []WatchedMovie
	resp, err := c.client.R().
		SetContext(ctx).
		SetResult(&movies).
		Get("/users/me/watched/movies")

	if err != nil {
		return nil, fmt.Errorf("getting watched movies: %w", err)
	}

	if resp.IsError() {
		return nil, fmt.Errorf("API error: status=%d", resp.StatusCode())
	}

	logger.Debugf("Fetched %d watched movies from Trakt", len(movies))
	return movies, nil
}
