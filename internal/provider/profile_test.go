package provider

import (
	"testing"
	"time"
)

func TestProfile_ClassifyHot(t *testing.T) {
	p := ProviderProfile{
		DailyEgressLimit:  0, // unlimited
		DailyIngressLimit: 0,
		MaxFileSize:       0,
	}
	if tier := p.Classify(); tier != TierHot {
		t.Fatalf("got %v, want TierHot", tier)
	}
}

func TestProfile_ClassifyWarm(t *testing.T) {
	p := ProviderProfile{
		DailyEgressLimit: 1 * 1024 * 1024 * 1024, // 1GB
	}
	if tier := p.Classify(); tier != TierWarm {
		t.Fatalf("got %v, want TierWarm", tier)
	}
}

func TestProfile_ClassifyCold(t *testing.T) {
	p := ProviderProfile{
		DailyEgressLimit: 100 * 1024 * 1024, // 100MB
	}
	if tier := p.Classify(); tier != TierCold {
		t.Fatalf("got %v, want TierCold", tier)
	}
}

func TestProfile_ClassifyByFileSize(t *testing.T) {
	p := ProviderProfile{
		MaxFileSize: 50 * 1024 * 1024, // 50MB limit
	}
	if tier := p.Classify(); tier != TierWarm {
		t.Fatalf("got %v, want TierWarm", tier)
	}
}

func TestBandwidthTracker_CanDownload(t *testing.T) {
	bt := NewBandwidthTracker(1024, 1024)
	if !bt.CanDownload(100) {
		t.Fatal("expected CanDownload to be true within limit")
	}
}

func TestBandwidthTracker_CanDownloadExceeded(t *testing.T) {
	bt := NewBandwidthTracker(100, 100)
	bt.Record(100, DirectionDownload)
	if bt.CanDownload(1) {
		t.Fatal("expected CanDownload to be false when exceeded")
	}
}

func TestBandwidthTracker_Reset(t *testing.T) {
	bt := NewBandwidthTracker(100, 100)
	bt.Record(100, DirectionDownload)
	// Simulate reset by moving the reset time to the past
	bt.nextReset = time.Now().Add(-1 * time.Second)
	if !bt.CanDownload(1) {
		t.Fatal("expected CanDownload to be true after reset")
	}
}

func TestBandwidthTracker_Record(t *testing.T) {
	bt := NewBandwidthTracker(1000, 1000)
	bt.Record(300, DirectionDownload)
	if bt.CanDownload(701) {
		t.Fatal("expected CanDownload to be false: 300+701 > 1000")
	}
	if !bt.CanDownload(700) {
		t.Fatal("expected CanDownload to be true: 300+700 = 1000")
	}
}
