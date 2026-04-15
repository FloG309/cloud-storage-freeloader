package bootstrap

import (
	"fmt"

	"github.com/FloG309/cloud-storage-freeloader/internal/config"
	"github.com/FloG309/cloud-storage-freeloader/internal/placement"
	"github.com/FloG309/cloud-storage-freeloader/internal/provider"
	"github.com/FloG309/cloud-storage-freeloader/internal/provider/gdrive"
	"github.com/FloG309/cloud-storage-freeloader/internal/provider/memory"
	"github.com/FloG309/cloud-storage-freeloader/internal/provider/onedrive"
	s3provider "github.com/FloG309/cloud-storage-freeloader/internal/provider/s3"
)

// CreateBackends creates real provider backends from config.
func CreateBackends(cfg *config.Config) (map[string]provider.StorageBackend, []placement.ProviderInfo, error) {
	backends := make(map[string]provider.StorageBackend)
	var infos []placement.ProviderInfo

	for _, p := range cfg.Providers {
		var backend provider.StorageBackend
		var err error

		cfgMap := providerConfigToMap(p)

		switch p.Type {
		case "s3":
			backend, err = s3provider.NewFromConfig(cfgMap)

		case "gdrive":
			if p.TokensFile != "" {
				backend, err = gdrive.NewFromConfig(cfgMap)
			} else {
				backend = gdrive.NewWithStore(memory.New(15 * 1024 * 1024 * 1024))
			}

		case "onedrive":
			if p.TokensFile != "" {
				backend, err = onedrive.NewFromConfig(cfgMap)
			} else {
				backend = onedrive.NewWithStore(memory.New(5 * 1024 * 1024 * 1024))
			}

		default:
			return nil, nil, fmt.Errorf("unknown provider type: %s", p.Type)
		}

		if err != nil {
			return nil, nil, fmt.Errorf("create %s (%s): %w", p.Name, p.Type, err)
		}

		backends[p.Name] = backend

		profile := provider.ProviderProfile{}
		switch p.Tier {
		case "warm":
			profile.DailyEgressLimit = 1 * 1024 * 1024 * 1024
		case "cold":
			profile.DailyEgressLimit = 100 * 1024 * 1024
		}

		infos = append(infos, placement.ProviderInfo{
			ID:        p.Name,
			Profile:   profile,
			Tracker:   provider.NewBandwidthTracker(profile.DailyEgressLimit, 0),
			Available: 10 * 1024 * 1024 * 1024,
		})
	}

	return backends, infos, nil
}

func providerConfigToMap(p config.ProviderConfig) map[string]string {
	m := map[string]string{
		"name":            p.Name,
		"type":            p.Type,
		"endpoint":        p.Endpoint,
		"region":          p.Region,
		"bucket":          p.Bucket,
		"key_id":          p.KeyID,
		"application_key": p.ApplicationKey,
		"client_id":       p.ClientID,
		"client_secret":   p.ClientSecret,
		"tokens_file":     p.TokensFile,
		"base_folder":     p.BaseFolder,
		"auth_endpoint":   p.AuthEndpoint,
	}
	return m
}
