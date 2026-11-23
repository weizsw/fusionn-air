package config

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func writeConfig(t *testing.T, days int) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	data := []byte("watcher:\n  source: scrob\n  calendar_days: " + strconv.Itoa(days) + "\n  excluded_genres: [animation]\n")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestManagerGetReturnsIsolatedSnapshot(t *testing.T) {
	manager, err := NewManager(writeConfig(t, 3))
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Stop()

	snapshot := manager.Get()
	snapshot.Watcher.CalendarDays = 99
	snapshot.Watcher.ExcludedGenres[0] = "Changed"
	fresh := manager.Get()
	if fresh.Watcher.CalendarDays != 3 || fresh.Watcher.ExcludedGenres[0] != "animation" {
		t.Fatalf("Get() leaked mutable state: %#v", fresh.Watcher)
	}
}

func TestReloadKeepsRestartOnlySettings(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("overseerr:\n  base_url: http://localhost:5055/\n  api_key: fixture\nwatcher:\n  enabled: true\n  source: scrob\n  calendar_days: 3\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	manager, err := NewManager(path)
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Stop()
	if err := os.WriteFile(path, []byte("watcher:\n  enabled: false\n  source: trakt\n  calendar_days: 9\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := manager.reload(); err != nil {
		t.Fatal(err)
	}
	got := manager.Get().Watcher
	if !got.Enabled || got.Source != "scrob" || got.CalendarDays != 9 {
		t.Fatalf("reloaded watcher = %#v, want enabled and source preserved with calendar days 9", got)
	}
}

func TestInvalidReloadPreservesPreviousSnapshot(t *testing.T) {
	path := writeConfig(t, 3)
	manager, err := NewManager(path)
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Stop()
	if err := os.WriteFile(path, []byte("watcher: ["), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := manager.reload(); err == nil {
		t.Fatal("reload() accepted invalid YAML")
	}
	if got := manager.Get().Watcher.CalendarDays; got != 3 {
		t.Fatalf("invalid reload changed calendar days to %d, want 3", got)
	}
}

func TestManagersDoNotShareViperState(t *testing.T) {
	first, err := NewManager(writeConfig(t, 3))
	if err != nil {
		t.Fatal(err)
	}
	defer first.Stop()
	second, err := NewManager(writeConfig(t, 8))
	if err != nil {
		t.Fatal(err)
	}
	defer second.Stop()

	if err := first.reload(); err != nil {
		t.Fatal(err)
	}
	if got := first.Get().Watcher.CalendarDays; got != 3 {
		t.Fatalf("first manager reloaded calendar days %d from second manager, want 3", got)
	}
}

func TestManagerRequiresExplicitAlternateServerAndKeepsPreviousConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	const enabled = `overseerr:
  base_url: http://localhost:5055/
  api_key: fixture
watcher:
  enabled: true
  routing:
    alternate_genres: [anime]
`
	write := func(content string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write(enabled + "    alternate_server_id: 0\n")
	manager, err := NewManager(path)
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Stop()
	if cfg := manager.Get(); cfg.Server.Port != 8080 || cfg.Watcher.Routing.AlternateServerID != 0 {
		t.Fatalf("port or alternate server did not use configured/default values: %+v %+v", cfg.Server, cfg.Watcher.Routing)
	}
	write(enabled)
	if err := manager.reload(); err == nil || !strings.Contains(err.Error(), "alternate_server_id") {
		t.Fatalf("reload error = %v", err)
	}
	if manager.Get().Watcher.Routing.AlternateServerID != 0 {
		t.Fatal("invalid reload changed routing")
	}
}
