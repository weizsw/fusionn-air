package overseerr

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/go-resty/resty/v2"
	"go.uber.org/zap"

	httpclient "github.com/fusionn-air/internal/client"
	"github.com/fusionn-air/internal/config"
	"github.com/fusionn-air/pkg/logger"
)

var log = logger.Component("overseerr")

type Client struct {
	client *resty.Client
	userID int
}

func NewClient(cfg config.OverseerrConfig) *Client {
	client := httpclient.NewHTTP().
		SetBaseURL(strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")+"/api/v1").
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

// GetTVByTMDB gets TV show details by TMDB ID
func (c *Client) GetTVByTMDB(ctx context.Context, tmdbID int) (*TVDetails, error) {
	var details TVDetails
	resp, err := c.client.R().
		SetContext(ctx).
		SetQueryParam("language", "en").
		SetResult(&details).
		Get(fmt.Sprintf("/tv/%d", tmdbID))

	if err != nil {
		return nil, fmt.Errorf("getting TV details: %w", err)
	}

	if !resp.IsSuccess() {
		return nil, fmt.Errorf("getting TV details: HTTP %d", resp.StatusCode())
	}
	if details.ID != tmdbID {
		return nil, fmt.Errorf("getting TV details: unexpected TMDB ID %d", details.ID)
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
		return nil, fmt.Errorf("creating request: HTTP %d", resp.StatusCode())
	}

	fields := []zap.Field{zap.Int("tmdb_id", tmdbID), zap.Ints("seasons", seasons), zap.Int("overseerr_request_id", result.ID)}
	if serverID != nil {
		fields = append(fields, zap.Int("server_id", *serverID))
	}
	log.Info(ctx, "request_submitted", "Season request submitted", fields...)
	return &result, nil
}

type SeasonRequestState int

const (
	SeasonRequestNone SeasonRequestState = iota
	SeasonRequestActive
	SeasonRequestAvailable
	SeasonRequestFailed
	SeasonRequestDeclined
	SeasonRequestBlocklisted
	SeasonRequestDeleted
)

type SeasonRequestInfo struct {
	State       SeasonRequestState
	RequestID   int
	RequestedBy string
}

// GetSeasonRequestInfo describes the existing state for a normal-quality request.
func (c *Client) GetSeasonRequestInfo(details *TVDetails, seasonNum int) SeasonRequestInfo {
	media := details.MediaInfo
	if media == nil {
		return SeasonRequestInfo{}
	}
	if media.Status == MediaStatusBlocklisted {
		return SeasonRequestInfo{State: SeasonRequestBlocklisted}
	}
	seasonStatus := MediaStatusUnknown
	for _, season := range media.Seasons {
		if season.SeasonNumber == seasonNum {
			seasonStatus = season.Status
			break
		}
	}
	switch seasonStatus {
	case MediaStatusPartiallyAvail, MediaStatusAvailable:
		return SeasonRequestInfo{State: SeasonRequestAvailable}
	case MediaStatusBlocklisted:
		return SeasonRequestInfo{State: SeasonRequestBlocklisted}
	case MediaStatusDeleted:
		return SeasonRequestInfo{State: SeasonRequestDeleted}
	}
	if media.Status == MediaStatusDeleted && seasonStatus == MediaStatusUnknown {
		return SeasonRequestInfo{State: SeasonRequestDeleted}
	}

	var active, failed, declined *Request
	for i := range media.Requests {
		request := &media.Requests[i]
		if request.Is4K || !slices.ContainsFunc(request.Seasons, func(season SeasonReq) bool { return season.SeasonNumber == seasonNum }) {
			continue
		}
		switch request.Status {
		case RequestStatusPending, RequestStatusApproved:
			if active == nil || request.ID > active.ID {
				active = request
			}
		case RequestStatusFailed:
			if failed == nil || request.ID > failed.ID {
				failed = request
			}
		case RequestStatusDeclined:
			if declined == nil || request.ID > declined.ID {
				declined = request
			}
		}
	}
	if active != nil {
		info := SeasonRequestInfo{State: SeasonRequestActive, RequestID: active.ID}
		if user := active.RequestedBy; user != nil {
			info.RequestedBy = user.DisplayName
			if info.RequestedBy == "" {
				info.RequestedBy = user.Username
			}
		}
		return info
	}
	if failed != nil {
		return SeasonRequestInfo{State: SeasonRequestFailed, RequestID: failed.ID}
	}
	if declined != nil {
		return SeasonRequestInfo{State: SeasonRequestDeclined, RequestID: declined.ID}
	}
	if seasonStatus == MediaStatusPending || seasonStatus == MediaStatusProcessing {
		return SeasonRequestInfo{State: SeasonRequestActive}
	}
	return SeasonRequestInfo{}
}
