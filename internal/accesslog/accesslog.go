package accesslog

import (
	"sync"
	"time"
)

// Entry represents a single access log entry.
type Entry struct {
	Timestamp  time.Time `json:"timestamp"`
	DeviceHost string    `json:"device_host"`
	DeviceIP   string    `json:"device_ip"`
	Type       string    `json:"type"` // "restconf" or "ssh"
	Source     string    `json:"source"`
	Method     string    `json:"method,omitempty"`
	Path       string    `json:"path,omitempty"`
	Status     int       `json:"status,omitempty"`
	Command    string    `json:"command,omitempty"`
	User       string    `json:"user"`
	Detail     string    `json:"detail,omitempty"`
}

// Store is a thread-safe, bounded ring buffer of log entries.
type Store struct {
	mu        sync.RWMutex
	entries   []Entry
	maxSize   int
	listeners []chan Entry
}

// NewStore creates a store that keeps the most recent maxSize entries.
func NewStore(maxSize int) *Store {
	return &Store{
		entries: make([]Entry, 0, maxSize),
		maxSize: maxSize,
	}
}

// Add appends an entry, evicting the oldest if at capacity,
// and notifies all SSE listeners.
func (s *Store) Add(e Entry) {
	if e.Timestamp.IsZero() {
		e.Timestamp = time.Now()
	}
	s.mu.Lock()
	if len(s.entries) >= s.maxSize {
		s.entries = s.entries[1:]
	}
	s.entries = append(s.entries, e)
	// Copy listener slice under lock to avoid races
	listeners := make([]chan Entry, len(s.listeners))
	copy(listeners, s.listeners)
	s.mu.Unlock()

	// Notify outside the lock
	for _, ch := range listeners {
		select {
		case ch <- e:
		default:
			// Drop if listener is slow
		}
	}
}

// Recent returns up to n most recent entries.
func (s *Store) Recent(n int) []Entry {
	s.mu.RLock()
	defer s.mu.RUnlock()
	total := len(s.entries)
	if n > total {
		n = total
	}
	result := make([]Entry, n)
	copy(result, s.entries[total-n:])
	return result
}

// Subscribe returns a channel that receives new entries in real time.
// Call Unsubscribe with the same channel to stop.
func (s *Store) Subscribe() chan Entry {
	ch := make(chan Entry, 64)
	s.mu.Lock()
	s.listeners = append(s.listeners, ch)
	s.mu.Unlock()
	return ch
}

// Unsubscribe removes a listener channel.
func (s *Store) Unsubscribe(ch chan Entry) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, l := range s.listeners {
		if l == ch {
			s.listeners = append(s.listeners[:i], s.listeners[i+1:]...)
			close(ch)
			return
		}
	}
}
