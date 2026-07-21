package cleanup

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestQueueLifecycleAndReload(t *testing.T) {
	path := filepath.Join(t.TempDir(), "queue.json")
	q, err := NewQueueWithFile(path, true)
	if err != nil {
		t.Fatal(err)
	}

	now := time.Date(2026, 7, 21, 12, 0, 0, 0, time.UTC)
	q.now = func() time.Time { return now }
	if created, err := q.Add(&QueueItem{ID: 7, Title: "Example"}); err != nil || !created {
		t.Fatalf("Add() = created %v, err %v", created, err)
	}
	if _, state, _ := q.status(7, 1); state != candidatePendingUnmonitor {
		t.Fatalf("state before unmonitor = %v, want pending", state)
	}

	now = now.Add(2 * time.Hour)
	if err := q.MarkUnmonitored(7); err != nil {
		t.Fatal(err)
	}
	now = now.Add(23 * time.Hour)
	if _, state, days := q.status(7, 1); state != candidateWaiting || days != 1 {
		t.Fatalf("state after 23h = %v, days %d; want waiting, 1", state, days)
	}
	now = now.Add(time.Hour)
	if _, state, _ := q.status(7, 1); state != candidateReady {
		t.Fatalf("state after 24h = %v, want ready", state)
	}

	reloaded, err := NewQueueWithFile(path, true)
	if err != nil {
		t.Fatal(err)
	}
	if item := reloaded.Get(7); item == nil || item.UnmonitoredAt == nil {
		t.Fatalf("reloaded item = %#v, want persisted unmonitor time", item)
	}
}

func TestQueueSnapshotsDoNotEscapeState(t *testing.T) {
	q, err := NewQueueWithFile(filepath.Join(t.TempDir(), "queue.json"), false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := q.Add(&QueueItem{ID: 1, Title: "Original"}); err != nil {
		t.Fatal(err)
	}

	q.Get(1).Title = "Changed"
	q.GetAll()[0].Title = "Also changed"
	if got := q.Get(1).Title; got != "Original" {
		t.Fatalf("stored title = %q, want Original", got)
	}
}

func TestQueueRejectsCorruptExistingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "queue.json")
	if err := os.WriteFile(path, []byte("not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewQueueWithFile(path, false); err == nil {
		t.Fatal("NewQueueWithFile() accepted corrupt state")
	}
}

func TestQueueDoesNotMutateWhenPersistenceFails(t *testing.T) {
	q, err := NewQueueWithFile(filepath.Join(t.TempDir(), "queue.json"), false)
	if err != nil {
		t.Fatal(err)
	}
	q.filePath = t.TempDir() // Renaming a file over a directory must fail.

	if created, err := q.Add(&QueueItem{ID: 9}); err == nil || created {
		t.Fatalf("Add() = created %v, err %v; want persistence failure", created, err)
	}
	if q.Get(9) != nil {
		t.Fatal("failed Add() mutated in-memory state")
	}
}
