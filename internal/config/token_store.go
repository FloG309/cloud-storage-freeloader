package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// TokenData holds OAuth2 tokens.
type TokenData struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
}

// LoadTokens loads OAuth2 tokens from a JSON file.
func LoadTokens(path string) (*TokenData, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read tokens: %w", err)
	}
	var td TokenData
	if err := json.Unmarshal(data, &td); err != nil {
		return nil, fmt.Errorf("parse tokens: %w", err)
	}
	return &td, nil
}

// SaveTokens writes OAuth2 tokens to a JSON file atomically.
func SaveTokens(path string, td *TokenData) error {
	data, err := json.MarshalIndent(td, "", "  ")
	if err != nil {
		return err
	}
	// Atomic write: temp file + rename
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".tokens-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp: %w", err)
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return err
	}
	tmp.Close()
	return os.Rename(tmpPath, path)
}
