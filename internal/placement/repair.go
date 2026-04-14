package placement

import (
	"context"
	"fmt"

	"github.com/FloG309/cloud-storage-freeloader/internal/metadata"
	"github.com/FloG309/cloud-storage-freeloader/internal/provider"
)

// DegradedSegment describes a segment with missing shards.
type DegradedSegment struct {
	FilePath     string
	SegmentIndex int
	MissingShards []int
	ProviderMap  map[int]string
}

// RepairEngine scans for and repairs degraded segments.
type RepairEngine struct {
	store    *metadata.Store
	backends map[string]provider.StorageBackend
}

// NewRepairEngine creates a repair engine.
func NewRepairEngine(store *metadata.Store, backends map[string]provider.StorageBackend) *RepairEngine {
	return &RepairEngine{store: store, backends: backends}
}

// Scan finds all degraded segments (segments with missing shards on providers).
func (re *RepairEngine) Scan(ctx context.Context) ([]DegradedSegment, error) {
	files, err := re.store.ListAllFiles("")
	if err != nil {
		return nil, err
	}

	var degraded []DegradedSegment
	for _, path := range files {
		shardMap, err := re.store.GetShardMap(path)
		if err != nil {
			continue
		}

		for segIdx, shards := range shardMap {
			var missing []int
			for shardIdx, pid := range shards {
				backend := re.backends[pid]
				if backend == nil {
					missing = append(missing, shardIdx)
					continue
				}
				key := fmt.Sprintf("shards/%s/seg%d/shard%d", path, segIdx, shardIdx)
				exists, _ := backend.Exists(ctx, key)
				if !exists {
					missing = append(missing, shardIdx)
				}
			}
			if len(missing) > 0 {
				degraded = append(degraded, DegradedSegment{
					FilePath:      path,
					SegmentIndex:  segIdx,
					MissingShards: missing,
					ProviderMap:   shards,
				})
			}
		}
	}

	return degraded, nil
}

// Repair attempts to rebuild missing shards in a degraded segment.
func (re *RepairEngine) Repair(ctx context.Context, seg DegradedSegment) error {
	// In a full implementation, this would:
	// 1. Download k available shards
	// 2. Re-encode using Reed-Solomon
	// 3. Upload replacement shards to new providers
	// For now, just validate that we have enough healthy shards
	healthyCount := len(seg.ProviderMap) - len(seg.MissingShards)
	if healthyCount < 2 { // minimum k=2
		return fmt.Errorf("not enough healthy shards: have %d, need at least 2", healthyCount)
	}
	return nil
}
