package overseerr

import (
	"context"
	"fmt"
	"net/url"
	"time"

	"github.com/fusionn-air/internal/config"
	"github.com/fusionn-air/pkg/logger"
	"github.com/go-resty/resty/v2"
)

type Client struct {
	client *resty.Client
}

func NewClient(cfg config.OverseerrConfig) *Client {
	client := resty.New().
		SetBaseURL(cfg.BaseURL+"/api/v1").
		SetTimeout(30*time.Second).
		SetHeader("Content-Type", "application/json").
		SetHeader("X-Api-Key", cfg.APIKey).
		SetRetryCount(3).
		SetRetryWaitTime(1 * time.Second).
		SetRetryMaxWaitTime(5 * time.Second).
		AddRetryCondition(func(r *resty.Response, err error) bool {
			return err != nil || r.StatusCode() >= 500
		})

	return &Client{client: client}
}

// SearchTV searches for a TV show by name
func (c *Client) SearchTV(ctx context.Context, query string) (*SearchResult, error) {
	var result SearchResult
	resp, err := c.client.R().
		SetContext(ctx).
		SetQueryParam("query", url.QueryEscape(query)).
		SetResult(&result).
		Get("/search")

	if err != nil {
		return nil, fmt.Errorf("searching: %w", err)
	}

	if resp.IsError() {
		return nil, fmt.Errorf("API error: status=%d", resp.StatusCode())
	}

	return &result, nil
}

// GetTVByTMDB gets TV show details by TMDB ID
func (c *Client) GetTVByTMDB(ctx context.Context, tmdbID int) (*TVDetails, error) {
	var details TVDetails
	resp, err := c.client.R().
		SetContext(ctx).
		SetResult(&details).
		Get(fmt.Sprintf("/tv/%d", tmdbID))

	if err != nil {
		return nil, fmt.Errorf("getting TV details: %w", err)
	}

	if resp.IsError() {
		return nil, fmt.Errorf("API error: status=%d", resp.StatusCode())
	}

	return &details, nil
}

// RequestTV requests specific seasons of a TV show
func (c *Client) RequestTV(ctx context.Context, tmdbID int, seasons []int) (*RequestResponse, error) {
	body := TVRequest{
		MediaType: string(MediaTypeTV),
		MediaID:   tmdbID,
		Seasons:   seasons,
	}

	var result RequestResponse
	resp, err := c.client.R().
		SetContext(ctx).
		SetBody(body).
		SetResult(&result).
		Post("/request")

	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	if resp.IsError() {
		return nil, fmt.Errorf("API error: status=%d body=%s", resp.StatusCode(), resp.String())
	}

	logger.Infof("📥 Requested TMDB=%d seasons=%v via Overseerr", tmdbID, seasons)
	return &result, nil
}

// IsSeasonRequested checks if a season is already requested or available
func (c *Client) IsSeasonRequested(details *TVDetails, seasonNum int) bool {
	if details.MediaInfo == nil {
		return false
	}

	// Check if season is in any existing request
	for _, req := range details.MediaInfo.Requests {
		for _, s := range req.Seasons {
			if s.SeasonNumber == seasonNum {
				return true
			}
		}
	}

	// Check season availability status
	for _, s := range details.MediaInfo.Seasons {
		if s.SeasonNumber == seasonNum {
			if s.Status >= MediaStatusPending {
				return true
			}
		}
	}

	return false
}

// GetSeasonStatus returns the current status of a specific season
func (c *Client) GetSeasonStatus(details *TVDetails, seasonNum int) MediaStatus {
	if details.MediaInfo == nil {
		return MediaStatusUnknown
	}

	for _, s := range details.MediaInfo.Seasons {
		if s.SeasonNumber == seasonNum {
			return s.Status
		}
	}

	return MediaStatusUnknown
}
