package memory

import (
	"bytes"
	"context"
	"testing"
)

func TestMemory_PutAndGet(t *testing.T) {
	b := New(1024)
	ctx := context.Background()

	data := []byte("hello world")
	if err := b.Put(ctx, "key1", data); err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	got, err := b.Get(ctx, "key1")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if !bytes.Equal(got, data) {
		t.Fatalf("got %q, want %q", got, data)
	}
}

func TestMemory_GetNotFound(t *testing.T) {
	b := New(1024)
	_, err := b.Get(context.Background(), "nonexistent")
	if err == nil {
		t.Fatal("expected error for non-existent key")
	}
}

func TestMemory_Delete(t *testing.T) {
	b := New(1024)
	ctx := context.Background()

	b.Put(ctx, "key1", []byte("data"))
	if err := b.Delete(ctx, "key1"); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}
	exists, _ := b.Exists(ctx, "key1")
	if exists {
		t.Fatal("key should not exist after delete")
	}
}

func TestMemory_DeleteNotFound(t *testing.T) {
	b := New(1024)
	err := b.Delete(context.Background(), "nonexistent")
	if err == nil {
		t.Fatal("expected error deleting non-existent key")
	}
}

func TestMemory_List(t *testing.T) {
	b := New(4096)
	ctx := context.Background()

	b.Put(ctx, "docs/a.txt", []byte("a"))
	b.Put(ctx, "docs/b.txt", []byte("b"))
	b.Put(ctx, "docs/c.txt", []byte("c"))
	b.Put(ctx, "other/d.txt", []byte("d"))

	keys, err := b.List(ctx, "docs/")
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(keys) != 3 {
		t.Fatalf("got %d keys, want 3", len(keys))
	}
}

func TestMemory_ListEmpty(t *testing.T) {
	b := New(1024)
	keys, err := b.List(context.Background(), "anything")
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(keys) != 0 {
		t.Fatalf("got %d keys, want 0", len(keys))
	}
}

func TestMemory_Available(t *testing.T) {
	b := New(1024)
	avail, err := b.Available(context.Background())
	if err != nil {
		t.Fatalf("Available failed: %v", err)
	}
	if avail != 1024 {
		t.Fatalf("got %d, want 1024", avail)
	}
}

func TestMemory_AvailableAfterPut(t *testing.T) {
	b := New(1024)
	ctx := context.Background()

	data := []byte("hello") // 5 bytes
	b.Put(ctx, "k", data)

	avail, _ := b.Available(ctx)
	if avail != 1024-5 {
		t.Fatalf("got %d, want %d", avail, 1024-5)
	}
}

func TestMemory_PutExceedsCapacity(t *testing.T) {
	b := New(10)
	err := b.Put(context.Background(), "big", make([]byte, 20))
	if err == nil {
		t.Fatal("expected error when exceeding capacity")
	}
}
