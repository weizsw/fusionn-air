package trakt

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fusionn-air/internal/config"
)

func TestClientUsesShortLivedTokensAndReauthorizesOnInvalidGrant(t *testing.T) {
	var refreshCalls atomic.Int32
	var deviceCalls atomic.Int32
	var calendarCalls atomic.Int32
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		var body string
		switch r.URL.Path {
		case "/oauth/token":
			refreshCalls.Add(1)
			w.WriteHeader(http.StatusBadRequest)
			body = `{"error":"invalid_grant"}`
		case "/oauth/device/code":
			deviceCalls.Add(1)
			body = `{"device_code":"sample","expires_in":10,"interval":1}`
		case "/oauth/device/token":
			body = `{"access_token":"renewed","refresh_token":"rotated","expires_in":86400}`
		default:
			want := "Bearer initial"
			if calendarCalls.Add(1) > 1 {
				want = "Bearer renewed"
			}
			if r.Header.Get("Authorization") != want {
				t.Errorf("calendar authorization did not match %s", want)
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			body = `[]`
		}
		if _, err := w.Write([]byte(body)); err != nil {
			t.Error(err)
		}
	}))
	defer api.Close()

	client := NewClient(config.TraktConfig{BaseURL: api.URL})
	client.auth.tokenPath = filepath.Join(t.TempDir(), "tokens.json")
	now := time.Now()
	stored, err := json.Marshal(TokenStore{AccessToken: "initial", RefreshToken: "one-use", CreatedAt: now, ExpiresAt: now.Add(24 * time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(client.auth.tokenPath, stored, 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := client.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := client.GetMyShowsCalendar(ctx, 7); err != nil || refreshCalls.Load() != 0 {
		t.Fatalf("24-hour token refreshed prematurely: calls=%d, err=%v", refreshCalls.Load(), err)
	}

	client.auth.mu.Lock()
	client.auth.tokens.ExpiresAt = time.Now().Add(-time.Second)
	client.auth.mu.Unlock()
	for range 2 {
		if _, err := client.GetMyShowsCalendar(ctx, 7); err != nil {
			t.Fatal(err)
		}
	}
	if refreshCalls.Load() != 1 || deviceCalls.Load() != 1 {
		t.Fatalf("refresh calls=%d, device calls=%d, want one each", refreshCalls.Load(), deviceCalls.Load())
	}
	data, err := os.ReadFile(client.auth.tokenPath)
	if err != nil {
		t.Fatal(err)
	}
	var tokens TokenStore
	if err := json.Unmarshal(data, &tokens); err != nil || tokens.RefreshToken != "rotated" {
		t.Fatalf("renewed token file is invalid: %v", err)
	}
}
