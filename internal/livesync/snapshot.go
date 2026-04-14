package livesync

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/FloG309/cloud-storage-freeloader/internal/provider"
)

type snapshotProvider struct {
	id      string
	backend provider.StorageBackend
}

type snapshotEntry struct {
	Content []byte `json:"content"`
	Seq     int64  `json:"seq"`
}

// SnapshotManager creates and restores full-file snapshots.
type SnapshotManager struct {
	providers      []snapshotProvider
	patchThreshold int
	mu             sync.Mutex
	lastSeqs       map[string]int64
}

// NewSnapshotManager creates a snapshot manager.
func NewSnapshotManager(providers []snapshotProvider, patchThreshold int) *SnapshotManager {
	return &SnapshotManager{
		providers:      providers,
		patchThreshold: patchThreshold,
		lastSeqs:       make(map[string]int64),
	}
}

// CreateSnapshot stores a snapshot of the file content.
func (sm *SnapshotManager) CreateSnapshot(ctx context.Context, path string, content []byte, seq int64) error {
	entry := snapshotEntry{Content: content, Seq: seq}
	data, err := json.Marshal(entry)
	if err != nil {
		return err
	}

	key := fmt.Sprintf(".cloudfs/snapshots/%s/latest.json", path)
	for _, p := range sm.providers {
		p.backend.Put(ctx, key, data)
	}

	sm.mu.Lock()
	sm.lastSeqs[path] = seq
	sm.mu.Unlock()
	return nil
}

// RestoreSnapshot retrieves the latest snapshot for a file.
func (sm *SnapshotManager) RestoreSnapshot(ctx context.Context, path string) ([]byte, error) {
	key := fmt.Sprintf(".cloudfs/snapshots/%s/latest.json", path)
	for _, p := range sm.providers {
		data, err := p.backend.Get(ctx, key)
		if err != nil {
			continue
		}
		var entry snapshotEntry
		if err := json.Unmarshal(data, &entry); err != nil {
			continue
		}
		return entry.Content, nil
	}
	return nil, fmt.Errorf("no snapshot found for %s", path)
}

// ShouldSnapshot returns true if the patch count has reached the threshold.
func (sm *SnapshotManager) ShouldSnapshot(patchCount int) bool {
	return patchCount >= sm.patchThreshold
}

// LastSnapshotSeq returns the last snapshot sequence for a file.
func (sm *SnapshotManager) LastSnapshotSeq(path string) int64 {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	return sm.lastSeqs[path]
}
