package livesync

import (
	"context"
	"fmt"
	"testing"

	"github.com/FloG309/cloud-storage-freeloader/internal/provider"
	"github.com/FloG309/cloud-storage-freeloader/internal/provider/memory"
)

func newMemProviders(n int) []provider.StorageBackend {
	providers := make([]provider.StorageBackend, n)
	for i := range providers {
		providers[i] = memory.New(1 << 20)
	}
	return providers
}

func TestSyncer_PushBatch(t *testing.T) {
	providers := newMemProviders(3)
	s := NewSyncer("deviceA", providers)
	ctx := context.Background()

	batch := []FilePatches{{Seq: 1, DeviceID: "deviceA", Patches: []Patch{{Type: PatchInsert, Data: []byte("x")}}}}

	if err := s.Push(ctx, batch); err != nil {
		t.Fatalf("Push failed: %v", err)
	}

	for i, p := range providers {
		keys, _ := p.List(ctx, ".cloudfs/patches/")
		if len(keys) == 0 {
			t.Fatalf("provider %d has no batch files", i)
		}
	}
}

func TestSyncer_PullBatch(t *testing.T) {
	providers := newMemProviders(3)
	ctx := context.Background()

	// Push from device A
	sA := NewSyncer("deviceA", providers)
	sA.Push(ctx, []FilePatches{{Seq: 1, DeviceID: "deviceA", Patches: []Patch{{Type: PatchInsert, Data: []byte("x")}}}})

	// Pull as device B
	sB := NewSyncer("deviceB", providers)
	pulled, err := sB.Pull(ctx)
	if err != nil {
		t.Fatalf("Pull failed: %v", err)
	}
	if len(pulled) == 0 {
		t.Fatal("expected pulled batches")
	}
}

func TestSyncer_PullOnlyNew(t *testing.T) {
	providers := newMemProviders(3)
	ctx := context.Background()

	sA := NewSyncer("deviceA", providers)
	sA.Push(ctx, []FilePatches{{Seq: 1, DeviceID: "deviceA"}})

	sB := NewSyncer("deviceB", providers)
	pull1, _ := sB.Pull(ctx)
	pull2, _ := sB.Pull(ctx)

	if len(pull2) != 0 {
		t.Fatalf("second pull should return nothing, got %d", len(pull2))
	}
	_ = pull1
}

func TestSyncer_ProviderDown(t *testing.T) {
	providers := newMemProviders(3)
	providers[0] = &failProvider{} // first provider is down
	ctx := context.Background()

	s := NewSyncer("deviceA", providers)
	err := s.Push(ctx, []FilePatches{{Seq: 1, DeviceID: "deviceA"}})
	if err != nil {
		t.Fatalf("Push should succeed even with one down: %v", err)
	}

	// Other providers should have the batch
	keys, _ := providers[1].List(ctx, ".cloudfs/patches/")
	if len(keys) == 0 {
		t.Fatal("provider 1 should have batch")
	}
}

func TestSyncer_TwoDevicesConverge(t *testing.T) {
	providers := newMemProviders(3)
	ctx := context.Background()

	sA := NewSyncer("deviceA", providers)
	sB := NewSyncer("deviceB", providers)

	sA.Push(ctx, []FilePatches{{Seq: 1, DeviceID: "deviceA", FileHash: "fileX"}})
	sB.Push(ctx, []FilePatches{{Seq: 1, DeviceID: "deviceB", FileHash: "fileY"}})

	pullA, _ := sA.Pull(ctx)
	pullB, _ := sB.Pull(ctx)

	if len(pullA) == 0 {
		t.Fatal("device A should see device B's patches")
	}
	if len(pullB) == 0 {
		t.Fatal("device B should see device A's patches")
	}
}

type failProvider struct{}

func (f *failProvider) Put(_ context.Context, _ string, _ []byte) error       { return errFail }
func (f *failProvider) Get(_ context.Context, _ string) ([]byte, error)        { return nil, errFail }
func (f *failProvider) Delete(_ context.Context, _ string) error               { return errFail }
func (f *failProvider) Exists(_ context.Context, _ string) (bool, error)       { return false, errFail }
func (f *failProvider) List(_ context.Context, _ string) ([]string, error)     { return nil, errFail }
func (f *failProvider) Available(_ context.Context) (int64, error)             { return 0, errFail }
func (f *failProvider) Close() error                                           { return nil }

var errFail = fmt.Errorf("provider down")
