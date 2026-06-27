package requestqueue

import (
	"context"
	"strings"
	"sync"
)

type Queue struct {
	mu    sync.Mutex
	gates map[string]*gate
}

type gate struct {
	busy bool
	wait []chan struct{}
}

type Lease struct {
	key      string
	queue    *Queue
	position int
	once     sync.Once
}

type SnapshotItem struct {
	Key     string `json:"key"`
	Active  bool   `json:"active"`
	Waiting int    `json:"waiting"`
}

func New() *Queue {
	return &Queue{gates: map[string]*gate{}}
}

func Key(accountID string, modelID string) string {
	accountID = strings.TrimSpace(accountID)
	modelID = strings.TrimSpace(modelID)
	if accountID == "" {
		accountID = "default"
	}
	if modelID == "" {
		modelID = "unknown"
	}
	return accountID + ":" + strings.ToLower(modelID)
}

func (q *Queue) Acquire(ctx context.Context, key string) (*Lease, error) {
	if key == "" {
		key = Key("", "")
	}
	q.mu.Lock()
	current := q.gates[key]
	if current == nil {
		current = &gate{}
		q.gates[key] = current
	}
	if !current.busy {
		current.busy = true
		q.mu.Unlock()
		return &Lease{key: key, queue: q, position: 0}, nil
	}
	waiter := make(chan struct{})
	current.wait = append(current.wait, waiter)
	position := len(current.wait)
	q.mu.Unlock()

	select {
	case <-waiter:
		return &Lease{key: key, queue: q, position: position}, nil
	case <-ctx.Done():
		q.removeWaiter(key, waiter)
		return nil, ctx.Err()
	}
}

func (q *Queue) Busy(key string) bool {
	if key == "" {
		key = Key("", "")
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	current := q.gates[key]
	return current != nil && current.busy
}

func (q *Queue) Snapshot() []SnapshotItem {
	q.mu.Lock()
	defer q.mu.Unlock()
	items := make([]SnapshotItem, 0, len(q.gates))
	for key, current := range q.gates {
		items = append(items, SnapshotItem{Key: key, Active: current.busy, Waiting: len(current.wait)})
	}
	return items
}

func (l *Lease) Release() {
	if l == nil || l.queue == nil {
		return
	}
	l.once.Do(func() {
		l.queue.release(l.key)
	})
}

func (l *Lease) Position() int {
	if l == nil {
		return 0
	}
	return l.position
}

func (q *Queue) release(key string) {
	q.mu.Lock()
	defer q.mu.Unlock()
	current := q.gates[key]
	if current == nil {
		return
	}
	if len(current.wait) == 0 {
		delete(q.gates, key)
		return
	}
	next := current.wait[0]
	current.wait = current.wait[1:]
	current.busy = true
	close(next)
}

func (q *Queue) removeWaiter(key string, waiter chan struct{}) {
	q.mu.Lock()
	defer q.mu.Unlock()
	current := q.gates[key]
	if current == nil {
		return
	}
	for index, candidate := range current.wait {
		if candidate == waiter {
			current.wait = append(current.wait[:index], current.wait[index+1:]...)
			break
		}
	}
	if !current.busy && len(current.wait) == 0 {
		delete(q.gates, key)
	}
}
