package gdrive

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

func TestGDrive_PutAndGet(t *testing.T) {
	b := newTestBackend()
	ctx := context.Background()

	data := []byte("gdrive content")
	if err := b.Put(ctx, "doc.txt", data); err != nil {
		t.Fatalf("Put failed: %v", err)
	}
	got, err := b.Get(ctx, "doc.txt")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if !bytes.Equal(got, data) {
		t.Fatalf("got %q, want %q", got, data)
	}
}

func TestGDrive_Delete(t *testing.T) {
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

func TestGDrive_List(t *testing.T) {
	b := newTestBackend()
	ctx := context.Background()

	b.Put(ctx, "folder/a", []byte("a"))
	b.Put(ctx, "folder/b", []byte("b"))
	b.Put(ctx, "other/c", []byte("c"))

	keys, err := b.List(ctx, "folder/")
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(keys) != 2 {
		t.Fatalf("got %d keys, want 2", len(keys))
	}
}

func TestGDrive_Available(t *testing.T) {
	b := newTestBackend()
	avail, err := b.Available(context.Background())
	if err != nil {
		t.Fatalf("Available failed: %v", err)
	}
	if avail <= 0 {
		t.Fatalf("expected positive available space, got %d", avail)
	}
}

func TestGDrive_Profile(t *testing.T) {
	b := newTestBackend()
	profile := b.Profile()
	tier := profile.Classify()
	if tier != provider.TierHot {
		t.Fatalf("got tier %v, want hot", tier)
	}
}
