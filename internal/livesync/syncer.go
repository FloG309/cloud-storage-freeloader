package livesync

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/FloG309/cloud-storage-freeloader/internal/provider"
)

// Syncer pushes and pulls patch batches between devices via providers.
type Syncer struct {
	deviceID  string
	providers []provider.StorageBackend
	pushSeq   int64
	pulled    map[string]bool // tracks already-pulled batch keys
}

// NewSyncer creates a syncer for the given device.
func NewSyncer(deviceID string, providers []provider.StorageBackend) *Syncer {
	return &Syncer{
		deviceID:  deviceID,
		providers: providers,
		pulled:    make(map[string]bool),
	}
}

// Push uploads a batch to all hot providers.
func (s *Syncer) Push(ctx context.Context, batch []FilePatches) error {
	s.pushSeq++
	data, err := json.Marshal(batch)
	if err != nil {
		return err
	}

	key := fmt.Sprintf(".cloudfs/patches/%s/%d.batch", s.deviceID, s.pushSeq)
	for _, p := range s.providers {
		p.Put(ctx, key, data) // best-effort
	}
	s.pulled[key] = true // don't pull our own batches
	return nil
}

// Pull downloads new batch files from providers.
func (s *Syncer) Pull(ctx context.Context) ([]FilePatches, error) {
	var result []FilePatches

	for _, p := range s.providers {
		keys, err := p.List(ctx, ".cloudfs/patches/")
		if err != nil {
			continue
		}

		for _, key := range keys {
			if s.pulled[key] {
				continue
			}
			// Skip our own batches
			if strings.Contains(key, "/"+s.deviceID+"/") {
				s.pulled[key] = true
				continue
			}

			data, err := p.Get(ctx, key)
			if err != nil {
				continue
			}

			var batch []FilePatches
			if err := json.Unmarshal(data, &batch); err != nil {
				continue
			}

			result = append(result, batch...)
			s.pulled[key] = true
		}
	}

	return result, nil
}
