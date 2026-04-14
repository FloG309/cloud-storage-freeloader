package vfs

import (
	"bytes"
	"context"
	"testing"

	"github.com/FloG309/cloud-storage-freeloader/internal/erasure"
	"github.com/FloG309/cloud-storage-freeloader/internal/metadata"
	"github.com/FloG309/cloud-storage-freeloader/internal/placement"
	"github.com/FloG309/cloud-storage-freeloader/internal/provider"
	"github.com/FloG309/cloud-storage-freeloader/internal/provider/memory"
)

func newTestVFS(t *testing.T) *VFS {
	t.Helper()
	return newTestVFSWithProviders(t, makeTestProviders(4, provider.TierHot))
}

func newTestVFSWithProviders(t *testing.T, providerInfos []placement.ProviderInfo) *VFS {
	t.Helper()
	backends := make(map[string]provider.StorageBackend)
	for _, p := range providerInfos {
		backends[p.ID] = memory.New(p.Available)
	}

	store, err := metadata.NewStore(":memory:")
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	opLog, _ := metadata.NewOpLog(store.DB())
	t.Cleanup(func() { store.Close() })

	enc, _ := erasure.NewEncoder(2, 2)
	chunker := erasure.NewChunker(64) // small segments for testing
	engine := placement.NewEngine(providerInfos)
	cache := NewSegmentCache(1024 * 1024)

	return NewVFS(store, opLog, engine, enc, chunker, cache, backends)
}

func makeTestProviders(n int, tier provider.StorageTier) []placement.ProviderInfo {
	var infos []placement.ProviderInfo
	for i := 0; i < n; i++ {
		p := provider.ProviderProfile{}
		switch tier {
		case provider.TierWarm:
			p.DailyEgressLimit = 1 * 1024 * 1024 * 1024
		case provider.TierCold:
			p.DailyEgressLimit = 100 * 1024 * 1024
		}
		id := string(rune('A' + i))
		infos = append(infos, placement.ProviderInfo{
			ID:        id,
			Profile:   p,
			Tracker:   provider.NewBandwidthTracker(p.DailyEgressLimit, 0),
			Available: 10 * 1024 * 1024,
		})
	}
	return infos
}

// Phase 5.2 tests

func TestVFS_ReadFile(t *testing.T) {
	v := newTestVFS(t)
	ctx := context.Background()

	data := []byte("Hello VFS world, this is a test file with enough data!")
	if err := v.Write(ctx, "/test.txt", bytes.NewReader(data), int64(len(data))); err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	got, err := v.Read(ctx, "/test.txt", 0, int64(len(data)))
	if err != nil {
		t.Fatalf("Read failed: %v", err)
	}
	if !bytes.Equal(got, data) {
		t.Fatalf("got %q, want %q", got, data)
	}
}

func TestVFS_ReadFilePartial(t *testing.T) {
	v := newTestVFS(t)
	ctx := context.Background()

	data := make([]byte, 256)
	for i := range data {
		data[i] = byte(i)
	}
	v.Write(ctx, "/partial.bin", bytes.NewReader(data), int64(len(data)))

	got, err := v.Read(ctx, "/partial.bin", 100, 100)
	if err != nil {
		t.Fatalf("Read partial failed: %v", err)
	}
	if !bytes.Equal(got, data[100:200]) {
		t.Fatal("partial read data mismatch")
	}
}

func TestVFS_ReadFileDegraded(t *testing.T) {
	v := newTestVFS(t)
	ctx := context.Background()

	data := []byte("Degraded read test with enough bytes to fill segments properly!!")
	v.Write(ctx, "/degraded.txt", bytes.NewReader(data), int64(len(data)))

	// Simulate provider failure by clearing one provider's data
	for _, backend := range v.backends {
		keys, _ := backend.List(ctx, "")
		for _, key := range keys {
			backend.Delete(ctx, key)
			// Only delete from one provider
			goto done
		}
	}
done:

	got, err := v.Read(ctx, "/degraded.txt", 0, int64(len(data)))
	if err != nil {
		t.Fatalf("Degraded read failed: %v", err)
	}
	if !bytes.Equal(got, data) {
		t.Fatal("degraded read data mismatch")
	}
}

func TestVFS_ReadFileNotFound(t *testing.T) {
	v := newTestVFS(t)
	_, err := v.Read(context.Background(), "/nonexistent.txt", 0, 100)
	if err == nil {
		t.Fatal("expected error for non-existent file")
	}
}

func TestVFS_ReadDir(t *testing.T) {
	v := newTestVFS(t)
	ctx := context.Background()

	for _, name := range []string{"/docs/a.txt", "/docs/b.txt", "/docs/c.txt"} {
		data := []byte("content of " + name)
		v.Write(ctx, name, bytes.NewReader(data), int64(len(data)))
	}

	entries, err := v.ReadDir(ctx, "/docs/")
	if err != nil {
		t.Fatalf("ReadDir failed: %v", err)
	}
	if len(entries) != 3 {
		t.Fatalf("got %d entries, want 3", len(entries))
	}
}

// Phase 5.3 tests

func TestVFS_WriteNewFile(t *testing.T) {
	v := newTestVFS(t)
	ctx := context.Background()

	data := []byte("brand new file content that spans multiple segments!!")
	err := v.Write(ctx, "/new.txt", bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	// File should appear in metadata
	info, err := v.Stat(ctx, "/new.txt")
	if err != nil {
		t.Fatalf("Stat failed: %v", err)
	}
	if info.Size != int64(len(data)) {
		t.Fatalf("got size %d, want %d", info.Size, len(data))
	}
}

func TestVFS_WriteUpdatesAvailableSpace(t *testing.T) {
	v := newTestVFS(t)
	ctx := context.Background()

	// Record available space before
	var beforeTotal int64
	for _, b := range v.backends {
		a, _ := b.Available(ctx)
		beforeTotal += a
	}

	data := make([]byte, 128)
	v.Write(ctx, "/space.bin", bytes.NewReader(data), int64(len(data)))

	var afterTotal int64
	for _, b := range v.backends {
		a, _ := b.Available(ctx)
		afterTotal += a
	}

	if afterTotal >= beforeTotal {
		t.Fatal("available space should have decreased after write")
	}
}

func TestVFS_Overwrite(t *testing.T) {
	v := newTestVFS(t)
	ctx := context.Background()

	data1 := []byte("original content that is long enough for segments!!")
	v.Write(ctx, "/overwrite.txt", bytes.NewReader(data1), int64(len(data1)))

	data2 := []byte("new content, completely different data for overwrite!")
	v.Write(ctx, "/overwrite.txt", bytes.NewReader(data2), int64(len(data2)))

	got, err := v.Read(ctx, "/overwrite.txt", 0, int64(len(data2)))
	if err != nil {
		t.Fatalf("Read after overwrite failed: %v", err)
	}
	if !bytes.Equal(got, data2) {
		t.Fatal("overwrite data mismatch")
	}
}

func TestVFS_Delete(t *testing.T) {
	v := newTestVFS(t)
	ctx := context.Background()

	data := []byte("delete me! this content should not persist after removal")
	v.Write(ctx, "/del.txt", bytes.NewReader(data), int64(len(data)))

	if err := v.Delete(ctx, "/del.txt"); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	_, err := v.Stat(ctx, "/del.txt")
	if err == nil {
		t.Fatal("expected error after delete")
	}
}

func TestVFS_Mkdir(t *testing.T) {
	v := newTestVFS(t)
	ctx := context.Background()

	if err := v.Mkdir(ctx, "/mydir"); err != nil {
		t.Fatalf("Mkdir failed: %v", err)
	}

	entries, err := v.ReadDir(ctx, "/")
	if err != nil {
		t.Fatalf("ReadDir failed: %v", err)
	}

	found := false
	for _, e := range entries {
		if e.Path == "/mydir/" {
			found = true
		}
	}
	if !found {
		t.Fatal("directory not found in listing")
	}
}

func TestVFS_Rename(t *testing.T) {
	v := newTestVFS(t)
	ctx := context.Background()

	data := []byte("rename test content that needs to be long enough!!!!")
	v.Write(ctx, "/oldname.txt", bytes.NewReader(data), int64(len(data)))

	if err := v.Rename(ctx, "/oldname.txt", "/newname.txt"); err != nil {
		t.Fatalf("Rename failed: %v", err)
	}

	_, err := v.Stat(ctx, "/oldname.txt")
	if err == nil {
		t.Fatal("old path should not exist")
	}

	got, err := v.Read(ctx, "/newname.txt", 0, int64(len(data)))
	if err != nil {
		t.Fatalf("Read after rename failed: %v", err)
	}
	if !bytes.Equal(got, data) {
		t.Fatal("renamed file data mismatch")
	}
}

func TestVFS_WriteGeneratesOps(t *testing.T) {
	v := newTestVFS(t)
	ctx := context.Background()

	data := []byte("ops test content here with sufficient length for test!")
	v.Write(ctx, "/ops.txt", bytes.NewReader(data), int64(len(data)))

	ops, _ := v.opLog.ReadAll()
	found := false
	for _, op := range ops {
		if op.Path == "/ops.txt" && op.Type == metadata.OpFileCreate {
			found = true
		}
	}
	if !found {
		t.Fatal("expected OpFileCreate in ops log")
	}
}

// Phase 5.4 tests

func TestVFS_WriteDataShardsOnHotProviders(t *testing.T) {
	hotProviders := makeTestProviders(3, provider.TierHot)
	coldProviders := makeTestProviders(2, provider.TierCold)
	// Rename cold providers to avoid ID collisions
	for i := range coldProviders {
		coldProviders[i].ID = string(rune('X' + i))
	}
	all := append(hotProviders, coldProviders...)

	v := newTestVFSWithProviders(t, all)
	ctx := context.Background()

	data := make([]byte, 128)
	for i := range data {
		data[i] = byte(i)
	}
	v.Write(ctx, "/tiered.bin", bytes.NewReader(data), int64(len(data)))

	// Check shard placement
	shardMap, _ := v.store.GetShardMap("/tiered.bin")
	for _, shards := range shardMap {
		for shardIdx, pid := range shards {
			if shardIdx < 2 { // data shards (k=2)
				// Should be on hot providers
				isHot := false
				for _, hp := range hotProviders {
					if hp.ID == pid {
						isHot = true
					}
				}
				if !isHot {
					t.Fatalf("data shard %d on non-hot provider %s", shardIdx, pid)
				}
			}
		}
	}
}
