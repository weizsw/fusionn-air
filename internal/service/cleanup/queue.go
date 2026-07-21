package cleanup

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// QueueItem represents media scheduled for removal.
type QueueItem struct {
	ID            int        `json:"id"`
	ExternalID    int        `json:"external_id"`
	Title         string     `json:"title"`
	MarkedAt      time.Time  `json:"marked_at"`
	UnmonitoredAt *time.Time `json:"unmonitored_at,omitempty"`
	Reason        string     `json:"reason"`
	SizeOnDisk    int64      `json:"size_on_disk"`
}

type candidateState uint8

const (
	candidateWaiting candidateState = iota
	candidatePendingUnmonitor
	candidateReady
)

// Queue owns durable Removal Candidate state.
type Queue struct {
	mu                sync.RWMutex
	items             map[int]*QueueItem
	filePath          string
	requiresUnmonitor bool
	now               func() time.Time
}

// NewQueueWithFile loads a queue. A missing file is an empty queue; any other
// load failure is returned because cleanup state would be unknown.
func NewQueueWithFile(path string, requiresUnmonitor bool) (*Queue, error) {
	q := &Queue{
		items:             make(map[int]*QueueItem),
		filePath:          path,
		requiresUnmonitor: requiresUnmonitor,
		now:               time.Now,
	}
	if err := q.load(); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("load cleanup queue %q: %w", path, err)
	}
	return q, nil
}

// Add durably adds an item. It reports whether a new item was created.
func (q *Queue) Add(item *QueueItem) (bool, error) {
	if item == nil || item.ID == 0 {
		return false, errors.New("queue item requires a non-zero ID")
	}

	q.mu.Lock()
	defer q.mu.Unlock()
	if _, exists := q.items[item.ID]; exists {
		return false, nil
	}

	next := cloneItems(q.items)
	item = cloneItem(item)
	if item.MarkedAt.IsZero() {
		item.MarkedAt = q.now()
	}
	next[item.ID] = item
	if err := q.save(next); err != nil {
		return false, err
	}
	q.items = next
	return true, nil
}

// Remove durably removes an item.
func (q *Queue) Remove(id int) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	if _, exists := q.items[id]; !exists {
		return nil
	}

	next := cloneItems(q.items)
	delete(next, id)
	if err := q.save(next); err != nil {
		return err
	}
	q.items = next
	return nil
}

// MarkUnmonitored durably records when monitoring stopped. The deletion delay
// for monitored media begins at this transition.
func (q *Queue) MarkUnmonitored(id int) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	item, exists := q.items[id]
	if !exists || item.UnmonitoredAt != nil {
		return nil
	}

	next := cloneItems(q.items)
	at := q.now()
	next[id].UnmonitoredAt = &at
	if err := q.save(next); err != nil {
		return err
	}
	q.items = next
	return nil
}

// Get returns an isolated snapshot of one item.
func (q *Queue) Get(id int) *QueueItem {
	q.mu.RLock()
	defer q.mu.RUnlock()
	return cloneItem(q.items[id])
}

// GetAll returns isolated snapshots ordered by ID.
func (q *Queue) GetAll() []*QueueItem {
	q.mu.RLock()
	defer q.mu.RUnlock()
	return sortedItems(q.items)
}

// status returns the durable state and whole days remaining for an item.
func (q *Queue) status(id, delayDays int) (*QueueItem, candidateState, int) {
	q.mu.RLock()
	defer q.mu.RUnlock()
	item := q.items[id]
	if item == nil {
		return nil, candidateWaiting, 0
	}

	if q.requiresUnmonitor && item.UnmonitoredAt == nil {
		return cloneItem(item), candidatePendingUnmonitor, delayDays
	}

	startedAt := item.MarkedAt
	if q.requiresUnmonitor {
		startedAt = *item.UnmonitoredAt
	}
	dueAt := startedAt.Add(time.Duration(delayDays) * 24 * time.Hour)
	remaining := dueAt.Sub(q.now())
	if remaining <= 0 {
		return cloneItem(item), candidateReady, 0
	}
	daysUntil := int((remaining + 24*time.Hour - 1) / (24 * time.Hour))
	return cloneItem(item), candidateWaiting, daysUntil
}

func (q *Queue) NeedsUnmonitor(id int) bool {
	q.mu.RLock()
	defer q.mu.RUnlock()
	item := q.items[id]
	return q.requiresUnmonitor && item != nil && item.UnmonitoredAt == nil
}

func (q *Queue) GetReadyForRemoval(delayDays int) []*QueueItem {
	q.mu.RLock()
	defer q.mu.RUnlock()

	ready := make(map[int]*QueueItem)
	now := q.now()
	for id, item := range q.items {
		if q.requiresUnmonitor && item.UnmonitoredAt == nil {
			continue
		}
		startedAt := item.MarkedAt
		if q.requiresUnmonitor {
			startedAt = *item.UnmonitoredAt
		}
		if !now.Before(startedAt.Add(time.Duration(delayDays) * 24 * time.Hour)) {
			ready[id] = item
		}
	}
	return sortedItems(ready)
}

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
		if item == nil || item.ID == 0 {
			return errors.New("queue contains an item with an invalid ID")
		}
		if _, exists := q.items[item.ID]; exists {
			return fmt.Errorf("queue contains duplicate ID %d", item.ID)
		}
		q.items[item.ID] = cloneItem(item)
	}
	return nil
}

// save writes a complete snapshot to a temporary file and atomically replaces
// the previous snapshot only after the new contents reach disk.
func (q *Queue) save(items map[int]*QueueItem) (err error) {
	dir := filepath.Dir(q.filePath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	f, err := os.CreateTemp(dir, ".queue-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer func() {
		_ = f.Close()
		_ = os.Remove(tmp)
	}()

	if err := f.Chmod(0o600); err != nil {
		return err
	}
	encoder := json.NewEncoder(f)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(sortedItems(items)); err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp, q.filePath); err != nil {
		return err
	}
	directory, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}

func cloneItems(items map[int]*QueueItem) map[int]*QueueItem {
	cloned := make(map[int]*QueueItem, len(items))
	for id, item := range items {
		cloned[id] = cloneItem(item)
	}
	return cloned
}

func cloneItem(item *QueueItem) *QueueItem {
	if item == nil {
		return nil
	}
	cloned := *item
	if item.UnmonitoredAt != nil {
		at := *item.UnmonitoredAt
		cloned.UnmonitoredAt = &at
	}
	return &cloned
}

func sortedItems(items map[int]*QueueItem) []*QueueItem {
	ids := make([]int, 0, len(items))
	for id := range items {
		ids = append(ids, id)
	}
	sort.Ints(ids)

	result := make([]*QueueItem, 0, len(ids))
	for _, id := range ids {
		result = append(result, cloneItem(items[id]))
	}
	return result
}
