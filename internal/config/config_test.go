package config

import (
	"os"
	"path/filepath"
	"testing"
)

const testYAML = `
base_folder: "cloud-storage-freeloader"
providers:
  - name: backblaze
    type: s3
    endpoint: s3.eu-central-003.backblazeb2.com
    region: eu-central-003
    bucket: freeloader-test-bucket
    key_id: "test-key-id"
    application_key: "test-app-key"
    tier: hot
  - name: gdrive
    type: gdrive
    client_id: "test-client-id.apps.googleusercontent.com"
    client_secret: "test-client-secret"
    tokens_file: "tokens/gdrive_tokens.json"
    base_folder: "cloud-storage-freeloader"
    tier: hot
  - name: onedrive
    type: onedrive
    client_id: "test-onedrive-client-id"
    auth_endpoint: "https://login.microsoftonline.com/common/oauth2/v2.0"
    public_client: true
    tokens_file: "tokens/onedrive_tokens.json"
    base_folder: "cloud-storage-freeloader"
    tier: warm
`

func TestConfig_LoadFromBytes(t *testing.T) {
	cfg, err := LoadBytes([]byte(testYAML))
	if err != nil {
		t.Fatalf("LoadBytes failed: %v", err)
	}
	if len(cfg.Providers) != 3 {
		t.Fatalf("got %d providers, want 3", len(cfg.Providers))
	}
}

func TestConfig_LoadFromFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	os.WriteFile(path, []byte(testYAML), 0644)

	cfg, err := LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile failed: %v", err)
	}
	if len(cfg.Providers) != 3 {
		t.Fatalf("got %d providers, want 3", len(cfg.Providers))
	}
}

func TestConfig_BackblazeFields(t *testing.T) {
	cfg, _ := LoadBytes([]byte(testYAML))
	p := cfg.Provider("backblaze")
	if p == nil {
		t.Fatal("backblaze provider not found")
	}
	if p.Type != "s3" {
		t.Fatalf("type = %s, want s3", p.Type)
	}
	if p.Endpoint != "s3.eu-central-003.backblazeb2.com" {
		t.Fatalf("endpoint = %s", p.Endpoint)
	}
	if p.Region != "eu-central-003" {
		t.Fatalf("region = %s", p.Region)
	}
	if p.Bucket != "freeloader-test-bucket" {
		t.Fatalf("bucket = %s", p.Bucket)
	}
	if p.KeyID != "test-key-id" {
		t.Fatalf("key_id = %s", p.KeyID)
	}
	if p.ApplicationKey != "test-app-key" {
		t.Fatalf("application_key = %s", p.ApplicationKey)
	}
}

func TestConfig_GDriveFields(t *testing.T) {
	cfg, _ := LoadBytes([]byte(testYAML))
	p := cfg.Provider("gdrive")
	if p == nil {
		t.Fatal("gdrive provider not found")
	}
	if p.ClientID == "" || p.ClientSecret == "" || p.TokensFile == "" {
		t.Fatalf("missing fields: %+v", p)
	}
	if p.BaseFolder != "cloud-storage-freeloader" {
		t.Fatalf("base_folder = %s", p.BaseFolder)
	}
}

func TestConfig_OneDriveFields(t *testing.T) {
	cfg, _ := LoadBytes([]byte(testYAML))
	p := cfg.Provider("onedrive")
	if p == nil {
		t.Fatal("onedrive provider not found")
	}
	if p.ClientID == "" || p.TokensFile == "" {
		t.Fatalf("missing fields: %+v", p)
	}
	if !p.PublicClient {
		t.Fatal("expected PublicClient=true")
	}
	if p.AuthEndpoint != "https://login.microsoftonline.com/common/oauth2/v2.0" {
		t.Fatalf("auth_endpoint = %s", p.AuthEndpoint)
	}
}

func TestConfig_MissingFile(t *testing.T) {
	_, err := LoadFile("/nonexistent/path/config.yaml")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestConfig_ProviderByName(t *testing.T) {
	cfg, _ := LoadBytes([]byte(testYAML))
	if cfg.Provider("backblaze") == nil {
		t.Fatal("expected backblaze")
	}
	if cfg.Provider("unknown") != nil {
		t.Fatal("expected nil for unknown")
	}
}

func TestConfig_BaseFolder(t *testing.T) {
	cfg, _ := LoadBytes([]byte(testYAML))
	if cfg.BaseFolder != "cloud-storage-freeloader" {
		t.Fatalf("base_folder = %s", cfg.BaseFolder)
	}
}

func TestConfig_TierField(t *testing.T) {
	cfg, _ := LoadBytes([]byte(testYAML))
	if cfg.Provider("backblaze").Tier != "hot" {
		t.Fatalf("tier = %s, want hot", cfg.Provider("backblaze").Tier)
	}
	if cfg.Provider("onedrive").Tier != "warm" {
		t.Fatalf("tier = %s, want warm", cfg.Provider("onedrive").Tier)
	}
}
