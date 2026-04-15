package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestTokenStore_LoadTokens(t *testing.T) {
	data := `{"access_token":"at123","refresh_token":"rt456","expires_in":3599}`
	dir := t.TempDir()
	path := filepath.Join(dir, "tokens.json")
	os.WriteFile(path, []byte(data), 0644)

	td, err := LoadTokens(path)
	if err != nil {
		t.Fatalf("LoadTokens: %v", err)
	}
	if td.AccessToken != "at123" || td.RefreshToken != "rt456" || td.ExpiresIn != 3599 {
		t.Fatalf("unexpected: %+v", td)
	}
}

func TestTokenStore_SaveTokens(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tokens.json")

	td := &TokenData{AccessToken: "new-at", RefreshToken: "new-rt", ExpiresIn: 7200}
	if err := SaveTokens(path, td); err != nil {
		t.Fatalf("SaveTokens: %v", err)
	}

	got, err := LoadTokens(path)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if got.AccessToken != "new-at" || got.RefreshToken != "new-rt" {
		t.Fatalf("roundtrip failed: %+v", got)
	}
}

func TestTokenStore_MissingFile(t *testing.T) {
	_, err := LoadTokens("/nonexistent/tokens.json")
	if err == nil {
		t.Fatal("expected error")
	}
}
