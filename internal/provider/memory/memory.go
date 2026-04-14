package memory

import (
	"context"
	"fmt"
	"strings"

	"github.com/FloG309/cloud-storage-freeloader/internal/provider"
)

// Backend is an in-memory StorageBackend for testing.
type Backend struct {
	data     map[string][]byte
	capacity int64
	used     int64
}

// New creates an in-memory backend with the given capacity in bytes.
func New(capacity int64) *Backend {
	return &Backend{
		data:     make(map[string][]byte),
		capacity: capacity,
	}
}

func (b *Backend) Put(_ context.Context, key string, data []byte) error {
	size := int64(len(data))
	// If key already exists, account for replacement
	if existing, ok := b.data[key]; ok {
		size -= int64(len(existing))
	}
	if b.used+size > b.capacity {
		return fmt.Errorf("insufficient capacity: need %d, have %d", size, b.capacity-b.used)
	}
	b.data[key] = append([]byte(nil), data...)
	b.used += size
	return nil
}

func (b *Backend) Get(_ context.Context, key string) ([]byte, error) {
	data, ok := b.data[key]
	if !ok {
		return nil, fmt.Errorf("key not found: %s", key)
	}
	return append([]byte(nil), data...), nil
}

func (b *Backend) Delete(_ context.Context, key string) error {
	data, ok := b.data[key]
	if !ok {
		return fmt.Errorf("key not found: %s", key)
	}
	b.used -= int64(len(data))
	delete(b.data, key)
	return nil
}

func (b *Backend) Exists(_ context.Context, key string) (bool, error) {
	_, ok := b.data[key]
	return ok, nil
}

func (b *Backend) List(_ context.Context, prefix string) ([]string, error) {
	var keys []string
	for k := range b.data {
		if strings.HasPrefix(k, prefix) {
			keys = append(keys, k)
		}
	}
	return keys, nil
}

func (b *Backend) Available(_ context.Context) (int64, error) {
	return b.capacity - b.used, nil
}

func (b *Backend) Close() error {
	return nil
}

// Verify interface compliance at compile time.
var _ provider.StorageBackend = (*Backend)(nil)
