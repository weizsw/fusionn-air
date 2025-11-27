package sonarr

import (
	"context"
	"fmt"
	"time"

	"github.com/fusionn-air/internal/config"
	"github.com/fusionn-air/pkg/logger"
	"github.com/go-resty/resty/v2"
)

type Client struct {
	client *resty.Client
}

func NewClient(cfg config.SonarrConfig) *Client {
	client := resty.New().
		SetBaseURL(cfg.BaseURL+"/api/v3").
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

// GetAllSeries returns all series in Sonarr
func (c *Client) GetAllSeries(ctx context.Context) ([]Series, error) {
	var series []Series
	resp, err := c.client.R().
		SetContext(ctx).
		SetResult(&series).
		Get("/series")

	if err != nil {
		return nil, fmt.Errorf("getting series: %w", err)
	}

	if resp.IsError() {
		return nil, fmt.Errorf("API error: status=%d", resp.StatusCode())
	}

	return series, nil
}

// GetSeries returns a specific series by ID
func (c *Client) GetSeries(ctx context.Context, seriesID int) (*Series, error) {
	var series Series
	resp, err := c.client.R().
		SetContext(ctx).
		SetResult(&series).
		Get(fmt.Sprintf("/series/%d", seriesID))

	if err != nil {
		return nil, fmt.Errorf("getting series: %w", err)
	}

	if resp.IsError() {
		if resp.StatusCode() == 404 {
			return nil, nil // Series not found
		}
		return nil, fmt.Errorf("API error: status=%d", resp.StatusCode())
	}

	return &series, nil
}

// GetSeriesByTvdbID finds a series by its TVDB ID
func (c *Client) GetSeriesByTvdbID(ctx context.Context, tvdbID int) (*Series, error) {
	series, err := c.GetAllSeries(ctx)
	if err != nil {
		return nil, err
	}

	for _, s := range series {
		if s.TvdbID == tvdbID {
			return &s, nil
		}
	}

	return nil, nil // Not found
}

// DeleteSeries removes a series from Sonarr
func (c *Client) DeleteSeries(ctx context.Context, seriesID int, deleteFiles bool) error {
	resp, err := c.client.R().
		SetContext(ctx).
		SetQueryParam("deleteFiles", fmt.Sprintf("%t", deleteFiles)).
		SetQueryParam("addImportListExclusion", "false").
		Delete(fmt.Sprintf("/series/%d", seriesID))

	if err != nil {
		return fmt.Errorf("deleting series: %w", err)
	}

	if resp.IsError() {
		if resp.StatusCode() == 404 {
			return nil // Already deleted
		}
		return fmt.Errorf("API error: status=%d body=%s", resp.StatusCode(), resp.String())
	}

	logger.Infof("🗑️  Deleted series ID=%d from Sonarr (deleteFiles=%t)", seriesID, deleteFiles)
	return nil
}

// GetEpisodes returns all episodes for a series
func (c *Client) GetEpisodes(ctx context.Context, seriesID int) ([]Episode, error) {
	var episodes []Episode
	resp, err := c.client.R().
		SetContext(ctx).
		SetQueryParam("seriesId", fmt.Sprintf("%d", seriesID)).
		SetResult(&episodes).
		Get("/episode")

	if err != nil {
		return nil, fmt.Errorf("getting episodes: %w", err)
	}

	if resp.IsError() {
		return nil, fmt.Errorf("API error: status=%d", resp.StatusCode())
	}

	return episodes, nil
}

// IsSeriesEnded checks if a series has ended (not continuing)
func IsSeriesEnded(series *Series) bool {
	return series.Status == StatusEnded
}

// GetDownloadedEpisodeCount returns the number of downloaded episodes
func GetDownloadedEpisodeCount(series *Series) int {
	return series.Statistics.EpisodeFileCount
}

// GetTotalEpisodeCount returns the total number of episodes (aired)
func GetTotalEpisodeCount(series *Series) int {
	return series.Statistics.EpisodeCount
}

// FormatSize formats bytes to human readable string
func FormatSize(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}
