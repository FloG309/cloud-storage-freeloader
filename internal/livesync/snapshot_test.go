package livesync

import (
	"bytes"
	"context"
	"testing"

	"github.com/FloG309/cloud-storage-freeloader/internal/provider/memory"
)

func TestSnapshot_Create(t *testing.T) {
	providers := []snapshotProvider{
		{id: "p1", backend: memory.New(1 << 20)},
		{id: "p2", backend: memory.New(1 << 20)},
	}
	sm := NewSnapshotManager(providers, 50)

	content := []byte("snapshot content data")
	err := sm.CreateSnapshot(context.Background(), "/notes.md", content, 1)
	if err != nil {
		t.Fatalf("CreateSnapshot failed: %v", err)
	}
}

func TestSnapshot_Restore(t *testing.T) {
	providers := []snapshotProvider{
		{id: "p1", backend: memory.New(1 << 20)},
	}
	sm := NewSnapshotManager(providers, 50)
	ctx := context.Background()

	content := []byte("restore me from snapshot")
	sm.CreateSnapshot(ctx, "/notes.md", content, 1)

	restored, err := sm.RestoreSnapshot(ctx, "/notes.md")
	if err != nil {
		t.Fatalf("RestoreSnapshot failed: %v", err)
	}
	if !bytes.Equal(restored, content) {
		t.Fatalf("got %q, want %q", restored, content)
	}
}

func TestSnapshot_TriggeredByPatchCount(t *testing.T) {
	providers := []snapshotProvider{
		{id: "p1", backend: memory.New(1 << 20)},
	}
	sm := NewSnapshotManager(providers, 5) // threshold = 5

	if !sm.ShouldSnapshot(5) {
		t.Fatal("expected snapshot at threshold 5")
	}
	if sm.ShouldSnapshot(4) {
		t.Fatal("should not snapshot below threshold")
	}
}

func TestSnapshot_RecoveryFlow(t *testing.T) {
	providers := []snapshotProvider{
		{id: "p1", backend: memory.New(1 << 20)},
	}
	sm := NewSnapshotManager(providers, 50)
	ctx := context.Background()

	// Create snapshot at "seq 100"
	content := []byte("base content for recovery")
	sm.CreateSnapshot(ctx, "/recovery.md", content, 100)

	// Apply patches 101-105
	patches := []Patch{
		{Type: PatchInsert, Offset: len(content), Data: []byte(" + more")},
	}
	restored, _ := sm.RestoreSnapshot(ctx, "/recovery.md")
	result := ApplyPatches(restored, patches)

	expected := append([]byte(nil), content...)
	expected = append(expected, []byte(" + more")...)
	if !bytes.Equal(result, expected) {
		t.Fatalf("recovery flow failed: got %q", result)
	}
}

func TestSnapshot_GarbageCollectsOldPatches(t *testing.T) {
	// Verify the manager reports the right snapshot seq for GC
	providers := []snapshotProvider{
		{id: "p1", backend: memory.New(1 << 20)},
	}
	sm := NewSnapshotManager(providers, 50)
	ctx := context.Background()

	sm.CreateSnapshot(ctx, "/gc.md", []byte("data"), 100)

	lastSeq := sm.LastSnapshotSeq("/gc.md")
	if lastSeq != 100 {
		t.Fatalf("got last seq %d, want 100", lastSeq)
	}
}
