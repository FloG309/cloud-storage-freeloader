package livesync

import (
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// EventType describes a filesystem event.
type EventType int

const (
	EventCreated  EventType = iota
	EventModified
	EventDeleted
	EventRenamed
)

// FileEvent represents a detected filesystem change.
type FileEvent struct {
	Path      string
	Type      EventType
	Timestamp time.Time
}

// Watcher monitors a directory for file changes.
type Watcher struct {
	fsWatcher  *fsnotify.Watcher
	events     chan FileEvent
	ignore     []string
	debounce   time.Duration
	selfWrites map[string]bool
	mu         sync.Mutex
	done       chan struct{}
	lastEvent  map[string]time.Time
}

// NewWatcher creates a filesystem watcher for the given directory.
func NewWatcher(dir string, ignore []string, debounce time.Duration) (*Watcher, error) {
	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	if err := fsw.Add(dir); err != nil {
		fsw.Close()
		return nil, err
	}

	w := &Watcher{
		fsWatcher:  fsw,
		events:     make(chan FileEvent, 100),
		ignore:     ignore,
		debounce:   debounce,
		selfWrites: make(map[string]bool),
		done:       make(chan struct{}),
		lastEvent:  make(map[string]time.Time),
	}

	go w.run()
	return w, nil
}

func (w *Watcher) run() {
	for {
		select {
		case event, ok := <-w.fsWatcher.Events:
			if !ok {
				return
			}
			w.handleEvent(event)
		case <-w.done:
			return
		}
	}
}

func (w *Watcher) handleEvent(event fsnotify.Event) {
	path := filepath.Clean(event.Name)

	// Check ignore patterns
	for _, pattern := range w.ignore {
		if matched, _ := filepath.Match(pattern, filepath.Base(path)); matched {
			return
		}
	}

	// Check self-write filter (keeps filtering for debounce duration)
	w.mu.Lock()
	if w.selfWrites[path] {
		// Don't delete — let it filter all events within the debounce window
		w.mu.Unlock()
		return
	}

	// Debounce: skip if too recent
	now := time.Now()
	if last, ok := w.lastEvent[path]; ok && now.Sub(last) < w.debounce {
		w.mu.Unlock()
		return
	}
	w.lastEvent[path] = now
	w.mu.Unlock()

	var evType EventType
	switch {
	case event.Op&fsnotify.Create != 0:
		evType = EventCreated
	case event.Op&fsnotify.Write != 0:
		evType = EventModified
	case event.Op&fsnotify.Remove != 0:
		evType = EventDeleted
	case event.Op&fsnotify.Rename != 0:
		evType = EventRenamed
	default:
		return
	}

	select {
	case w.events <- FileEvent{Path: path, Type: evType, Timestamp: now}:
	default:
		// Drop if channel full
	}
}

// Events returns the channel of file events.
func (w *Watcher) Events() <-chan FileEvent {
	return w.events
}

// MarkSelfWrite marks a path as a self-write to filter from events.
func (w *Watcher) MarkSelfWrite(path string) {
	cleanPath := filepath.Clean(path)
	w.mu.Lock()
	w.selfWrites[cleanPath] = true
	w.mu.Unlock()
}

// Close stops the watcher.
func (w *Watcher) Close() error {
	close(w.done)
	return w.fsWatcher.Close()
}
