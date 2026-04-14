package livesync

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/FloG309/cloud-storage-freeloader/internal/provider"
	"github.com/FloG309/cloud-storage-freeloader/internal/provider/memory"
)

type testDevice struct {
	id       string
	patchLog *PatchLog
	syncer   *Syncer
	snapshot *SnapshotManager
}

func newTestDevice(id string, providers []provider.StorageBackend, snapProviders []snapshotProvider) *testDevice {
	pl, _ := NewPatchLog(":memory:")
	return &testDevice{
		id:       id,
		patchLog: pl,
		syncer:   NewSyncer(id, providers),
		snapshot: NewSnapshotManager(snapProviders, 50),
	}
}

func sharedSetup() ([]provider.StorageBackend, []snapshotProvider) {
	mem1 := memory.New(1 << 20)
	mem2 := memory.New(1 << 20)
	mem3 := memory.New(1 << 20)

	providers := []provider.StorageBackend{mem1, mem2, mem3}
	snapProviders := []snapshotProvider{
		{id: "p1", backend: mem1},
		{id: "p2", backend: mem2},
		{id: "p3", backend: mem3},
	}
	return providers, snapProviders
}

func TestLiveSync_EndToEnd(t *testing.T) {
	providers, snapProviders := sharedSetup()
	ctx := context.Background()

	devA := newTestDevice("deviceA", providers, snapProviders)
	devB := newTestDevice("deviceB", providers, snapProviders)
	defer devA.patchLog.Close()
	defer devB.patchLog.Close()

	// Device A writes "Hello" to notes.md
	fileContent := []byte("Hello")
	patches := Diff(nil, fileContent)
	devA.patchLog.Append("/notes.md", "deviceA", patches)

	// Batcher flushes, syncer pushes
	batch := []FilePatches{{
		FileHash: "hash1",
		Seq:      1,
		DeviceID: "deviceA",
		Time:     time.Now(),
		Patches:  patches,
	}}
	devA.syncer.Push(ctx, batch)

	// Device B pulls
	pulled, _ := devB.syncer.Pull(ctx)
	if len(pulled) == 0 {
		t.Fatal("device B should have pulled patches")
	}

	// Apply patch to reconstruct content
	result := ApplyPatches(nil, pulled[0].Patches)
	if !bytes.Equal(result, fileContent) {
		t.Fatalf("got %q, want %q", result, fileContent)
	}
}

func TestLiveSync_BidirectionalEditing(t *testing.T) {
	providers, snapProviders := sharedSetup()
	ctx := context.Background()

	devA := newTestDevice("deviceA", providers, snapProviders)
	devB := newTestDevice("deviceB", providers, snapProviders)
	defer devA.patchLog.Close()
	defer devB.patchLog.Close()

	// A writes notes.md
	patchesA := Diff(nil, []byte("Notes from A"))
	devA.syncer.Push(ctx, []FilePatches{{DeviceID: "deviceA", Seq: 1, FileHash: "notes", Patches: patchesA}})

	// B writes todo.md
	patchesB := Diff(nil, []byte("Todo from B"))
	devB.syncer.Push(ctx, []FilePatches{{DeviceID: "deviceB", Seq: 1, FileHash: "todo", Patches: patchesB}})

	// Both sync
	pullA, _ := devA.syncer.Pull(ctx)
	pullB, _ := devB.syncer.Pull(ctx)

	if len(pullA) == 0 {
		t.Fatal("A should see B's patches")
	}
	if len(pullB) == 0 {
		t.Fatal("B should see A's patches")
	}
}

func TestLiveSync_ConflictProducesConflictCopy(t *testing.T) {
	providers, snapProviders := sharedSetup()
	ctx := context.Background()

	devA := newTestDevice("deviceA", providers, snapProviders)
	devB := newTestDevice("deviceB", providers, snapProviders)
	defer devA.patchLog.Close()
	defer devB.patchLog.Close()

	base := []byte("Original content")

	// Both edit the same file without syncing
	patchesA := Diff(base, []byte("Content from device A"))
	patchesB := Diff(base, []byte("Content from device B"))

	devA.syncer.Push(ctx, []FilePatches{{DeviceID: "deviceA", Seq: 1, FileHash: "shared", Patches: patchesA}})
	devB.syncer.Push(ctx, []FilePatches{{DeviceID: "deviceB", Seq: 1, FileHash: "shared", Patches: patchesB}})

	// Both pull
	pullA, _ := devA.syncer.Pull(ctx)
	pullB, _ := devB.syncer.Pull(ctx)

	// A sees B's changes, try to merge
	contentA := ApplyPatches(base, patchesA)
	contentFromB := ApplyPatches(base, pullA[0].Patches)
	_, conflicts, _ := ThreeWayMerge(base, contentA, contentFromB)

	if len(conflicts) == 0 {
		t.Fatal("expected merge conflict")
	}
	_ = pullB
}

func TestLiveSync_SnapshotAndRecovery(t *testing.T) {
	providers, snapProviders := sharedSetup()
	ctx := context.Background()

	devA := newTestDevice("deviceA", providers, snapProviders)
	defer devA.patchLog.Close()

	// Device A makes many edits
	content := []byte("Initial")
	for i := 0; i < 60; i++ {
		newContent := append(content, []byte(" edit")...)
		patches := Diff(content, newContent)
		devA.patchLog.Append("/notes.md", "deviceA", patches)
		content = newContent
	}

	// Create snapshot (threshold exceeded at 50)
	devA.snapshot.CreateSnapshot(ctx, "/notes.md", content, 60)

	// Device C bootstraps
	devC := newTestDevice("deviceC", providers, snapProviders)
	defer devC.patchLog.Close()

	restored, err := devC.snapshot.RestoreSnapshot(ctx, "/notes.md")
	if err != nil {
		t.Fatalf("RestoreSnapshot: %v", err)
	}
	if !bytes.Equal(restored, content) {
		t.Fatal("restored content mismatch")
	}
}

func TestLiveSync_OfflineDevice(t *testing.T) {
	providers, snapProviders := sharedSetup()
	ctx := context.Background()

	devA := newTestDevice("deviceA", providers, snapProviders)
	defer devA.patchLog.Close()

	// A makes many edits over time, creating snapshots
	content := []byte("start")
	for i := 0; i < 200; i++ {
		newContent := append([]byte(nil), content...)
		newContent = append(newContent, '.')
		patches := Diff(content, newContent)
		devA.patchLog.Append("/long.md", "deviceA", patches)
		content = newContent

		if i == 99 || i == 199 {
			devA.snapshot.CreateSnapshot(ctx, "/long.md", content, int64(i+1))
		}
	}

	// Push final patches
	devA.syncer.Push(ctx, []FilePatches{{DeviceID: "deviceA", Seq: 200}})

	// Device B comes online and restores
	devB := newTestDevice("deviceB", providers, snapProviders)
	defer devB.patchLog.Close()

	restored, err := devB.snapshot.RestoreSnapshot(ctx, "/long.md")
	if err != nil {
		t.Fatalf("RestoreSnapshot: %v", err)
	}
	if !bytes.Equal(restored, content) {
		t.Fatal("offline device recovery failed")
	}
}
