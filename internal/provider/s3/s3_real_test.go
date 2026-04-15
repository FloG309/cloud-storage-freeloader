package s3

import (
	"bytes"
	"context"
	"testing"
)

func TestS3Real_NewFromConfig(t *testing.T) {
	cfg := map[string]string{
		"endpoint":        "http://localhost:9999",
		"region":          "us-east-1",
		"bucket":          "test-bucket",
		"key_id":          "test-key",
		"application_key": "test-secret",
	}

	b, err := NewFromConfig(cfg)
	if err != nil {
		t.Fatalf("NewFromConfig failed: %v", err)
	}
	if b.bucket != "test-bucket" {
		t.Fatalf("bucket = %s, want test-bucket", b.bucket)
	}
}

func TestS3Real_NewFromConfig_MissingBucket(t *testing.T) {
	cfg := map[string]string{
		"endpoint":        "http://localhost:9999",
		"region":          "us-east-1",
		"key_id":          "test-key",
		"application_key": "test-secret",
	}
	_, err := NewFromConfig(cfg)
	if err == nil {
		t.Fatal("expected error for missing bucket")
	}
}

func TestS3Real_PutAndGet(t *testing.T) {
	srv, b := newMockS3Server(t)
	defer srv.Close()
	ctx := context.Background()

	data := []byte("hello s3 real")
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

func TestS3Real_Delete(t *testing.T) {
	srv, b := newMockS3Server(t)
	defer srv.Close()
	ctx := context.Background()

	b.Put(ctx, "del-key", []byte("data"))
	if err := b.Delete(ctx, "del-key"); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}
	exists, _ := b.Exists(ctx, "del-key")
	if exists {
		t.Fatal("should not exist after delete")
	}
}

func TestS3Real_Exists(t *testing.T) {
	srv, b := newMockS3Server(t)
	defer srv.Close()
	ctx := context.Background()

	exists, _ := b.Exists(ctx, "nope")
	if exists {
		t.Fatal("should not exist")
	}

	b.Put(ctx, "yes-key", []byte("data"))
	exists, _ = b.Exists(ctx, "yes-key")
	if !exists {
		t.Fatal("should exist")
	}
}

func TestS3Real_List(t *testing.T) {
	srv, b := newMockS3Server(t)
	defer srv.Close()
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

func TestS3Real_GetNotFound(t *testing.T) {
	srv, b := newMockS3Server(t)
	defer srv.Close()

	_, err := b.Get(context.Background(), "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent key")
	}
}
