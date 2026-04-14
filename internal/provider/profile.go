package provider

import "time"

// StorageTier classifies a provider's quality for shard placement.
type StorageTier int

const (
	TierHot  StorageTier = iota // Unlimited or high bandwidth
	TierWarm                    // Moderate limits
	TierCold                    // Severe bandwidth limits
)

func (t StorageTier) String() string {
	switch t {
	case TierHot:
		return "hot"
	case TierWarm:
		return "warm"
	case TierCold:
		return "cold"
	}
	return "unknown"
}

// ProviderProfile describes a provider's constraints.
type ProviderProfile struct {
	DailyEgressLimit  int64 // bytes/day, 0 = unlimited
	DailyIngressLimit int64 // bytes/day, 0 = unlimited
	MaxFileSize       int64 // bytes, 0 = unlimited
	TotalCapacity     int64 // bytes
}

const (
	coldEgressThreshold = 500 * 1024 * 1024  // 500MB
	warmFileSizeLimit   = 100 * 1024 * 1024  // 100MB - providers with file size limits are at most warm
)

// Classify derives a StorageTier from the provider's constraints.
func (p ProviderProfile) Classify() StorageTier {
	if p.DailyEgressLimit > 0 && p.DailyEgressLimit < coldEgressThreshold {
		return TierCold
	}
	if p.DailyEgressLimit > 0 || (p.MaxFileSize > 0 && p.MaxFileSize <= warmFileSizeLimit) {
		return TierWarm
	}
	return TierHot
}

// Direction indicates upload or download.
type Direction int

const (
	DirectionDownload Direction = iota
	DirectionUpload
)

// BandwidthTracker tracks daily bandwidth usage for a provider.
type BandwidthTracker struct {
	dailyDownloadLimit int64
	dailyUploadLimit   int64
	downloadUsed       int64
	uploadUsed         int64
	nextReset          time.Time
}

// NewBandwidthTracker creates a tracker with the given daily limits (0 = unlimited).
func NewBandwidthTracker(downloadLimit, uploadLimit int64) *BandwidthTracker {
	return &BandwidthTracker{
		dailyDownloadLimit: downloadLimit,
		dailyUploadLimit:   uploadLimit,
		nextReset:          time.Now().Add(24 * time.Hour),
	}
}

func (bt *BandwidthTracker) maybeReset() {
	if time.Now().After(bt.nextReset) {
		bt.downloadUsed = 0
		bt.uploadUsed = 0
		bt.nextReset = time.Now().Add(24 * time.Hour)
	}
}

// CanDownload returns true if downloading size bytes is within the daily limit.
func (bt *BandwidthTracker) CanDownload(size int64) bool {
	bt.maybeReset()
	if bt.dailyDownloadLimit == 0 {
		return true
	}
	return bt.downloadUsed+size <= bt.dailyDownloadLimit
}

// CanUpload returns true if uploading size bytes is within the daily limit.
func (bt *BandwidthTracker) CanUpload(size int64) bool {
	bt.maybeReset()
	if bt.dailyUploadLimit == 0 {
		return true
	}
	return bt.uploadUsed+size <= bt.dailyUploadLimit
}

// Record records bandwidth usage.
func (bt *BandwidthTracker) Record(size int64, dir Direction) {
	bt.maybeReset()
	switch dir {
	case DirectionDownload:
		bt.downloadUsed += size
	case DirectionUpload:
		bt.uploadUsed += size
	}
}
