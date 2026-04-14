package metadata

import (
	"context"
	"fmt"
	"testing"

	"github.com/FloG309/cloud-storage-freeloader/internal/provider"
	"github.com/FloG309/cloud-storage-freeloader/internal/provider/memory"
)

func newTestSyncEngine(t *testing.T) (*SyncEngine, *Store, *OpLog) {
	t.Helper()
	s := newTestStore(t)
	ol, _ := NewOpLog(s.db)

	providers := []provider.StorageBackend{
		memory.New(1024 * 1024),
		memory.New(1024 * 1024),
		memory.New(1024 * 1024),
	}

	se := NewSyncEngine(ol, s, providers, "device1")
	return se, s, ol
}

func TestSync_PushOpsToProviders(t *testing.T) {
	se, _, ol := newTestSyncEngine(t)
	ctx := context.Background()

	ol.Append("device1", OpFileCreate, "/a.txt", nil)
	ol.Append("device1", OpFileCreate, "/b.txt", nil)

	if err := se.Push(ctx); err != nil {
		t.Fatalf("Push failed: %v", err)
	}

	// Check that ops file exists on each provider
	for i, p := range se.providers {
		keys, _ := p.List(ctx, ".cloudfs/ops/")
		if len(keys) == 0 {
			t.Fatalf("provider %d has no ops files", i)
		}
	}
}

func TestSync_PullOpsFromProviders(t *testing.T) {
	se, _, ol := newTestSyncEngine(t)
	ctx := context.Background()

	// Simulate another device's ops on providers
	otherOps := `{"ops":[{"op_id":"uuid1","device_id":"device2","seq_num":1,"type":"file_create","path":"/remote.txt"}]}`
	for _, p := range se.providers {
		p.Put(ctx, ".cloudfs/ops/device2/1.json", []byte(otherOps))
	}

	if err := se.Pull(ctx); err != nil {
		t.Fatalf("Pull failed: %v", err)
	}

	ops, _ := ol.ReadAll()
	found := false
	for _, op := range ops {
		if op.Path == "/remote.txt" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected pulled op /remote.txt in local log")
	}
}

func TestSync_MergeNonConflicting(t *testing.T) {
	se1, s1, ol1 := newTestSyncEngine(t)
	ctx := context.Background()

	// Device 1 creates file X
	s1.CreateFile(&FileMeta{Path: "/x.txt", Size: 10})
	ol1.Append("device1", OpFileCreate, "/x.txt", nil)
	se1.Push(ctx)

	// Device 2 creates file Y on same providers
	se2, s2, ol2 := newTestSyncEngineWith(t, se1.providers, "device2")
	s2.CreateFile(&FileMeta{Path: "/y.txt", Size: 20})
	ol2.Append("device2", OpFileCreate, "/y.txt", nil)
	se2.Push(ctx)

	// Both pull
	se1.Pull(ctx)
	se2.Pull(ctx)

	// Both should see both ops
	ops1, _ := ol1.ReadAll()
	ops2, _ := ol2.ReadAll()

	if len(ops1) < 2 {
		t.Fatalf("device1 has %d ops, want at least 2", len(ops1))
	}
	if len(ops2) < 2 {
		t.Fatalf("device2 has %d ops, want at least 2", len(ops2))
	}
}

func TestSync_HighWaterMark(t *testing.T) {
	se, _, ol := newTestSyncEngine(t)
	ctx := context.Background()

	ol.Append("device1", OpFileCreate, "/a.txt", nil)
	se.Push(ctx)

	// First pull: should get ops
	se2, _, ol2 := newTestSyncEngineWith(t, se.providers, "device2")
	se2.Pull(ctx)
	ops1, _ := ol2.ReadAll()

	// Second pull: no new ops
	se2.Pull(ctx)
	ops2, _ := ol2.ReadAll()

	if len(ops2) != len(ops1) {
		t.Fatalf("second pull added ops: %d vs %d", len(ops2), len(ops1))
	}
}

func TestSync_ProviderDown(t *testing.T) {
	se, _, ol := newTestSyncEngine(t)
	ctx := context.Background()

	ol.Append("device1", OpFileCreate, "/a.txt", nil)

	// Replace one provider with a failing one
	se.providers[0] = &failingBackend{}

	err := se.Push(ctx)
	// Should not fail — best effort
	if err != nil {
		t.Fatalf("Push should succeed even if one provider is down: %v", err)
	}

	// Other providers should have the data
	for i := 1; i < len(se.providers); i++ {
		keys, _ := se.providers[i].List(ctx, ".cloudfs/ops/")
		if len(keys) == 0 {
			t.Fatalf("provider %d should have ops", i)
		}
	}
}

func TestSync_SnapshotCreateAndRestore(t *testing.T) {
	se, s, _ := newTestSyncEngine(t)
	ctx := context.Background()

	s.CreateFile(&FileMeta{Path: "/snap.txt", Size: 100})

	if err := se.Snapshot(ctx); err != nil {
		t.Fatalf("Snapshot failed: %v", err)
	}

	// Verify snapshot exists on providers
	for i, p := range se.providers {
		keys, _ := p.List(ctx, ".cloudfs/snapshot/")
		if len(keys) == 0 {
			t.Fatalf("provider %d has no snapshot", i)
		}
	}

	// Restore to a new store
	s2, _ := NewStore(":memory:")
	defer s2.Close()
	ol2, _ := NewOpLog(s2.db)
	se2 := NewSyncEngine(ol2, s2, se.providers, "device3")

	if err := se2.Restore(ctx); err != nil {
		t.Fatalf("Restore failed: %v", err)
	}

	f, err := s2.GetFile("/snap.txt")
	if err != nil {
		t.Fatalf("restored file not found: %v", err)
	}
	if f.Size != 100 {
		t.Fatalf("got size %d, want 100", f.Size)
	}
}

func newTestSyncEngineWith(t *testing.T, providers []provider.StorageBackend, deviceID string) (*SyncEngine, *Store, *OpLog) {
	t.Helper()
	s, _ := NewStore(":memory:")
	t.Cleanup(func() { s.Close() })
	ol, _ := NewOpLog(s.db)
	se := NewSyncEngine(ol, s, providers, deviceID)
	return se, s, ol
}

// failingBackend always returns errors.
type failingBackend struct{}

func (f *failingBackend) Put(_ context.Context, _ string, _ []byte) error       { return errDown }
func (f *failingBackend) Get(_ context.Context, _ string) ([]byte, error)        { return nil, errDown }
func (f *failingBackend) Delete(_ context.Context, _ string) error               { return errDown }
func (f *failingBackend) Exists(_ context.Context, _ string) (bool, error)       { return false, errDown }
func (f *failingBackend) List(_ context.Context, _ string) ([]string, error)     { return nil, errDown }
func (f *failingBackend) Available(_ context.Context) (int64, error)             { return 0, errDown }
func (f *failingBackend) Close() error                                           { return nil }

var errDown = fmt.Errorf("provider down")
