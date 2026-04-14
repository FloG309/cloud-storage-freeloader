//go:build integration

package fuse

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/FloG309/cloud-storage-freeloader/internal/erasure"
	"github.com/FloG309/cloud-storage-freeloader/internal/metadata"
	"github.com/FloG309/cloud-storage-freeloader/internal/placement"
	"github.com/FloG309/cloud-storage-freeloader/internal/provider"
	"github.com/FloG309/cloud-storage-freeloader/internal/provider/memory"
	"github.com/FloG309/cloud-storage-freeloader/internal/vfs"
)

func newTestMounter(t *testing.T) (*Mounter, string) {
	t.Helper()

	providers := make([]placement.ProviderInfo, 4)
	backends := make(map[string]provider.StorageBackend)
	for i := 0; i < 4; i++ {
		id := string(rune('A' + i))
		providers[i] = placement.ProviderInfo{
			ID:        id,
			Profile:   provider.ProviderProfile{},
			Tracker:   provider.NewBandwidthTracker(0, 0),
			Available: 100 * 1024 * 1024,
		}
		backends[id] = memory.New(100 * 1024 * 1024)
	}

	store, _ := metadata.NewStore(":memory:")
	opLog, _ := metadata.NewOpLog(store.DB())
	t.Cleanup(func() { store.Close() })

	enc, _ := erasure.NewEncoder(2, 2)
	chunker := erasure.NewChunker(1024 * 1024)
	engine := placement.NewEngine(providers)
	cache := vfs.NewSegmentCache(10 * 1024 * 1024)

	v := vfs.NewVFS(store, opLog, engine, enc, chunker, cache, backends)

	mountPoint := t.TempDir()
	mounter := NewMounter(v, mountPoint)

	return mounter, mountPoint
}

func TestFUSE_MountAndUnmount(t *testing.T) {
	m, mountPoint := newTestMounter(t)

	if err := m.Mount(); err != nil {
		t.Fatalf("Mount failed: %v", err)
	}
	defer m.Unmount()

	info, err := os.Stat(mountPoint)
	if err != nil {
		t.Fatalf("mount point not accessible: %v", err)
	}
	if !info.IsDir() {
		t.Fatal("mount point is not a directory")
	}
}

func TestFUSE_WriteAndReadViaOS(t *testing.T) {
	m, mountPoint := newTestMounter(t)
	m.Mount()
	defer m.Unmount()

	testFile := filepath.Join(mountPoint, "hello.txt")
	data := []byte("Hello from FUSE!")

	if err := os.WriteFile(testFile, data, 0644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	got, err := os.ReadFile(testFile)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}
	if string(got) != string(data) {
		t.Fatalf("got %q, want %q", got, data)
	}
}

func TestFUSE_ListDirViaOS(t *testing.T) {
	m, mountPoint := newTestMounter(t)
	m.Mount()
	defer m.Unmount()

	for _, name := range []string{"a.txt", "b.txt", "c.txt"} {
		os.WriteFile(filepath.Join(mountPoint, name), []byte("content"), 0644)
	}

	entries, err := os.ReadDir(mountPoint)
	if err != nil {
		t.Fatalf("ReadDir failed: %v", err)
	}
	if len(entries) != 3 {
		t.Fatalf("got %d entries, want 3", len(entries))
	}
}

func TestFUSE_DeleteViaOS(t *testing.T) {
	m, mountPoint := newTestMounter(t)
	m.Mount()
	defer m.Unmount()

	testFile := filepath.Join(mountPoint, "delete_me.txt")
	os.WriteFile(testFile, []byte("gone soon"), 0644)

	if err := os.Remove(testFile); err != nil {
		t.Fatalf("Remove failed: %v", err)
	}

	if _, err := os.Stat(testFile); !os.IsNotExist(err) {
		t.Fatal("file should not exist after delete")
	}
}

func TestFUSE_LargeFileViaOS(t *testing.T) {
	m, mountPoint := newTestMounter(t)
	m.Mount()
	defer m.Unmount()

	data := make([]byte, 50*1024*1024)
	for i := range data {
		data[i] = byte(i % 256)
	}

	testFile := filepath.Join(mountPoint, "large.bin")
	if err := os.WriteFile(testFile, data, 0644); err != nil {
		t.Fatalf("WriteFile large failed: %v", err)
	}

	got, err := os.ReadFile(testFile)
	if err != nil {
		t.Fatalf("ReadFile large failed: %v", err)
	}
	if len(got) != len(data) {
		t.Fatalf("got %d bytes, want %d", len(got), len(data))
	}
}

func TestFUSE_StatViaOS(t *testing.T) {
	m, mountPoint := newTestMounter(t)
	m.Mount()
	defer m.Unmount()

	data := []byte("stat test content")
	testFile := filepath.Join(mountPoint, "stat.txt")
	os.WriteFile(testFile, data, 0644)

	info, err := os.Stat(testFile)
	if err != nil {
		t.Fatalf("Stat failed: %v", err)
	}
	if info.Size() != int64(len(data)) {
		t.Fatalf("got size %d, want %d", info.Size(), len(data))
	}
}
