package radarr

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

func NewClient(cfg config.RadarrConfig) *Client {
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

// GetAllMovies returns all movies in Radarr
func (c *Client) GetAllMovies(ctx context.Context) ([]Movie, error) {
	var movies []Movie
	resp, err := c.client.R().
		SetContext(ctx).
		SetResult(&movies).
		Get("/movie")

	if err != nil {
		return nil, fmt.Errorf("getting movies: %w", err)
	}

	if resp.IsError() {
		return nil, fmt.Errorf("API error: status=%d", resp.StatusCode())
	}

	return movies, nil
}

// GetMovie returns a specific movie by ID
func (c *Client) GetMovie(ctx context.Context, movieID int) (*Movie, error) {
	var movie Movie
	resp, err := c.client.R().
		SetContext(ctx).
		SetResult(&movie).
		Get(fmt.Sprintf("/movie/%d", movieID))

	if err != nil {
		return nil, fmt.Errorf("getting movie: %w", err)
	}

	if resp.IsError() {
		if resp.StatusCode() == 404 {
			return nil, nil // Movie not found
		}
		return nil, fmt.Errorf("API error: status=%d", resp.StatusCode())
	}

	return &movie, nil
}

// GetMovieByTmdbID finds a movie by its TMDB ID
func (c *Client) GetMovieByTmdbID(ctx context.Context, tmdbID int) (*Movie, error) {
	movies, err := c.GetAllMovies(ctx)
	if err != nil {
		return nil, err
	}

	for _, m := range movies {
		if m.TmdbID == tmdbID {
			return &m, nil
		}
	}

	return nil, nil // Not found
}

// DeleteMovie removes a movie from Radarr
func (c *Client) DeleteMovie(ctx context.Context, movieID int, deleteFiles bool) error {
	resp, err := c.client.R().
		SetContext(ctx).
		SetQueryParam("deleteFiles", fmt.Sprintf("%t", deleteFiles)).
		SetQueryParam("addImportExclusion", "false").
		Delete(fmt.Sprintf("/movie/%d", movieID))

	if err != nil {
		return fmt.Errorf("deleting movie: %w", err)
	}

	if resp.IsError() {
		if resp.StatusCode() == 404 {
			return nil // Already deleted
		}
		return fmt.Errorf("API error: status=%d body=%s", resp.StatusCode(), resp.String())
	}

	logger.Infof("🗑️  Deleted movie ID=%d from Radarr (deleteFiles=%t)", movieID, deleteFiles)
	return nil
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
