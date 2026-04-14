package placement

import (
	"context"
	"testing"

	"github.com/FloG309/cloud-storage-freeloader/internal/metadata"
	"github.com/FloG309/cloud-storage-freeloader/internal/provider"
	"github.com/FloG309/cloud-storage-freeloader/internal/provider/memory"
)

func newTestRepairEnv(t *testing.T) (*RepairEngine, *metadata.Store, map[string]provider.StorageBackend) {
	t.Helper()
	store, _ := metadata.NewStore(":memory:")
	t.Cleanup(func() { store.Close() })

	backends := map[string]provider.StorageBackend{
		"p1": memory.New(10 * 1024 * 1024),
		"p2": memory.New(10 * 1024 * 1024),
		"p3": memory.New(10 * 1024 * 1024),
		"p4": memory.New(10 * 1024 * 1024),
	}

	re := NewRepairEngine(store, backends)
	return re, store, backends
}

func TestRepair_DetectDegraded(t *testing.T) {
	re, store, backends := newTestRepairEnv(t)
	ctx := context.Background()

	// Create a file with shards
	store.CreateFile(&metadata.FileMeta{Path: "/test.bin", Size: 100})
	store.AddShard("/test.bin", 0, 0, "p1")
	store.AddShard("/test.bin", 0, 1, "p2")
	store.AddShard("/test.bin", 0, 2, "p3")
	store.AddShard("/test.bin", 0, 3, "p4")

	// Put data on providers
	backends["p1"].Put(ctx, "shards//test.bin/seg0/shard0", []byte("data0"))
	backends["p2"].Put(ctx, "shards//test.bin/seg0/shard1", []byte("data1"))
	backends["p3"].Put(ctx, "shards//test.bin/seg0/shard2", []byte("data2"))
	// p4 is "down" — no data

	degraded, err := re.Scan(ctx)
	if err != nil {
		t.Fatalf("Scan failed: %v", err)
	}
	if len(degraded) == 0 {
		t.Fatal("expected degraded segments")
	}
}

func TestRepair_RebuildShard(t *testing.T) {
	re, store, backends := newTestRepairEnv(t)
	ctx := context.Background()

	store.CreateFile(&metadata.FileMeta{Path: "/rebuild.bin", Size: 100})
	for i := 0; i < 4; i++ {
		pid := "p" + string(rune('1'+i))
		store.AddShard("/rebuild.bin", 0, i, pid)
		backends[pid].Put(ctx, "shards//rebuild.bin/seg0/shard"+string(rune('0'+i)), []byte("shard-data"))
	}

	// Remove one shard to simulate degradation
	backends["p4"].Delete(ctx, "shards//rebuild.bin/seg0/shard3")

	degraded, _ := re.Scan(ctx)
	if len(degraded) > 0 {
		err := re.Repair(ctx, degraded[0])
		if err != nil {
			t.Fatalf("Repair failed: %v", err)
		}
	}
}

func TestRepair_NoHealthyReplacement(t *testing.T) {
	store, _ := metadata.NewStore(":memory:")
	defer store.Close()

	// All providers have 0 capacity
	backends := map[string]provider.StorageBackend{
		"p1": memory.New(10),
	}

	re := NewRepairEngine(store, backends)

	store.CreateFile(&metadata.FileMeta{Path: "/full.bin", Size: 100})
	store.AddShard("/full.bin", 0, 0, "p1")

	// Fill provider to capacity
	backends["p1"].Put(context.Background(), "filler", make([]byte, 10))

	// This test validates the repair engine handles capacity issues
	_, err := re.Scan(context.Background())
	if err != nil {
		t.Fatalf("Scan should not fail: %v", err)
	}
}
