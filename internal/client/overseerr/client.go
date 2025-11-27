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
		}).
		OnBeforeRequest(func(c *resty.Client, r *resty.Request) error {
			logger.Debugf("[overseerr] --> %s %s", r.Method, r.URL)
			if r.Body != nil {
				logger.Debugf("[overseerr] --> body: %+v", r.Body)
			}
			return nil
		}).
		OnAfterResponse(func(c *resty.Client, r *resty.Response) error {
			logger.Debugf("[overseerr] <-- %s %s [%d] %v",
				r.Request.Method, r.Request.URL, r.StatusCode(), r.Time())
			if r.IsError() {
				logger.Warnf("[overseerr] error response: %s", r.String())
			}
			return nil
		}).
		OnError(func(r *resty.Request, err error) {
			logger.Errorf("[overseerr] request failed: %s %s error=%v", r.Method, r.URL, err)
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
		return nil, fmt.Errorf("API error: status=%d body=%s", resp.StatusCode(), resp.String())
	}

	logger.Debugf("[overseerr] search '%s' returned %d results", query, result.TotalResults)
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
		return nil, fmt.Errorf("API error: status=%d body=%s", resp.StatusCode(), resp.String())
	}

	logger.Debugf("[overseerr] TV TMDB=%d name=%s seasons=%d", tmdbID, details.Name, details.NumberOfSeasons)
	return &details, nil
}

// RequestTV requests specific seasons of a TV show
func (c *Client) RequestTV(ctx context.Context, tmdbID int, seasons []int) (*RequestResponse, error) {
	body := TVRequest{
		MediaType: string(MediaTypeTV),
		MediaID:   tmdbID,
		Seasons:   seasons,
	}

	logger.Infof("[overseerr] requesting TMDB=%d seasons=%v", tmdbID, seasons)

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

	logger.Infof("[overseerr] created request ID=%d for TMDB=%d seasons=%v", result.ID, tmdbID, seasons)
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
				logger.Debugf("[overseerr] season %d already in request ID=%d", seasonNum, req.ID)
				return true
			}
		}
	}

	// Check season availability status
	for _, s := range details.MediaInfo.Seasons {
		if s.SeasonNumber == seasonNum {
			if s.Status >= MediaStatusPending {
				logger.Debugf("[overseerr] season %d status=%d (pending or better)", seasonNum, s.Status)
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
