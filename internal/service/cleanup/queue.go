package cleanup

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// QueueItem represents any media item marked for removal
type QueueItem struct {
	ID         int       `json:"id"`          // Sonarr/Radarr/etc ID
	ExternalID int       `json:"external_id"` // TVDB for shows, TMDB for movies
	Title      string    `json:"title"`
	MarkedAt   time.Time `json:"marked_at"`
	Reason     string    `json:"reason"`
	SizeOnDisk int64     `json:"size_on_disk"`
}

// Queue manages the cleanup queue persistence
type Queue struct {
	mu       sync.RWMutex
	items    map[int]*QueueItem // keyed by ID
	filePath string
}

// NewQueueWithFile creates a new queue with a specific file path
func NewQueueWithFile(path string) *Queue {
	q := &Queue{
		items:    make(map[int]*QueueItem),
		filePath: path,
	}
	_ = q.load()
	return q
}

// Add adds an item to the queue
func (q *Queue) Add(item *QueueItem) {
	q.mu.Lock()
	defer q.mu.Unlock()

	// Don't overwrite if already in queue (preserve original marked time)
	if _, exists := q.items[item.ID]; !exists {
		q.items[item.ID] = item
		_ = q.save()
	}
}

// Remove removes an item from the queue
func (q *Queue) Remove(id int) {
	q.mu.Lock()
	defer q.mu.Unlock()

	delete(q.items, id)
	_ = q.save()
}

// Get returns a queue item by ID
func (q *Queue) Get(id int) *QueueItem {
	q.mu.RLock()
	defer q.mu.RUnlock()

	return q.items[id]
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

// IsQueued checks if an item is in the queue
func (q *Queue) IsQueued(id int) bool {
	q.mu.RLock()
	defer q.mu.RUnlock()

	_, exists := q.items[id]
	return exists
}

// load reads the queue from disk
func (q *Queue) load() error {
	data, err := os.ReadFile(q.filePath)
	if err != nil {
		return err
	}

	var items []*QueueItem
	if err := json.Unmarshal(data, &items); err != nil {
		return err
	}

	for _, item := range items {
		q.items[item.ID] = item
	}

	return nil
}

// save writes the queue to disk
func (q *Queue) save() error {
	dir := filepath.Dir(q.filePath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
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

	return os.WriteFile(q.filePath, data, 0o600)
}

// Clear removes all items from the queue
func (q *Queue) Clear() {
	q.mu.Lock()
	defer q.mu.Unlock()

	q.items = make(map[int]*QueueItem)
	_ = q.save()
}
