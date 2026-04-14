package provider

import (
	"context"
	"testing"
)

func TestRegistry_RegisterAndCreate(t *testing.T) {
	r := NewRegistry()
	r.Register("memory", func(cfg map[string]string) (StorageBackend, error) {
		return &stubBackend{}, nil
	})

	backend, err := r.New("memory", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if backend == nil {
		t.Fatal("expected non-nil backend")
	}

	// Verify it satisfies the interface by calling a method
	_, err = backend.List(context.Background(), "")
	if err != nil {
		t.Fatalf("unexpected error from List: %v", err)
	}
}

func TestRegistry_UnknownProvider(t *testing.T) {
	r := NewRegistry()
	_, err := r.New("nonexistent", nil)
	if err == nil {
		t.Fatal("expected error for unknown provider")
	}
}

// stubBackend is a minimal implementation for registry tests
type stubBackend struct{}

func (s *stubBackend) Put(ctx context.Context, key string, data []byte) error   { return nil }
func (s *stubBackend) Get(ctx context.Context, key string) ([]byte, error)      { return nil, nil }
func (s *stubBackend) Delete(ctx context.Context, key string) error             { return nil }
func (s *stubBackend) Exists(ctx context.Context, key string) (bool, error)     { return false, nil }
func (s *stubBackend) List(ctx context.Context, prefix string) ([]string, error) { return nil, nil }
func (s *stubBackend) Available(ctx context.Context) (int64, error)             { return 0, nil }
func (s *stubBackend) Close() error                                              { return nil }
