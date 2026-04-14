package s3

import (
	"context"

	"github.com/FloG309/cloud-storage-freeloader/internal/provider"
)

// Backend implements provider.StorageBackend for S3-compatible storage.
// For unit tests, it delegates to an injected store. For production,
// it uses the AWS SDK (configured via NewFromConfig).
type Backend struct {
	store provider.StorageBackend
}

// NewWithStore creates an S3 backend backed by the given store.
// Used for unit testing without network access.
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

// Profile returns the provider's constraint profile.
func (b *Backend) Profile() provider.ProviderProfile {
	return provider.ProviderProfile{
		// Backblaze B2 free tier: 1GB egress/day, but S3-compatible
		// buckets generally have no file size limit. Default to hot.
		DailyEgressLimit: 0,
		MaxFileSize:      0,
	}
}

var _ provider.StorageBackend = (*Backend)(nil)
