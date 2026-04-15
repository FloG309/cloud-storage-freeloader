package bootstrap

import (
	"testing"

	"github.com/FloG309/cloud-storage-freeloader/internal/config"
)

func TestFactory_CreateS3(t *testing.T) {
	cfg := &config.Config{
		Providers: []config.ProviderConfig{
			{Name: "backblaze", Type: "s3", Endpoint: "http://localhost:9999", Region: "us-east-1", Bucket: "test", KeyID: "k", ApplicationKey: "s"},
		},
	}
	backends, infos, err := CreateBackends(cfg)
	if err != nil {
		t.Fatalf("CreateBackends: %v", err)
	}
	if len(backends) != 1 {
		t.Fatalf("got %d backends, want 1", len(backends))
	}
	if len(infos) != 1 {
		t.Fatalf("got %d infos, want 1", len(infos))
	}
}

func TestFactory_NamesMatchConfig(t *testing.T) {
	cfg := &config.Config{
		Providers: []config.ProviderConfig{
			{Name: "my-s3", Type: "s3", Endpoint: "http://localhost:9999", Region: "us-east-1", Bucket: "test", KeyID: "k", ApplicationKey: "s"},
		},
	}
	backends, _, _ := CreateBackends(cfg)
	if _, ok := backends["my-s3"]; !ok {
		t.Fatal("expected 'my-s3' key")
	}
}

func TestFactory_UnknownType(t *testing.T) {
	cfg := &config.Config{
		Providers: []config.ProviderConfig{
			{Name: "ftp", Type: "ftp"},
		},
	}
	_, _, err := CreateBackends(cfg)
	if err == nil {
		t.Fatal("expected error for unknown type")
	}
}

func TestFactory_SkipsEmptyTokens(t *testing.T) {
	// GDrive/OneDrive without tokens_file should fallback to memory
	cfg := &config.Config{
		Providers: []config.ProviderConfig{
			{Name: "gdrive", Type: "gdrive", ClientID: "cid", ClientSecret: "cs"},
			{Name: "onedrive", Type: "onedrive", ClientID: "cid", PublicClient: true},
		},
	}
	backends, _, err := CreateBackends(cfg)
	if err != nil {
		t.Fatalf("CreateBackends: %v", err)
	}
	if len(backends) != 2 {
		t.Fatalf("got %d backends, want 2", len(backends))
	}
}
