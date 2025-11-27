package cleanup

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const queueFile = "data/cleanup_queue.json"

// QueueItem represents a series marked for removal
type QueueItem struct {
	SonarrID   int       `json:"sonarr_id"`
	TvdbID     int       `json:"tvdb_id"`
	Title      string    `json:"title"`
	MarkedAt   time.Time `json:"marked_at"`
	Reason     string    `json:"reason"`
	SizeOnDisk int64     `json:"size_on_disk"`
}

// Queue manages the cleanup queue persistence
type Queue struct {
	mu    sync.RWMutex
	items map[int]*QueueItem // keyed by Sonarr ID
}

// NewQueue creates a new queue and loads existing data
func NewQueue() *Queue {
	q := &Queue{
		items: make(map[int]*QueueItem),
	}
	_ = q.load()
	return q
}

// Add adds a series to the cleanup queue
func (q *Queue) Add(item *QueueItem) {
	q.mu.Lock()
	defer q.mu.Unlock()

	// Don't overwrite if already in queue (preserve original marked time)
	if _, exists := q.items[item.SonarrID]; !exists {
		q.items[item.SonarrID] = item
		_ = q.save()
	}
}

// Remove removes a series from the queue
func (q *Queue) Remove(sonarrID int) {
	q.mu.Lock()
	defer q.mu.Unlock()

	delete(q.items, sonarrID)
	_ = q.save()
}

// Get returns a queue item by Sonarr ID
func (q *Queue) Get(sonarrID int) *QueueItem {
	q.mu.RLock()
	defer q.mu.RUnlock()

	return q.items[sonarrID]
}

// GetAll returns all items in the queue
func (q *Queue) GetAll() []*QueueItem {
	q.mu.RLock()
	defer q.mu.RUnlock()

	items := make([]*QueueItem, 0, len(q.items))
	for _, item := range q.items {
		items = append(items, item)
	}
	return items
}

// GetReadyForRemoval returns items that have passed the delay period
func (q *Queue) GetReadyForRemoval(delayDays int) []*QueueItem {
	q.mu.RLock()
	defer q.mu.RUnlock()

	delay := time.Duration(delayDays) * 24 * time.Hour
	cutoff := time.Now().Add(-delay)

	var ready []*QueueItem
	for _, item := range q.items {
		if item.MarkedAt.Before(cutoff) {
			ready = append(ready, item)
		}
	}
	return ready
}

// IsQueued checks if a series is in the queue
func (q *Queue) IsQueued(sonarrID int) bool {
	q.mu.RLock()
	defer q.mu.RUnlock()

	_, exists := q.items[sonarrID]
	return exists
}

// load reads the queue from disk
func (q *Queue) load() error {
	data, err := os.ReadFile(queueFile)
	if err != nil {
		return err
	}

	var items []*QueueItem
	if err := json.Unmarshal(data, &items); err != nil {
		return err
	}

	for _, item := range items {
		q.items[item.SonarrID] = item
	}

	return nil
}

// save writes the queue to disk
func (q *Queue) save() error {
	dir := filepath.Dir(queueFile)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	items := make([]*QueueItem, 0, len(q.items))
	for _, item := range q.items {
		items = append(items, item)
	}

	data, err := json.MarshalIndent(items, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(queueFile, data, 0600)
}

// Clear removes all items from the queue
func (q *Queue) Clear() {
	q.mu.Lock()
	defer q.mu.Unlock()

	q.items = make(map[int]*QueueItem)
	_ = q.save()
}

