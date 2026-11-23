package trakt

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/go-resty/resty/v2"
	"go.uber.org/zap"

	httpclient "github.com/fusionn-air/internal/client"
)

const tokenFile = "data/trakt_tokens.json"

var errInvalidGrant = errors.New("refresh token rejected by Trakt")

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

	mu         sync.Mutex
	tokenPath  string
	tokens     *TokenStore
	retryAfter time.Time
	lastError  error
}

// NewAuthManager creates a new auth manager
func NewAuthManager(clientID, clientSecret, baseURL string) *AuthManager {
	return &AuthManager{
		client: httpclient.NewHTTP().
			SetTimeout(30*time.Second).
			SetHeader("Content-Type", "application/json"),
		clientID:     clientID,
		clientSecret: clientSecret,
		baseURL:      baseURL,
		tokenPath:    tokenFile,
	}
}

// Initialize loads the token file and authorizes the client if needed.
func (a *AuthManager) Initialize(ctx context.Context) error {
	a.mu.Lock()
	if err := a.loadTokens(); err != nil && !errors.Is(err, os.ErrNotExist) {
		log.Warn(ctx, "token_load_failed", "Could not load stored Trakt tokens", zap.Error(err))
	}
	a.mu.Unlock()
	_, err := a.accessToken(ctx)
	return err
}

func (a *AuthManager) loadTokens() error {
	data, err := os.ReadFile(a.tokenPath)
	if err != nil {
		return err
	}
	var tokens TokenStore
	if err := json.Unmarshal(data, &tokens); err != nil {
		return err
	}
	if tokens.AccessToken != "" && !tokens.ExpiresAt.IsZero() {
		a.tokens = &tokens
	}
	return nil
}

func (a *AuthManager) storeToken(token TokenResponse) error {
	if token.AccessToken == "" || token.RefreshToken == "" || token.ExpiresIn <= 0 {
		return fmt.Errorf("incomplete OAuth token from Trakt")
	}
	created := time.Now()
	if token.CreatedAt > 0 {
		created = time.Unix(token.CreatedAt, 0)
	}
	tokens := TokenStore{
		AccessToken: token.AccessToken, RefreshToken: token.RefreshToken,
		CreatedAt: created, ExpiresAt: created.Add(time.Duration(token.ExpiresIn) * time.Second),
	}
	a.tokens = &tokens

	dir := filepath.Dir(a.tokenPath)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("creating Trakt token directory: %w", err)
	}
	file, err := os.CreateTemp(dir, ".trakt-tokens-*")
	if err != nil {
		return fmt.Errorf("creating Trakt token file: %w", err)
	}
	defer os.Remove(file.Name())
	writeErr := json.NewEncoder(file).Encode(tokens)
	closeErr := file.Close()
	if err := errors.Join(writeErr, closeErr); err != nil {
		return fmt.Errorf("writing Trakt tokens: %w", err)
	}
	if err := os.Rename(file.Name(), a.tokenPath); err != nil {
		return fmt.Errorf("replacing Trakt token file: %w", err)
	}
	return nil
}

// startDeviceAuth initiates the device authorization flow
func (a *AuthManager) startDeviceAuth(ctx context.Context) error {
	// Get device code
	var deviceCode DeviceCodeResponse
	resp, err := a.client.R().
		SetContext(ctx).
		SetBody(map[string]string{"client_id": a.clientID}).
		SetResult(&deviceCode).
		Post(a.baseURL + "/oauth/device/code")

	if err != nil {
		return fmt.Errorf("getting device code: %w", err)
	}

	if !resp.IsSuccess() {
		return fmt.Errorf("getting device code: HTTP %d", resp.StatusCode())
	}

	log.Warn(ctx, "authorization_required", "Open the Trakt verification URL and enter the user code",
		zap.String("verification_url", deviceCode.VerificationURL), zap.String("user_code", deviceCode.UserCode))

	// Poll for token
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
			return a.storeToken(*token)
		}

		if !pending {
			return fmt.Errorf("authorization denied or expired")
		}
	}

	return fmt.Errorf("device authorization with Trakt timed out")
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
		return nil, true, nil
	default:
		return nil, false, fmt.Errorf("unexpected status: %d", resp.StatusCode())
	}
}

// refreshTokens runs under a.mu. Each refresh token is sent once per attempt.
func (a *AuthManager) refreshTokens(ctx context.Context) error {
	log.Debug(ctx, "token_refresh_started", "Refreshing Trakt access token")
	var token TokenResponse
	var rejection struct {
		Code string `json:"error"`
	}
	resp, err := a.client.R().
		SetContext(ctx).
		SetBody(map[string]string{
			"refresh_token": a.tokens.RefreshToken,
			"client_id":     a.clientID,
			"client_secret": a.clientSecret,
			"grant_type":    "refresh_token",
		}).
		SetResult(&token).
		SetError(&rejection).
		Post(a.baseURL + "/oauth/token")
	if err != nil {
		return fmt.Errorf("refreshing token: %w", err)
	}
	if rejection.Code == "invalid_grant" {
		return errInvalidGrant
	}
	if resp.StatusCode() == http.StatusTooManyRequests {
		if retry := resp.Header().Get("Retry-After"); retry != "" {
			if seconds, err := strconv.Atoi(retry); err == nil && seconds > 0 {
				a.retryAfter = time.Now().Add(time.Duration(seconds) * time.Second)
			} else if until, err := http.ParseTime(retry); err == nil {
				a.retryAfter = until
			}
		}
		return fmt.Errorf("OAuth rate limit exceeded by Trakt")
	}
	if !resp.IsSuccess() {
		return fmt.Errorf("refresh request returned HTTP %d", resp.StatusCode())
	}
	if err := a.storeToken(token); err != nil {
		return err
	}
	log.Debug(ctx, "token_refresh_completed", "Trakt access token refreshed")
	return nil
}

func (a *AuthManager) accessToken(ctx context.Context) (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return "", err
	}

	now := time.Now()
	if a.tokens != nil {
		lifetime := a.tokens.ExpiresAt.Sub(a.tokens.CreatedAt)
		lead := min(5*time.Minute, max(0, lifetime/10))
		if now.Add(lead).Before(a.tokens.ExpiresAt) {
			return a.tokens.AccessToken, nil
		}
	}
	if now.Before(a.retryAfter) {
		if a.tokens != nil && now.Before(a.tokens.ExpiresAt) {
			return a.tokens.AccessToken, nil
		}
		return "", a.lastError
	}

	previous := a.tokens
	var err error
	if a.tokens != nil && a.tokens.RefreshToken != "" {
		err = a.refreshTokens(ctx)
		if errors.Is(err, errInvalidGrant) {
			log.Warn(ctx, "token_refresh_rejected", "Trakt rejected the refresh token. Starting device authorization")
			a.tokens = nil
		}
	}
	if a.tokens == nil || a.tokens.RefreshToken == "" {
		if deviceErr := a.startDeviceAuth(ctx); deviceErr != nil {
			err = fmt.Errorf("authorization with Trakt required: %w", deviceErr)
		} else {
			err = nil
		}
	}
	if err == nil {
		a.retryAfter = time.Time{}
		a.lastError = nil
		return a.tokens.AccessToken, nil
	}
	a.lastError = err
	if !a.retryAfter.After(time.Now()) {
		a.retryAfter = time.Now().Add(time.Minute)
	}
	log.Warn(ctx, "token_refresh_failed", "Could not renew Trakt authorization", zap.Error(err))
	if a.tokens != nil && a.tokens == previous && time.Now().Before(a.tokens.ExpiresAt) && ctx.Err() == nil {
		return a.tokens.AccessToken, nil
	}
	return "", err
}
