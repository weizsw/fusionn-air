package trakt

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/fusionn-air/pkg/logger"
	"github.com/go-resty/resty/v2"
)

const (
	tokenFile       = "data/trakt_tokens.json"
	tokenExpirySafe = 24 * time.Hour // Refresh 1 day before expiry
)

// TokenStore holds OAuth tokens
type TokenStore struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	ExpiresAt    time.Time `json:"expires_at"`
	CreatedAt    time.Time `json:"created_at"`
}

// DeviceCodeResponse from Trakt device auth
type DeviceCodeResponse struct {
	DeviceCode      string `json:"device_code"`
	UserCode        string `json:"user_code"`
	VerificationURL string `json:"verification_url"`
	ExpiresIn       int    `json:"expires_in"`
	Interval        int    `json:"interval"`
}

// TokenResponse from Trakt OAuth
type TokenResponse struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	RefreshToken string `json:"refresh_token"`
	Scope        string `json:"scope"`
	CreatedAt    int64  `json:"created_at"`
}

// AuthManager handles Trakt OAuth
type AuthManager struct {
	client       *resty.Client
	clientID     string
	clientSecret string
	baseURL      string

	mu     sync.RWMutex
	tokens *TokenStore
}

// NewAuthManager creates a new auth manager
func NewAuthManager(clientID, clientSecret, baseURL string) *AuthManager {
	return &AuthManager{
		client: resty.New().
			SetTimeout(30 * time.Second).
			SetHeader("Content-Type", "application/json"),
		clientID:     clientID,
		clientSecret: clientSecret,
		baseURL:      baseURL,
	}
}

// Initialize loads tokens or starts device auth flow
func (a *AuthManager) Initialize(ctx context.Context) error {
	// Try to load existing tokens
	if err := a.loadTokens(); err == nil {
		logger.Info("[trakt-auth] loaded existing tokens")

		// Check if refresh needed
		if a.needsRefresh() {
			logger.Info("[trakt-auth] tokens need refresh...")
			if err := a.refreshTokens(ctx); err != nil {
				logger.Warnf("[trakt-auth] refresh failed, will need re-auth: %v", err)
				return a.startDeviceAuth(ctx)
			}
		}

		return nil
	}

	// No tokens - start device auth
	return a.startDeviceAuth(ctx)
}

// GetAccessToken returns the current access token
func (a *AuthManager) GetAccessToken() string {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if a.tokens == nil {
		return ""
	}
	return a.tokens.AccessToken
}

// IsAuthenticated returns true if we have valid tokens
func (a *AuthManager) IsAuthenticated() bool {
	a.mu.RLock()
	defer a.mu.RUnlock()

	return a.tokens != nil && a.tokens.AccessToken != ""
}

// needsRefresh checks if token refresh is needed
func (a *AuthManager) needsRefresh() bool {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if a.tokens == nil {
		return true
	}

	// Refresh if within 7 days of expiry
	return time.Now().Add(tokenExpirySafe).After(a.tokens.ExpiresAt)
}

// loadTokens loads tokens from file
func (a *AuthManager) loadTokens() error {
	data, err := os.ReadFile(tokenFile)
	if err != nil {
		return err
	}

	var tokens TokenStore
	if err := json.Unmarshal(data, &tokens); err != nil {
		return err
	}

	a.mu.Lock()
	a.tokens = &tokens
	a.mu.Unlock()

	return nil
}

// saveTokens saves tokens to file
func (a *AuthManager) saveTokens() error {
	a.mu.RLock()
	tokens := a.tokens
	a.mu.RUnlock()

	if tokens == nil {
		return fmt.Errorf("no tokens to save")
	}

	// Ensure directory exists
	dir := filepath.Dir(tokenFile)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("creating data dir: %w", err)
	}

	data, err := json.MarshalIndent(tokens, "", "  ")
	if err != nil {
		return err
	}

	if err := os.WriteFile(tokenFile, data, 0600); err != nil {
		return err
	}

	logger.Debug("[trakt-auth] tokens saved to file")
	return nil
}

// startDeviceAuth initiates the device authorization flow
func (a *AuthManager) startDeviceAuth(ctx context.Context) error {
	logger.Info("[trakt-auth] starting device authorization flow...")

	// Step 1: Get device code
	var deviceCode DeviceCodeResponse
	resp, err := a.client.R().
		SetContext(ctx).
		SetBody(map[string]string{"client_id": a.clientID}).
		SetResult(&deviceCode).
		Post(a.baseURL + "/oauth/device/code")

	if err != nil {
		return fmt.Errorf("getting device code: %w", err)
	}

	if resp.IsError() {
		return fmt.Errorf("device code error: %s", resp.String())
	}

	// Step 2: Show user instructions
	logger.Info("[trakt-auth] ╔════════════════════════════════════════════════════════════╗")
	logger.Info("[trakt-auth] ║              TRAKT AUTHORIZATION REQUIRED                  ║")
	logger.Info("[trakt-auth] ╠════════════════════════════════════════════════════════════╣")
	logger.Infof("[trakt-auth] ║  1. Go to: %-47s ║", deviceCode.VerificationURL)
	logger.Infof("[trakt-auth] ║  2. Enter code: %-42s ║", deviceCode.UserCode)
	logger.Info("[trakt-auth] ║  3. Click 'Authorize' on the Trakt website                 ║")
	logger.Info("[trakt-auth] ╚════════════════════════════════════════════════════════════╝")
	logger.Info("[trakt-auth] Waiting for authorization...")

	// Step 3: Poll for token
	interval := time.Duration(deviceCode.Interval) * time.Second
	if interval < time.Second {
		interval = 5 * time.Second
	}
	expires := time.Now().Add(time.Duration(deviceCode.ExpiresIn) * time.Second)

	for time.Now().Before(expires) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(interval):
		}

		token, pending, err := a.pollToken(ctx, deviceCode.DeviceCode)
		if err != nil {
			return err
		}

		if token != nil {
			// Success!
			a.mu.Lock()
			a.tokens = &TokenStore{
				AccessToken:  token.AccessToken,
				RefreshToken: token.RefreshToken,
				ExpiresAt:    time.Unix(token.CreatedAt, 0).Add(time.Duration(token.ExpiresIn) * time.Second),
				CreatedAt:    time.Unix(token.CreatedAt, 0),
			}
			a.mu.Unlock()

			if err := a.saveTokens(); err != nil {
				logger.Warnf("[trakt-auth] failed to save tokens: %v", err)
			}

			logger.Info("[trakt-auth] ✓ Authorization successful!")
			logger.Infof("[trakt-auth] Token expires: %s", a.tokens.ExpiresAt.Format(time.RFC3339))
			return nil
		}

		if !pending {
			return fmt.Errorf("authorization denied or expired")
		}
	}

	return fmt.Errorf("authorization timed out - please restart and try again")
}

// pollToken checks if user has authorized
func (a *AuthManager) pollToken(ctx context.Context, deviceCode string) (*TokenResponse, bool, error) {
	var token TokenResponse
	resp, err := a.client.R().
		SetContext(ctx).
		SetBody(map[string]string{
			"code":          deviceCode,
			"client_id":     a.clientID,
			"client_secret": a.clientSecret,
		}).
		SetResult(&token).
		Post(a.baseURL + "/oauth/device/token")

	if err != nil {
		return nil, false, fmt.Errorf("polling token: %w", err)
	}

	switch resp.StatusCode() {
	case 200:
		return &token, false, nil
	case 400:
		// Still pending
		return nil, true, nil
	case 404:
		return nil, false, fmt.Errorf("invalid device code")
	case 409:
		return nil, false, fmt.Errorf("code already used")
	case 410:
		return nil, false, fmt.Errorf("code expired")
	case 418:
		return nil, false, fmt.Errorf("user denied authorization")
	case 429:
		// Rate limited
		return nil, true, nil
	default:
		return nil, false, fmt.Errorf("unexpected status: %d", resp.StatusCode())
	}
}

// refreshTokens refreshes the access token
func (a *AuthManager) refreshTokens(ctx context.Context) error {
	a.mu.RLock()
	refreshToken := a.tokens.RefreshToken
	a.mu.RUnlock()

	var token TokenResponse
	resp, err := a.client.R().
		SetContext(ctx).
		SetBody(map[string]string{
			"refresh_token": refreshToken,
			"client_id":     a.clientID,
			"client_secret": a.clientSecret,
			"grant_type":    "refresh_token",
		}).
		SetResult(&token).
		Post(a.baseURL + "/oauth/token")

	if err != nil {
		return fmt.Errorf("refreshing token: %w", err)
	}

	if resp.IsError() {
		return fmt.Errorf("refresh error: %s", resp.String())
	}

	a.mu.Lock()
	a.tokens = &TokenStore{
		AccessToken:  token.AccessToken,
		RefreshToken: token.RefreshToken,
		ExpiresAt:    time.Unix(token.CreatedAt, 0).Add(time.Duration(token.ExpiresIn) * time.Second),
		CreatedAt:    time.Unix(token.CreatedAt, 0),
	}
	a.mu.Unlock()

	if err := a.saveTokens(); err != nil {
		logger.Warnf("[trakt-auth] failed to save refreshed tokens: %v", err)
	}

	logger.Infof("[trakt-auth] ✓ Token refreshed, expires: %s", a.tokens.ExpiresAt.Format(time.RFC3339))
	return nil
}

// EnsureValidToken checks and refreshes token if needed
func (a *AuthManager) EnsureValidToken(ctx context.Context) error {
	if a.needsRefresh() {
		return a.refreshTokens(ctx)
	}
	return nil
}

