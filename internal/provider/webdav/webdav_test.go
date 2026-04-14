package webdav

import (
	"bytes"
	"context"
	"testing"

	"github.com/FloG309/cloud-storage-freeloader/internal/provider/memory"
)

func newTestBackend() *Backend {
	return NewWithStore(memory.New(10 * 1024 * 1024))
}

func TestWebDAV_PutAndGet(t *testing.T) {
	b := newTestBackend()
	ctx := context.Background()

	data := []byte("webdav data")
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

func TestWebDAV_Delete(t *testing.T) {
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

func TestWebDAV_List(t *testing.T) {
	b := newTestBackend()
	ctx := context.Background()

	b.Put(ctx, "dir/a.txt", []byte("a"))
	b.Put(ctx, "dir/b.txt", []byte("b"))
	b.Put(ctx, "other/c.txt", []byte("c"))

	keys, err := b.List(ctx, "dir/")
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(keys) != 2 {
		t.Fatalf("got %d keys, want 2", len(keys))
	}
}

func TestWebDAV_Exists(t *testing.T) {
	b := newTestBackend()
	ctx := context.Background()

	exists, _ := b.Exists(ctx, "nope")
	if exists {
		t.Fatal("expected false for non-existent key")
	}

	b.Put(ctx, "yes", []byte("data"))
	exists, _ = b.Exists(ctx, "yes")
	if !exists {
		t.Fatal("expected true for existing key")
	}
}
