package provider

import (
	"context"
	"fmt"
)

// StorageBackend is the interface that all cloud storage providers implement.
type StorageBackend interface {
	Put(ctx context.Context, key string, data []byte) error
	Get(ctx context.Context, key string) ([]byte, error)
	Delete(ctx context.Context, key string) error
	Exists(ctx context.Context, key string) (bool, error)
	List(ctx context.Context, prefix string) ([]string, error)
	Available(ctx context.Context) (int64, error)
	Close() error
}

// Factory creates a StorageBackend from configuration.
type Factory func(cfg map[string]string) (StorageBackend, error)

// Registry manages provider factories.
type Registry struct {
	factories map[string]Factory
}

// NewRegistry creates an empty provider registry.
func NewRegistry() *Registry {
	return &Registry{factories: make(map[string]Factory)}
}

// Register adds a provider factory under the given name.
func (r *Registry) Register(name string, f Factory) {
	r.factories[name] = f
}

// New creates a StorageBackend instance for the named provider.
func (r *Registry) New(name string, cfg map[string]string) (StorageBackend, error) {
	f, ok := r.factories[name]
	if !ok {
		return nil, fmt.Errorf("unknown provider: %s", name)
	}
	return f(cfg)
}
