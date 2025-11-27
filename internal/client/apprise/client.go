package apprise

import (
	"context"
	"fmt"
	"time"

	"github.com/go-resty/resty/v2"

	"github.com/fusionn-air/internal/config"
)

// Response represents an Apprise API response
type Response struct {
	Error string `json:"error,omitempty"`
}

// Client handles Apprise API communication
type Client struct {
	client  *resty.Client
	key     string
	tag     string
	enabled bool
}

// NewClient creates a new Apprise client
func NewClient(cfg config.AppriseConfig) *Client {
	key := cfg.Key
	if key == "" {
		key = "apprise"
	}
	tag := cfg.Tag
	if tag == "" {
		tag = "all"
	}

	client := resty.New().
		SetBaseURL(cfg.BaseURL).
		SetTimeout(30 * time.Second).
		SetRetryCount(2).
		SetRetryWaitTime(1 * time.Second)

	return &Client{
		client:  client,
		key:     key,
		tag:     tag,
		enabled: cfg.Enabled,
	}
}

// Notify sends a notification via Apprise
func (c *Client) Notify(ctx context.Context, title, body, notifyType string) error {
	if !c.enabled {
		return nil
	}

	// Build form data
	formData := map[string]string{
		"body": body,
		"tags": c.tag,
	}
	if title != "" {
		formData["title"] = title
	}
	if notifyType != "" {
		formData["type"] = notifyType
	}

	var apiResp Response
	resp, err := c.client.R().
		SetContext(ctx).
		SetFormData(formData).
		SetResult(&apiResp).
		Post(fmt.Sprintf("/notify/%s", c.key))

	if err != nil {
		return fmt.Errorf("sending request: %w", err)
	}

	if resp.IsError() {
		return fmt.Errorf("apprise returned status %d: %s", resp.StatusCode(), resp.String())
	}

	// Check for error in response body (Apprise returns 200 with error in JSON)
	if apiResp.Error != "" {
		return fmt.Errorf("apprise error: %s", apiResp.Error)
	}

	return nil
}

// NotifySuccess sends a success notification
func (c *Client) NotifySuccess(ctx context.Context, title, body string) error {
	return c.Notify(ctx, title, body, "success")
}

// NotifyWarning sends a warning notification
func (c *Client) NotifyWarning(ctx context.Context, title, body string) error {
	return c.Notify(ctx, title, body, "warning")
}

// NotifyFailure sends a failure notification
func (c *Client) NotifyFailure(ctx context.Context, title, body string) error {
	return c.Notify(ctx, title, body, "failure")
}

// NotifyInfo sends an info notification
func (c *Client) NotifyInfo(ctx context.Context, title, body string) error {
	return c.Notify(ctx, title, body, "info")
}

// IsEnabled returns whether notifications are enabled
func (c *Client) IsEnabled() bool {
	return c.enabled
}
