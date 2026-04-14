package onedrive

import (
	"bytes"
	"context"
	"testing"

	"github.com/FloG309/cloud-storage-freeloader/internal/provider"
	"github.com/FloG309/cloud-storage-freeloader/internal/provider/memory"
)

func newTestBackend() *Backend {
	return NewWithStore(memory.New(10 * 1024 * 1024))
}

func TestOneDrive_PutAndGet(t *testing.T) {
	b := newTestBackend()
	ctx := context.Background()

	data := []byte("onedrive content")
	if err := b.Put(ctx, "file.txt", data); err != nil {
		t.Fatalf("Put failed: %v", err)
	}
	got, err := b.Get(ctx, "file.txt")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if !bytes.Equal(got, data) {
		t.Fatalf("got %q, want %q", got, data)
	}
}

func TestOneDrive_PutLargeFile(t *testing.T) {
	b := newTestBackend()
	ctx := context.Background()

	// >4MB triggers upload session in production
	data := make([]byte, 5*1024*1024)
	for i := range data {
		data[i] = byte(i % 256)
	}

	if err := b.Put(ctx, "large.bin", data); err != nil {
		t.Fatalf("Put large failed: %v", err)
	}
	got, err := b.Get(ctx, "large.bin")
	if err != nil {
		t.Fatalf("Get large failed: %v", err)
	}
	if !bytes.Equal(got, data) {
		t.Fatal("large file data mismatch")
	}
}

func TestOneDrive_Delete(t *testing.T) {
	b := newTestBackend()
	ctx := context.Background()

	b.Put(ctx, "del.txt", []byte("data"))
	if err := b.Delete(ctx, "del.txt"); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}
	exists, _ := b.Exists(ctx, "del.txt")
	if exists {
		t.Fatal("key should not exist after delete")
	}
}

func TestOneDrive_List(t *testing.T) {
	b := newTestBackend()
	ctx := context.Background()

	b.Put(ctx, "dir/a", []byte("a"))
	b.Put(ctx, "dir/b", []byte("b"))
	b.Put(ctx, "other/c", []byte("c"))

	keys, err := b.List(ctx, "dir/")
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(keys) != 2 {
		t.Fatalf("got %d keys, want 2", len(keys))
	}
}

func TestOneDrive_Available(t *testing.T) {
	b := newTestBackend()
	avail, err := b.Available(context.Background())
	if err != nil {
		t.Fatalf("Available failed: %v", err)
	}
	if avail <= 0 {
		t.Fatalf("expected positive available space, got %d", avail)
	}
}

func TestOneDrive_Profile(t *testing.T) {
	b := newTestBackend()
	profile := b.Profile()
	tier := profile.Classify()
	// OneDrive with egress limits → warm
	if tier != provider.TierWarm {
		t.Fatalf("got tier %v, want warm", tier)
	}
}
