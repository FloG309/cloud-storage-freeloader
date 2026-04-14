package placement

import (
	"context"
	"testing"

	"github.com/FloG309/cloud-storage-freeloader/internal/metadata"
	"github.com/FloG309/cloud-storage-freeloader/internal/provider"
	"github.com/FloG309/cloud-storage-freeloader/internal/provider/memory"
)

func TestMigration_RemoveProvider(t *testing.T) {
	store, _ := metadata.NewStore(":memory:")
	defer store.Close()

	backends := map[string]provider.StorageBackend{
		"p1": memory.New(10 * 1024 * 1024),
		"p2": memory.New(10 * 1024 * 1024),
		"p3": memory.New(10 * 1024 * 1024),
	}

	ctx := context.Background()

	store.CreateFile(&metadata.FileMeta{Path: "/migrate.bin", Size: 100})
	store.AddShard("/migrate.bin", 0, 0, "p1")
	store.AddShard("/migrate.bin", 0, 1, "p2")
	backends["p1"].Put(ctx, "shards//migrate.bin/seg0/shard0", []byte("data0"))
	backends["p2"].Put(ctx, "shards//migrate.bin/seg0/shard1", []byte("data1"))

	me := NewMigrationEngine(store, backends)

	var progress []string
	err := me.Migrate(ctx, "p1", "p3", func(msg string) {
		progress = append(progress, msg)
	})
	if err != nil {
		t.Fatalf("Migrate failed: %v", err)
	}

	// Verify shard moved
	shardMap, _ := store.GetShardMap("/migrate.bin")
	if shardMap[0][0] != "p3" {
		t.Fatalf("shard not migrated: got provider %s, want p3", shardMap[0][0])
	}

	// Verify data exists on new provider
	data, err := backends["p3"].Get(ctx, "shards//migrate.bin/seg0/shard0")
	if err != nil {
		t.Fatalf("data not on new provider: %v", err)
	}
	if string(data) != "data0" {
		t.Fatal("migrated data mismatch")
	}
}

func TestMigration_AddProvider(t *testing.T) {
	store, _ := metadata.NewStore(":memory:")
	defer store.Close()

	backends := map[string]provider.StorageBackend{
		"p1": memory.New(10 * 1024 * 1024),
		"p2": memory.New(10 * 1024 * 1024),
	}

	me := NewMigrationEngine(store, backends)

	// Adding a provider is a no-error operation
	backends["p3"] = memory.New(10 * 1024 * 1024)
	me.backends = backends

	err := me.Rebalance(context.Background(), func(msg string) {})
	if err != nil {
		t.Fatalf("Rebalance failed: %v", err)
	}
}

func TestMigration_ProgressCallback(t *testing.T) {
	store, _ := metadata.NewStore(":memory:")
	defer store.Close()

	backends := map[string]provider.StorageBackend{
		"p1": memory.New(10 * 1024 * 1024),
		"p2": memory.New(10 * 1024 * 1024),
	}

	ctx := context.Background()
	store.CreateFile(&metadata.FileMeta{Path: "/a.bin", Size: 10})
	store.AddShard("/a.bin", 0, 0, "p1")
	backends["p1"].Put(ctx, "shards//a.bin/seg0/shard0", []byte("data"))

	store.CreateFile(&metadata.FileMeta{Path: "/b.bin", Size: 10})
	store.AddShard("/b.bin", 0, 0, "p1")
	backends["p1"].Put(ctx, "shards//b.bin/seg0/shard0", []byte("data"))

	me := NewMigrationEngine(store, backends)

	var progress []string
	me.Migrate(ctx, "p1", "p2", func(msg string) {
		progress = append(progress, msg)
	})

	if len(progress) == 0 {
		t.Fatal("expected progress callbacks")
	}
}
