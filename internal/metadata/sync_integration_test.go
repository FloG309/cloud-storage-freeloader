package metadata

import (
	"bytes"
	"context"
	"testing"

	"github.com/FloG309/cloud-storage-freeloader/internal/provider"
	"github.com/FloG309/cloud-storage-freeloader/internal/provider/memory"
)

func newSharedProviders() []provider.StorageBackend {
	return []provider.StorageBackend{
		memory.New(10 * 1024 * 1024),
		memory.New(10 * 1024 * 1024),
		memory.New(10 * 1024 * 1024),
	}
}

func TestSync_TwoDevicesConverge(t *testing.T) {
	shared := newSharedProviders()
	ctx := context.Background()

	// Device A
	storeA, _ := NewStore(":memory:")
	defer storeA.Close()
	olA, _ := NewOpLog(storeA.db)
	seA := NewSyncEngine(olA, storeA, shared, "deviceA")

	storeA.CreateFile(&FileMeta{Path: "/fileX.txt", Size: 100})
	olA.Append("deviceA", OpFileCreate, "/fileX.txt", nil)
	seA.Push(ctx)

	// Device B
	storeB, _ := NewStore(":memory:")
	defer storeB.Close()
	olB, _ := NewOpLog(storeB.db)
	seB := NewSyncEngine(olB, storeB, shared, "deviceB")

	storeB.CreateFile(&FileMeta{Path: "/fileY.txt", Size: 200})
	olB.Append("deviceB", OpFileCreate, "/fileY.txt", nil)
	seB.Push(ctx)

	// Both sync
	seA.Pull(ctx)
	seB.Pull(ctx)

	// Both should see both files in ops
	opsA, _ := olA.ReadAll()
	opsB, _ := olB.ReadAll()

	hasX, hasY := false, false
	for _, op := range opsA {
		if op.Path == "/fileX.txt" {
			hasX = true
		}
		if op.Path == "/fileY.txt" {
			hasY = true
		}
	}
	if !hasX || !hasY {
		t.Fatalf("device A missing ops: X=%v Y=%v (has %d ops)", hasX, hasY, len(opsA))
	}

	hasX, hasY = false, false
	for _, op := range opsB {
		if op.Path == "/fileX.txt" {
			hasX = true
		}
		if op.Path == "/fileY.txt" {
			hasY = true
		}
	}
	if !hasX || !hasY {
		t.Fatalf("device B missing ops: X=%v Y=%v (has %d ops)", hasX, hasY, len(opsB))
	}
}

func TestSync_ConflictResolution_Integration(t *testing.T) {
	shared := newSharedProviders()
	ctx := context.Background()

	storeA, _ := NewStore(":memory:")
	defer storeA.Close()
	olA, _ := NewOpLog(storeA.db)
	seA := NewSyncEngine(olA, storeA, shared, "deviceA")

	storeB, _ := NewStore(":memory:")
	defer storeB.Close()
	olB, _ := NewOpLog(storeB.db)
	seB := NewSyncEngine(olB, storeB, shared, "deviceB")

	// Both create same path
	storeA.CreateFile(&FileMeta{Path: "/conflict.txt", Size: 100})
	olA.Append("deviceA", OpFileUpdate, "/conflict.txt", nil)
	seA.Push(ctx)

	storeB.CreateFile(&FileMeta{Path: "/conflict.txt", Size: 200})
	olB.Append("deviceB", OpFileUpdate, "/conflict.txt", nil)
	seB.Push(ctx)

	// Both pull
	seA.Pull(ctx)
	seB.Pull(ctx)

	// Detect conflicts from the pulled ops
	opsFromA, _ := olA.ReadSince("deviceA", 0)
	opsFromB_onA, _ := olA.ReadSince("deviceB", 0)

	conflicts := DetectConflicts(opsFromA, opsFromB_onA)
	if len(conflicts) == 0 {
		t.Fatal("expected at least one conflict")
	}

	resolutions := ResolveConflicts(conflicts, storeA)
	if len(resolutions) == 0 {
		t.Fatal("expected at least one resolution")
	}
	if resolutions[0].ConflictCopy == nil {
		t.Fatal("expected conflict copy")
	}
}

func TestSync_NewDeviceBootstrap(t *testing.T) {
	shared := newSharedProviders()
	ctx := context.Background()

	// Device A creates files and snapshots
	storeA, _ := NewStore(":memory:")
	defer storeA.Close()
	olA, _ := NewOpLog(storeA.db)
	seA := NewSyncEngine(olA, storeA, shared, "deviceA")

	storeA.CreateFile(&FileMeta{Path: "/existing1.txt", Size: 100})
	storeA.CreateFile(&FileMeta{Path: "/existing2.txt", Size: 200})
	seA.Snapshot(ctx)

	// Device C starts fresh and restores
	storeC, _ := NewStore(":memory:")
	defer storeC.Close()
	olC, _ := NewOpLog(storeC.db)
	seC := NewSyncEngine(olC, storeC, shared, "deviceC")

	if err := seC.Restore(ctx); err != nil {
		t.Fatalf("Restore failed: %v", err)
	}

	f1, err := storeC.GetFile("/existing1.txt")
	if err != nil {
		t.Fatalf("file1 not found: %v", err)
	}
	if f1.Size != 100 {
		t.Fatalf("file1 size %d, want 100", f1.Size)
	}

	f2, err := storeC.GetFile("/existing2.txt")
	if err != nil {
		t.Fatalf("file2 not found: %v", err)
	}
	if f2.Size != 200 {
		t.Fatalf("file2 size %d, want 200", f2.Size)
	}
}

// Suppress unused import warning
var _ = bytes.Equal
