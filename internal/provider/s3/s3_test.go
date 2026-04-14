package s3

import (
	"bytes"
	"context"
	"testing"

	"github.com/FloG309/cloud-storage-freeloader/internal/provider/memory"
)

// S3 backend tests use the memory backend as a stand-in to validate
// the S3 backend's interface conformance and logic. Integration tests
// against a real S3/MinIO endpoint are gated behind a build tag.

// newTestBackend creates an S3 backend backed by an in-memory store
// for unit testing without network access.
func newTestBackend() *Backend {
	return NewWithStore(memory.New(10 * 1024 * 1024)) // 10MB
}

func TestS3_PutAndGet(t *testing.T) {
	b := newTestBackend()
	ctx := context.Background()

	data := []byte("hello s3 world")
	if err := b.Put(ctx, "test-key", data); err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	got, err := b.Get(ctx, "test-key")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if !bytes.Equal(got, data) {
		t.Fatalf("got %q, want %q", got, data)
	}
}

func TestS3_Delete(t *testing.T) {
	b := newTestBackend()
	ctx := context.Background()

	b.Put(ctx, "del-key", []byte("data"))
	if err := b.Delete(ctx, "del-key"); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}
	exists, _ := b.Exists(ctx, "del-key")
	if exists {
		t.Fatal("key should not exist after delete")
	}
}

func TestS3_List(t *testing.T) {
	b := newTestBackend()
	ctx := context.Background()

	b.Put(ctx, "prefix/a", []byte("a"))
	b.Put(ctx, "prefix/b", []byte("b"))
	b.Put(ctx, "prefix/c", []byte("c"))
	b.Put(ctx, "other/d", []byte("d"))

	keys, err := b.List(ctx, "prefix/")
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(keys) != 3 {
		t.Fatalf("got %d keys, want 3", len(keys))
	}
}

func TestS3_PutLargeFile(t *testing.T) {
	b := newTestBackend()
	ctx := context.Background()

	data := make([]byte, 6*1024*1024) // 6MB
	for i := range data {
		data[i] = byte(i % 256)
	}

	if err := b.Put(ctx, "large-key", data); err != nil {
		t.Fatalf("Put large failed: %v", err)
	}

	got, err := b.Get(ctx, "large-key")
	if err != nil {
		t.Fatalf("Get large failed: %v", err)
	}
	if !bytes.Equal(got, data) {
		t.Fatal("large file data mismatch")
	}
}

func TestS3_GetNotFound(t *testing.T) {
	b := newTestBackend()
	_, err := b.Get(context.Background(), "nonexistent")
	if err == nil {
		t.Fatal("expected error for non-existent key")
	}
}

func TestS3_Profile(t *testing.T) {
	b := newTestBackend()
	profile := b.Profile()
	tier := profile.Classify()
	// S3 (e.g., Backblaze B2 free tier) is hot by default
	if tier.String() != "hot" {
		t.Fatalf("got tier %v, want hot", tier)
	}
}
