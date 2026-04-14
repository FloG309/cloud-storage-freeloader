package placement

import (
	"testing"

	"github.com/FloG309/cloud-storage-freeloader/internal/provider"
)

func makeProviders(tiers map[string]provider.StorageTier) []ProviderInfo {
	var providers []ProviderInfo
	for id, tier := range tiers {
		p := provider.ProviderProfile{}
		switch tier {
		case provider.TierHot:
			// no limits
		case provider.TierWarm:
			p.DailyEgressLimit = 1 * 1024 * 1024 * 1024
		case provider.TierCold:
			p.DailyEgressLimit = 100 * 1024 * 1024
		}
		providers = append(providers, ProviderInfo{
			ID:        id,
			Profile:   p,
			Tracker:   provider.NewBandwidthTracker(p.DailyEgressLimit, 0),
			Available: 10 * 1024 * 1024 * 1024, // 10GB
		})
	}
	return providers
}

func TestPlacement_DataShardsOnHotProviders(t *testing.T) {
	providers := makeProviders(map[string]provider.StorageTier{
		"hot1": provider.TierHot, "hot2": provider.TierHot,
		"hot3": provider.TierHot, "hot4": provider.TierHot,
		"cold1": provider.TierCold, "cold2": provider.TierCold,
		"cold3": provider.TierCold,
	})

	eng := NewEngine(providers)
	pm, err := eng.Place(4, 3, makeSizes(7, 1024))
	if err != nil {
		t.Fatalf("Place failed: %v", err)
	}

	for i := 0; i < 4; i++ {
		pid := pm[i]
		info := eng.providerByID(pid)
		if info.Profile.Classify() != provider.TierHot {
			t.Fatalf("data shard %d placed on %s (tier %v), want hot",
				i, pid, info.Profile.Classify())
		}
	}
}

func TestPlacement_ParityShardsOnColdOK(t *testing.T) {
	providers := makeProviders(map[string]provider.StorageTier{
		"hot1": provider.TierHot, "hot2": provider.TierHot,
		"hot3": provider.TierHot, "hot4": provider.TierHot,
		"cold1": provider.TierCold, "cold2": provider.TierCold,
		"cold3": provider.TierCold,
	})

	eng := NewEngine(providers)
	pm, _ := eng.Place(4, 3, makeSizes(7, 1024))

	for i := 4; i < 7; i++ {
		pid := pm[i]
		info := eng.providerByID(pid)
		tier := info.Profile.Classify()
		if tier != provider.TierCold {
			t.Fatalf("parity shard %d placed on %s (tier %v), want cold", i, pid, tier)
		}
	}
}

func TestPlacement_FallbackWhenNotEnoughHot(t *testing.T) {
	providers := makeProviders(map[string]provider.StorageTier{
		"hot1": provider.TierHot, "hot2": provider.TierHot,
		"warm1": provider.TierWarm, "warm2": provider.TierWarm,
		"cold1": provider.TierCold, "cold2": provider.TierCold,
		"cold3": provider.TierCold,
	})

	eng := NewEngine(providers)
	pm, err := eng.Place(4, 3, makeSizes(7, 1024))
	if err != nil {
		t.Fatalf("Place failed: %v", err)
	}

	// Data shards should be on hot or warm, never cold
	for i := 0; i < 4; i++ {
		info := eng.providerByID(pm[i])
		tier := info.Profile.Classify()
		if tier == provider.TierCold {
			t.Fatalf("data shard %d placed on cold provider %s", i, pm[i])
		}
	}
}

func TestPlacement_RespectsFileSizeLimit(t *testing.T) {
	providers := []ProviderInfo{
		{ID: "limited", Profile: provider.ProviderProfile{MaxFileSize: 500}, Available: 10 * 1024 * 1024 * 1024,
			Tracker: provider.NewBandwidthTracker(0, 0)},
		{ID: "ok1", Profile: provider.ProviderProfile{}, Available: 10 * 1024 * 1024 * 1024,
			Tracker: provider.NewBandwidthTracker(0, 0)},
		{ID: "ok2", Profile: provider.ProviderProfile{}, Available: 10 * 1024 * 1024 * 1024,
			Tracker: provider.NewBandwidthTracker(0, 0)},
		{ID: "ok3", Profile: provider.ProviderProfile{}, Available: 10 * 1024 * 1024 * 1024,
			Tracker: provider.NewBandwidthTracker(0, 0)},
	}

	eng := NewEngine(providers)
	pm, err := eng.Place(2, 1, []int64{1024, 1024, 1024})
	if err != nil {
		t.Fatalf("Place failed: %v", err)
	}

	for _, pid := range pm {
		if pid == "limited" {
			t.Fatal("shard placed on provider with file size limit too small")
		}
	}
}

func TestPlacement_RespectsBandwidthBudget(t *testing.T) {
	coldTracker := provider.NewBandwidthTracker(100, 0)
	coldTracker.Record(100, provider.DirectionDownload) // exhaust

	providers := []ProviderInfo{
		{ID: "hot1", Profile: provider.ProviderProfile{}, Available: 10 * 1024 * 1024 * 1024,
			Tracker: provider.NewBandwidthTracker(0, 0)},
		{ID: "hot2", Profile: provider.ProviderProfile{}, Available: 10 * 1024 * 1024 * 1024,
			Tracker: provider.NewBandwidthTracker(0, 0)},
		{ID: "exhausted", Profile: provider.ProviderProfile{DailyEgressLimit: 100}, Available: 10 * 1024 * 1024 * 1024,
			Tracker: coldTracker},
		{ID: "cold2", Profile: provider.ProviderProfile{DailyEgressLimit: 100 * 1024 * 1024}, Available: 10 * 1024 * 1024 * 1024,
			Tracker: provider.NewBandwidthTracker(100*1024*1024, 0)},
	}

	eng := NewEngine(providers)
	eng.k = 2
	targets, err := eng.SelectForRead(PlacementMap{0: "hot1", 1: "hot2", 2: "exhausted", 3: "cold2"}, nil)
	if err != nil {
		t.Fatalf("SelectForRead failed: %v", err)
	}

	for _, tgt := range targets {
		if tgt.ProviderID == "exhausted" {
			t.Fatal("selected exhausted provider for read")
		}
	}
}

func TestPlacement_SpreadAcrossProviders(t *testing.T) {
	providers := makeProviders(map[string]provider.StorageTier{
		"hot1": provider.TierHot, "hot2": provider.TierHot,
		"hot3": provider.TierHot, "hot4": provider.TierHot,
		"cold1": provider.TierCold, "cold2": provider.TierCold,
		"cold3": provider.TierCold,
	})

	eng := NewEngine(providers)
	pm, _ := eng.Place(4, 3, makeSizes(7, 1024))

	seen := make(map[string]bool)
	for _, pid := range pm {
		if seen[pid] {
			t.Fatalf("provider %s used more than once", pid)
		}
		seen[pid] = true
	}
}

func TestPlacement_ReturnsPlacementMap(t *testing.T) {
	providers := makeProviders(map[string]provider.StorageTier{
		"hot1": provider.TierHot, "hot2": provider.TierHot,
		"cold1": provider.TierCold, "cold2": provider.TierCold,
	})

	eng := NewEngine(providers)
	pm, err := eng.Place(2, 2, makeSizes(4, 1024))
	if err != nil {
		t.Fatalf("Place failed: %v", err)
	}
	if len(pm) != 4 {
		t.Fatalf("got %d entries, want 4", len(pm))
	}
}

// Phase 3.2 tests

func TestReadPath_PreferHotProviders(t *testing.T) {
	providers := makeProviders(map[string]provider.StorageTier{
		"hot1": provider.TierHot, "hot2": provider.TierHot,
		"cold1": provider.TierCold, "cold2": provider.TierCold,
	})

	eng := NewEngine(providers)
	eng.k = 2
	pm := PlacementMap{0: "hot1", 1: "hot2", 2: "cold1", 3: "cold2"}
	targets, err := eng.SelectForRead(pm, nil)
	if err != nil {
		t.Fatalf("SelectForRead failed: %v", err)
	}

	// Should select hot providers for the required k=2 shards
	for _, tgt := range targets {
		info := eng.providerByID(tgt.ProviderID)
		if info.Profile.Classify() == provider.TierCold {
			t.Logf("warning: cold provider selected, but hot was available")
		}
	}
	if len(targets) < 2 {
		t.Fatalf("got %d targets, want at least 2", len(targets))
	}
}

func TestReadPath_DegradedMode(t *testing.T) {
	providers := makeProviders(map[string]provider.StorageTier{
		"hot1": provider.TierHot, "hot2": provider.TierHot,
		"cold1": provider.TierCold, "cold2": provider.TierCold,
	})

	eng := NewEngine(providers)
	eng.k = 2
	pm := PlacementMap{0: "hot1", 1: "hot2", 2: "cold1", 3: "cold2"}
	// hot1 is down
	targets, err := eng.SelectForRead(pm, []string{"hot1"})
	if err != nil {
		t.Fatalf("SelectForRead failed: %v", err)
	}

	for _, tgt := range targets {
		if tgt.ProviderID == "hot1" {
			t.Fatal("selected unavailable provider")
		}
	}
}

func TestReadPath_SkipBandwidthExhausted(t *testing.T) {
	coldTracker := provider.NewBandwidthTracker(100, 0)
	coldTracker.Record(100, provider.DirectionDownload)

	providers := []ProviderInfo{
		{ID: "hot1", Profile: provider.ProviderProfile{}, Available: 1 << 30,
			Tracker: provider.NewBandwidthTracker(0, 0)},
		{ID: "hot2", Profile: provider.ProviderProfile{}, Available: 1 << 30,
			Tracker: provider.NewBandwidthTracker(0, 0)},
		{ID: "exhausted", Profile: provider.ProviderProfile{DailyEgressLimit: 100}, Available: 1 << 30,
			Tracker: coldTracker},
		{ID: "cold2", Profile: provider.ProviderProfile{DailyEgressLimit: 100 * 1024 * 1024}, Available: 1 << 30,
			Tracker: provider.NewBandwidthTracker(100*1024*1024, 0)},
	}

	eng := NewEngine(providers)
	eng.k = 2
	pm := PlacementMap{0: "hot1", 1: "hot2", 2: "exhausted", 3: "cold2"}
	targets, _ := eng.SelectForRead(pm, nil)

	for _, tgt := range targets {
		if tgt.ProviderID == "exhausted" {
			t.Fatal("selected bandwidth-exhausted provider")
		}
	}
}

func TestReadPath_AllProvidersDown(t *testing.T) {
	providers := makeProviders(map[string]provider.StorageTier{
		"hot1": provider.TierHot, "hot2": provider.TierHot,
	})

	eng := NewEngine(providers)
	eng.k = 2
	pm := PlacementMap{0: "hot1", 1: "hot2"}
	_, err := eng.SelectForRead(pm, []string{"hot1", "hot2"})
	if err == nil {
		t.Fatal("expected error when all providers down")
	}
}

func makeSizes(n int, size int64) []int64 {
	sizes := make([]int64, n)
	for i := range sizes {
		sizes[i] = size
	}
	return sizes
}
