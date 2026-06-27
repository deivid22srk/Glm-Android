package api

import (
	"sync"
	"time"
)

type LogEntry struct {
	ID        int64     `json:"id"`
	Timestamp time.Time `json:"timestamp"`
	Level     string    `json:"level"`
	Event     string    `json:"event"`
	Message   string    `json:"message"`
}

type logBuffer struct {
	mu      sync.RWMutex
	nextID  int64
	entries []LogEntry
	limit   int
}

func newLogBuffer(limit int) *logBuffer {
	return &logBuffer{limit: limit, entries: make([]LogEntry, 0, limit)}
}

func (b *logBuffer) add(level, event, message string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.nextID++
	b.entries = append(b.entries, LogEntry{
		ID: b.nextID, Timestamp: time.Now().UTC(), Level: level, Event: event, Message: message,
	})
	if len(b.entries) > b.limit {
		b.entries = append([]LogEntry(nil), b.entries[len(b.entries)-b.limit:]...)
	}
}

func (b *logBuffer) list(limit int) []LogEntry {
	b.mu.RLock()
	defer b.mu.RUnlock()
	if limit <= 0 || limit > len(b.entries) {
		limit = len(b.entries)
	}
	start := len(b.entries) - limit
	out := make([]LogEntry, limit)
	copy(out, b.entries[start:])
	return out
}
