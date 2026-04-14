package placement

import (
	"fmt"
	"sort"

	"github.com/FloG309/cloud-storage-freeloader/internal/provider"
)

// PlacementMap maps shard index to provider ID.
type PlacementMap map[int]string

// ReadTarget describes where to read a specific shard.
type ReadTarget struct {
	ShardIndex int
	ProviderID string
	IsParity   bool
}

// ProviderInfo holds a provider's metadata for placement decisions.
type ProviderInfo struct {
	ID        string
	Profile   provider.ProviderProfile
	Tracker   *provider.BandwidthTracker
	Available int64
}

// Engine decides which provider stores which shard.
type Engine struct {
	providers []ProviderInfo
	k         int
}

// NewEngine creates a placement engine with the given providers.
func NewEngine(providers []ProviderInfo) *Engine {
	return &Engine{providers: providers}
}

func (e *Engine) providerByID(id string) *ProviderInfo {
	for i := range e.providers {
		if e.providers[i].ID == id {
			return &e.providers[i]
		}
	}
	return nil
}

// Place assigns shards to providers. k = data shards, m = parity shards.
// shardSizes[i] is the size of shard i.
func (e *Engine) Place(k, m int, shardSizes []int64) (PlacementMap, error) {
	e.k = k
	total := k + m

	// Sort providers by tier: hot first, then warm, then cold
	type ranked struct {
		info ProviderInfo
		tier provider.StorageTier
	}
	var candidates []ranked
	for _, p := range e.providers {
		candidates = append(candidates, ranked{info: p, tier: p.Profile.Classify()})
	}
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].tier < candidates[j].tier
	})

	pm := make(PlacementMap)
	used := make(map[string]bool)

	// Place data shards (prefer hot, then warm, never cold)
	for i := 0; i < k; i++ {
		placed := false
		for _, c := range candidates {
			if used[c.info.ID] {
				continue
			}
			if c.tier == provider.TierCold {
				continue // data shards avoid cold
			}
			if !e.canFit(c.info, shardSizes[i]) {
				continue
			}
			pm[i] = c.info.ID
			used[c.info.ID] = true
			placed = true
			break
		}
		if !placed {
			// Fallback: allow cold if no other option
			for _, c := range candidates {
				if used[c.info.ID] {
					continue
				}
				if !e.canFit(c.info, shardSizes[i]) {
					continue
				}
				pm[i] = c.info.ID
				used[c.info.ID] = true
				placed = true
				break
			}
		}
		if !placed {
			return nil, fmt.Errorf("cannot place data shard %d: no eligible provider", i)
		}
	}

	// Place parity shards (prefer cold, then warm, then hot)
	// Reverse order: cold first
	for i := k; i < total; i++ {
		placed := false
		for j := len(candidates) - 1; j >= 0; j-- {
			c := candidates[j]
			if used[c.info.ID] {
				continue
			}
			if !e.canFit(c.info, shardSizes[i]) {
				continue
			}
			pm[i] = c.info.ID
			used[c.info.ID] = true
			placed = true
			break
		}
		if !placed {
			return nil, fmt.Errorf("cannot place parity shard %d: no eligible provider", i)
		}
	}

	return pm, nil
}

func (e *Engine) canFit(info ProviderInfo, shardSize int64) bool {
	if info.Profile.MaxFileSize > 0 && shardSize > info.Profile.MaxFileSize {
		return false
	}
	if info.Available < shardSize {
		return false
	}
	return true
}

// SelectForRead picks providers to read k shards from, preferring hot providers
// and skipping unavailable or bandwidth-exhausted ones.
func (e *Engine) SelectForRead(pm PlacementMap, unavailable []string) ([]ReadTarget, error) {
	unavailSet := make(map[string]bool)
	for _, id := range unavailable {
		unavailSet[id] = true
	}

	// Determine k from the placement map or engine state
	k := e.k
	if k == 0 {
		// Infer: assume the highest data shard index + 1
		for idx := range pm {
			if idx+1 > k {
				k = idx + 1
			}
		}
		// But that counts parity too, so use engine's k if set
	}

	type candidate struct {
		shardIdx   int
		providerID string
		tier       provider.StorageTier
		isParity   bool
	}

	var candidates []candidate
	for shardIdx, pid := range pm {
		if unavailSet[pid] {
			continue
		}
		info := e.providerByID(pid)
		if info == nil {
			continue
		}
		// Skip bandwidth-exhausted providers
		if !info.Tracker.CanDownload(1) {
			continue
		}
		candidates = append(candidates, candidate{
			shardIdx:   shardIdx,
			providerID: pid,
			tier:       info.Profile.Classify(),
			isParity:   shardIdx >= k,
		})
	}

	// Sort: hot first, then warm, then cold
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].tier < candidates[j].tier
	})

	if len(candidates) < k {
		return nil, fmt.Errorf("not enough available providers: have %d, need %d", len(candidates), k)
	}

	// Take the first k
	targets := make([]ReadTarget, k)
	for i := 0; i < k; i++ {
		targets[i] = ReadTarget{
			ShardIndex: candidates[i].shardIdx,
			ProviderID: candidates[i].providerID,
			IsParity:   candidates[i].isParity,
		}
	}
	return targets, nil
}
