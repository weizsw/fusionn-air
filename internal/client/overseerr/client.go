package overseerr

import (
	"context"
	"fmt"
	"net/url"
	"time"

	"github.com/go-resty/resty/v2"

	"github.com/fusionn-air/internal/config"
	"github.com/fusionn-air/pkg/logger"
)

type Client struct {
	client *resty.Client
	userID int
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

	return &Client{
		client: client,
		userID: cfg.UserID,
	}
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

// RequestTV requests specific seasons of a TV show.
// serverID is optional — when non-nil, it targets a specific Overseerr backend server.
func (c *Client) RequestTV(ctx context.Context, tmdbID int, seasons []int, serverID *int) (*RequestResponse, error) {
	body := TVRequest{
		MediaType: string(MediaTypeTV),
		MediaID:   tmdbID,
		Seasons:   seasons,
		UserID:    c.userID,
		ServerID:  serverID,
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

	if serverID != nil {
		logger.Infof("📥 Requested TMDB=%d seasons=%v via Overseerr (serverId=%d)", tmdbID, seasons, *serverID)
	} else {
		logger.Infof("📥 Requested TMDB=%d seasons=%v via Overseerr", tmdbID, seasons)
	}
	return &result, nil
}

// SeasonRequestInfo contains details about a season's request status
type SeasonRequestInfo struct {
	Requested   bool
	Status      MediaStatus
	RequestedBy string // Username of who requested it (empty if not requested or available)
}

// GetSeasonRequestInfo returns detailed info about a season's request status
func (c *Client) GetSeasonRequestInfo(details *TVDetails, seasonNum int) SeasonRequestInfo {
	info := SeasonRequestInfo{}

	if details.MediaInfo == nil {
		return info
	}

	// Check if season is in any existing request
	for _, req := range details.MediaInfo.Requests {
		for _, s := range req.Seasons {
			if s.SeasonNumber == seasonNum {
				info.Requested = true
				if req.RequestedBy != nil {
					if req.RequestedBy.DisplayName != "" {
						info.RequestedBy = req.RequestedBy.DisplayName
					} else {
						info.RequestedBy = req.RequestedBy.Username
					}
				}
				return info
			}
		}
	}

	// Check season availability status
	for _, s := range details.MediaInfo.Seasons {
		if s.SeasonNumber == seasonNum {
			info.Status = s.Status
			if s.Status >= MediaStatusPending {
				info.Requested = true
			}
		}
	}

	return info
}

// IsSeasonRequested checks if a season is already requested or available
func (c *Client) IsSeasonRequested(details *TVDetails, seasonNum int) bool {
	return c.GetSeasonRequestInfo(details, seasonNum).Requested
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
