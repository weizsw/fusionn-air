package config

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/fusionn-air/pkg/logger"
)

func writeConfig(t *testing.T, delay int) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	data := []byte("cleanup:\n  enabled: true\n  delay_days: " + strconv.Itoa(delay) + "\n  exclusions: [Keep]\n")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestManagerGetReturnsIsolatedSnapshot(t *testing.T) {
	logger.Init(true)
	manager, err := NewManager(writeConfig(t, 3))
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Stop()

	snapshot := manager.Get()
	snapshot.Cleanup.DelayDays = 99
	snapshot.Cleanup.Exclusions[0] = "Changed"
	fresh := manager.Get()
	if fresh.Cleanup.DelayDays != 3 || fresh.Cleanup.Exclusions[0] != "Keep" {
		t.Fatalf("Get() leaked mutable state: %#v", fresh.Cleanup)
	}
}

func TestReloadKeepsRestartOnlySettings(t *testing.T) {
	logger.Init(true)
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("cleanup:\n  enabled: true\n  delay_days: 3\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	manager, err := NewManager(path)
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Stop()
	if err := os.WriteFile(path, []byte("cleanup:\n  enabled: false\n  delay_days: 9\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := manager.reload(); err != nil {
		t.Fatal(err)
	}
	got := manager.Get().Cleanup
	if !got.Enabled || got.DelayDays != 9 {
		t.Fatalf("reloaded cleanup = %#v, want enabled preserved and delay 9", got)
	}
}

func TestInvalidReloadPreservesPreviousSnapshot(t *testing.T) {
	logger.Init(true)
	path := writeConfig(t, 3)
	manager, err := NewManager(path)
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Stop()
	if err := os.WriteFile(path, []byte("cleanup: ["), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := manager.reload(); err == nil {
		t.Fatal("reload() accepted invalid YAML")
	}
	if got := manager.Get().Cleanup.DelayDays; got != 3 {
		t.Fatalf("invalid reload changed delay to %d, want 3", got)
	}
}

func TestManagersDoNotShareViperState(t *testing.T) {
	logger.Init(true)
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

	first.reload()
	if got := first.Get().Cleanup.DelayDays; got != 3 {
		t.Fatalf("first manager reloaded delay %d from second manager, want 3", got)
	}
}
