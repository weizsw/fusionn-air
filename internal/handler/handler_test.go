package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/fusionn-air/internal/client/scrob"
	"github.com/fusionn-air/internal/config"
	"github.com/fusionn-air/internal/scheduler"
	"github.com/fusionn-air/internal/service/watcher"
)

func TestManualRunRequiresKeyAndSurvivesRequestCancellation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	started := make(chan struct{})
	unblock := make(chan struct{})
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		select {
		case <-unblock:
			w.Header().Set("Content-Type", "application/json")
			if _, err := w.Write([]byte(`{"calendar":{"today":"2026-01-10","entries":[]}}`)); err != nil {
				t.Error(err)
			}
		case <-r.Context().Done():
		case <-time.After(5 * time.Second):
			t.Error("watcher request stalled")
		}
	}))
	t.Cleanup(backend.Close)
	cfgPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(cfgPath, []byte("scheduler:\n  dry_run: true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.NewManager(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cfg.Stop)
	source, err := scrob.NewClient(config.ScrobConfig{BaseURL: backend.URL, APIKey: "fixture"})
	if err != nil {
		t.Fatal(err)
	}
	svc := watcher.NewService(watcher.NewScrobSource(source), nil, nil, cfg)
	sched := scheduler.New(svc)
	if err := sched.Start("@daily"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(sched.Stop)
	router := gin.New()
	New(svc, sched, "air-fixture").RegisterRoutes(router)

	post := func(ctx context.Context, path, key string, want int) {
		t.Helper()
		body := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, path, nil).WithContext(ctx)
		request.Header.Set("X-API-Key", key)
		router.ServeHTTP(body, request)
		if body.Code != want {
			t.Fatalf("POST %s = %d, want %d. %s", path, body.Code, want, body.Body.String())
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	post(ctx, "/api/v1/watcher/run", "", http.StatusUnauthorized)
	post(ctx, "/api/v1/process", "incorrect", http.StatusUnauthorized)
	post(ctx, "/api/v1/watcher/run", "air-fixture", http.StatusAccepted)
	cancel()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("watcher did not start")
	}
	post(context.Background(), "/api/v1/process", "air-fixture", http.StatusConflict)
	close(unblock)

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/watcher/stats", nil))
		var body struct{ Stats watcher.Stats }
		if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		if !body.Stats.Running && !body.Stats.LastRun.IsZero() {
			if body.Stats.LastError != "" {
				t.Fatalf("accepted run failed: %s", body.Stats.LastError)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("watcher did not finish")
}
