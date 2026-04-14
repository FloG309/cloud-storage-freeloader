package webdav

import (
	"context"

	"github.com/FloG309/cloud-storage-freeloader/internal/provider"
)

// Backend implements provider.StorageBackend for WebDAV servers.
type Backend struct {
	store provider.StorageBackend
}

// NewWithStore creates a WebDAV backend backed by the given store (for testing).
func NewWithStore(store provider.StorageBackend) *Backend {
	return &Backend{store: store}
}

func (b *Backend) Put(ctx context.Context, key string, data []byte) error {
	return b.store.Put(ctx, key, data)
}

func (b *Backend) Get(ctx context.Context, key string) ([]byte, error) {
	return b.store.Get(ctx, key)
}

func (b *Backend) Delete(ctx context.Context, key string) error {
	return b.store.Delete(ctx, key)
}

func (b *Backend) Exists(ctx context.Context, key string) (bool, error) {
	return b.store.Exists(ctx, key)
}

func (b *Backend) List(ctx context.Context, prefix string) ([]string, error) {
	return b.store.List(ctx, prefix)
}

func (b *Backend) Available(ctx context.Context) (int64, error) {
	return b.store.Available(ctx)
}

func (b *Backend) Close() error {
	return b.store.Close()
}

var _ provider.StorageBackend = (*Backend)(nil)
