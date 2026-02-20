package emby

import (
	"context"
	"fmt"
	"time"

	"github.com/go-resty/resty/v2"

	"github.com/fusionn-air/internal/config"
	"github.com/fusionn-air/pkg/logger"
)

type Client struct {
	client *resty.Client
}

func NewClient(cfg config.EmbyConfig) *Client {
	client := resty.New().
		SetBaseURL(cfg.BaseURL+"/emby").
		SetTimeout(30*time.Second).
		SetHeader("Content-Type", "application/json").
		SetQueryParam("api_key", cfg.APIKey).
		SetRetryCount(3).
		SetRetryWaitTime(1 * time.Second).
		SetRetryMaxWaitTime(5 * time.Second).
		AddRetryCondition(func(r *resty.Response, err error) bool {
			return err != nil || r.StatusCode() >= 500
		})

	return &Client{client: client}
}

func (c *Client) GetLibraries(ctx context.Context) ([]VirtualFolder, error) {
	var folders []VirtualFolder
	r, err := c.client.R().
		SetContext(ctx).
		SetResult(&folders).
		Get("/Library/VirtualFolders")

	if err != nil {
		return nil, fmt.Errorf("getting libraries: %w", err)
	}

	if r.IsError() {
		return nil, fmt.Errorf("API error: status=%d", r.StatusCode())
	}

	return folders, nil
}

func (c *Client) GetSeries(ctx context.Context, parentID string) ([]Item, error) {
	var resp ItemsResponse
	req := c.client.R().
		SetContext(ctx).
		SetResult(&resp).
		SetQueryParam("IncludeItemTypes", "Series").
		SetQueryParam("Recursive", "true").
		SetQueryParam("Fields", "ProviderIds,Path,ParentId")

	if parentID != "" {
		req.SetQueryParam("ParentId", parentID)
	}

	r, err := req.Get("/Items")

	if err != nil {
		return nil, fmt.Errorf("getting series: %w", err)
	}

	if r.IsError() {
		return nil, fmt.Errorf("API error: status=%d", r.StatusCode())
	}

	return resp.Items, nil
}

func (c *Client) GetAllSeries(ctx context.Context) ([]Item, error) {
	return c.GetSeries(ctx, "")
}

func (c *Client) GetMovies(ctx context.Context, parentID string) ([]Item, error) {
	var resp ItemsResponse
	req := c.client.R().
		SetContext(ctx).
		SetResult(&resp).
		SetQueryParam("IncludeItemTypes", "Movie").
		SetQueryParam("Recursive", "true").
		SetQueryParam("Fields", "ProviderIds,Path,ParentId")

	if parentID != "" {
		req.SetQueryParam("ParentId", parentID)
	}

	r, err := req.Get("/Items")

	if err != nil {
		return nil, fmt.Errorf("getting movies: %w", err)
	}

	if r.IsError() {
		return nil, fmt.Errorf("API error: status=%d", r.StatusCode())
	}

	return resp.Items, nil
}

func (c *Client) GetAllMovies(ctx context.Context) ([]Item, error) {
	return c.GetMovies(ctx, "")
}

func (c *Client) GetSeasons(ctx context.Context, seriesID string) ([]Item, error) {
	var resp ItemsResponse
	r, err := c.client.R().
		SetContext(ctx).
		SetResult(&resp).
		Get(fmt.Sprintf("/Shows/%s/Seasons", seriesID))

	if err != nil {
		return nil, fmt.Errorf("getting seasons: %w", err)
	}

	if r.IsError() {
		return nil, fmt.Errorf("API error: status=%d", r.StatusCode())
	}

	return resp.Items, nil
}

func (c *Client) GetEpisodes(ctx context.Context, seriesID, seasonID string) ([]Item, error) {
	var resp ItemsResponse
	r, err := c.client.R().
		SetContext(ctx).
		SetResult(&resp).
		SetQueryParam("SeasonId", seasonID).
		SetQueryParam("Fields", "LocationType").
		Get(fmt.Sprintf("/Shows/%s/Episodes", seriesID))

	if err != nil {
		return nil, fmt.Errorf("getting episodes: %w", err)
	}

	if r.IsError() {
		return nil, fmt.Errorf("API error: status=%d", r.StatusCode())
	}

	return resp.Items, nil
}

func (c *Client) DeleteItem(ctx context.Context, itemID string) error {
	r, err := c.client.R().
		SetContext(ctx).
		Delete(fmt.Sprintf("/Items/%s", itemID))

	if err != nil {
		return fmt.Errorf("deleting item: %w", err)
	}

	if r.IsError() {
		if r.StatusCode() == 404 {
			return nil
		}
		return fmt.Errorf("API error: status=%d body=%s", r.StatusCode(), r.String())
	}

	logger.Infof("🗑️  Deleted item ID=%s from Emby", itemID)
	return nil
}
