// Package events implements a tiny in-process pub/sub used to fan
// changes out to WebSocket subscribers and to the background reminder
// ticker.
//
// The Hub has zero external dependencies. Subscribers get a buffered
// channel and can range over it. Publishers call Publish and never
// block: if a subscriber's buffer is full, the event is dropped for
// that subscriber only (with a counter so tests can assert).
package events

import (
	"sync"
	"sync/atomic"
)

// Event kinds. Keep this list small; consumers switch on Kind.
const (
	KindTaskCreated   = "task.created"
	KindTaskUpdated   = "task.updated"
	KindTaskDeleted   = "task.deleted"
	KindTaskCompleted = "task.completed"
	KindTimerStarted  = "timer.started"
	KindTimerStopped  = "timer.stopped"
	KindReminderFired = "reminder.fired"
)

// Event is what flows through the hub.
type Event struct {
	Kind     string         `json:"kind"`
	TenantID string         `json:"tenant_id"`
	UserID   string         `json:"user_id,omitempty"`
	Payload  map[string]any `json:"payload"`
}

// Hub is the central broker.
type Hub struct {
	mu      sync.RWMutex
	subs    map[chan Event]struct{}
	dropped atomic.Int64
}

// New returns a new Hub.
func New() *Hub {
	return &Hub{subs: map[chan Event]struct{}{}}
}

// Publish fans out e to all current subscribers. Non-blocking.
func (h *Hub) Publish(e Event) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for ch := range h.subs {
		select {
		case ch <- e:
		default:
			h.dropped.Add(1)
		}
	}
}

// Subscribe returns a buffered channel that receives future events.
// Call the returned cancel func to unsubscribe.
func (h *Hub) Subscribe(buf int) (<-chan Event, func()) {
	if buf <= 0 {
		buf = 32
	}
	ch := make(chan Event, buf)
	h.mu.Lock()
	h.subs[ch] = struct{}{}
	h.mu.Unlock()
	return ch, func() {
		h.mu.Lock()
		if _, ok := h.subs[ch]; ok {
			delete(h.subs, ch)
			close(ch)
		}
		h.mu.Unlock()
	}
}

// Dropped returns the count of events dropped due to slow consumers.
func (h *Hub) Dropped() int64 { return h.dropped.Load() }

// Count returns the current number of subscribers. Useful for tests.
func (h *Hub) Count() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.subs)
}
