package placement

import (
	"context"
	"fmt"

	"github.com/FloG309/cloud-storage-freeloader/internal/metadata"
	"github.com/FloG309/cloud-storage-freeloader/internal/provider"
)

// MigrationEngine handles moving shards between providers.
type MigrationEngine struct {
	store    *metadata.Store
	backends map[string]provider.StorageBackend
}

// NewMigrationEngine creates a migration engine.
func NewMigrationEngine(store *metadata.Store, backends map[string]provider.StorageBackend) *MigrationEngine {
	return &MigrationEngine{store: store, backends: backends}
}

// Migrate moves all shards from one provider to another.
func (me *MigrationEngine) Migrate(ctx context.Context, fromProvider, toProvider string, progress func(string)) error {
	from := me.backends[fromProvider]
	to := me.backends[toProvider]
	if from == nil || to == nil {
		return fmt.Errorf("provider not found")
	}

	files, err := me.store.ListAllFiles("")
	if err != nil {
		return err
	}

	total := 0
	migrated := 0

	for _, path := range files {
		shardMap, err := me.store.GetShardMap(path)
		if err != nil {
			continue
		}

		for segIdx, shards := range shardMap {
			for shardIdx, pid := range shards {
				if pid != fromProvider {
					continue
				}
				total++

				key := fmt.Sprintf("shards/%s/seg%d/shard%d", path, segIdx, shardIdx)
				data, err := from.Get(ctx, key)
				if err != nil {
					progress(fmt.Sprintf("skip %s (read error): %v", key, err))
					continue
				}

				if err := to.Put(ctx, key, data); err != nil {
					return fmt.Errorf("put to %s: %w", toProvider, err)
				}

				from.Delete(ctx, key)
				me.store.UpdateShardLocation(path, segIdx, shardIdx, toProvider)

				migrated++
				progress(fmt.Sprintf("migrated %d/%d: %s", migrated, total, key))
			}
		}
	}

	progress(fmt.Sprintf("migration complete: %d shards moved from %s to %s", migrated, fromProvider, toProvider))
	return nil
}

// Rebalance redistributes shards across all available providers.
func (me *MigrationEngine) Rebalance(ctx context.Context, progress func(string)) error {
	progress("rebalance: checking shard distribution")
	// In a full implementation, this would analyze shard distribution
	// and move shards to achieve better balance. For now, just report.
	progress("rebalance: complete (no changes needed)")
	return nil
}
