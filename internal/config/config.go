package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Config is the top-level configuration.
type Config struct {
	BaseFolder string           `yaml:"base_folder"`
	Providers  []ProviderConfig `yaml:"providers"`
}

// ProviderConfig holds configuration for a single provider.
type ProviderConfig struct {
	Name           string `yaml:"name"`
	Type           string `yaml:"type"`
	Tier           string `yaml:"tier"`
	Endpoint       string `yaml:"endpoint"`
	Region         string `yaml:"region"`
	Bucket         string `yaml:"bucket"`
	KeyID          string `yaml:"key_id"`
	ApplicationKey string `yaml:"application_key"`
	ClientID       string `yaml:"client_id"`
	ClientSecret   string `yaml:"client_secret"`
	TokensFile     string `yaml:"tokens_file"`
	BaseFolder     string `yaml:"base_folder"`
	AuthEndpoint   string `yaml:"auth_endpoint"`
	PublicClient   bool   `yaml:"public_client"`
}

// LoadFile loads config from a YAML file.
func LoadFile(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	return LoadBytes(data)
}

// LoadBytes parses config from YAML bytes.
func LoadBytes(data []byte) (*Config, error) {
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	return &cfg, nil
}

// Provider returns the config for a named provider, or nil.
func (c *Config) Provider(name string) *ProviderConfig {
	for i := range c.Providers {
		if c.Providers[i].Name == name {
			return &c.Providers[i]
		}
	}
	return nil
}
