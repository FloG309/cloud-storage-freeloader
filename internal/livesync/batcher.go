package livesync

import (
	"sync"
	"time"
)

// BatcherConfig configures the batcher behavior.
type BatcherConfig struct {
	FlushInterval time.Duration
	MaxPatches    int
	MaxBytes      int64
}

// Batcher accumulates patches and flushes on timer or threshold.
type Batcher struct {
	config  BatcherConfig
	onFlush func([]FilePatches) error
	mu      sync.Mutex
	buffer  []FilePatches
	size    int64
	ticker  *time.Ticker
	done    chan struct{}
}

// NewBatcher creates a batcher with the given config and flush callback.
func NewBatcher(config BatcherConfig, onFlush func([]FilePatches) error) *Batcher {
	b := &Batcher{
		config:  config,
		onFlush: onFlush,
		ticker:  time.NewTicker(config.FlushInterval),
		done:    make(chan struct{}),
	}
	go b.run()
	return b
}

func (b *Batcher) run() {
	for {
		select {
		case <-b.ticker.C:
			b.flush()
		case <-b.done:
			return
		}
	}
}

// Add adds a patch to the batch. May trigger immediate flush if threshold exceeded.
func (b *Batcher) Add(fp FilePatches) {
	b.mu.Lock()
	b.buffer = append(b.buffer, fp)

	patchSize := int64(0)
	for _, p := range fp.Patches {
		patchSize += int64(len(p.Data))
	}
	b.size += patchSize

	shouldFlush := len(b.buffer) >= b.config.MaxPatches || b.size >= b.config.MaxBytes
	b.mu.Unlock()

	if shouldFlush {
		b.flush()
	}
}

func (b *Batcher) flush() {
	b.mu.Lock()
	if len(b.buffer) == 0 {
		b.mu.Unlock()
		return
	}
	batch := b.buffer
	b.buffer = nil
	b.size = 0
	b.mu.Unlock()

	b.onFlush(batch)
}

// Stop stops the batcher.
func (b *Batcher) Stop() {
	b.ticker.Stop()
	close(b.done)
}
