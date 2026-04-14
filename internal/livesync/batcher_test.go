package livesync

import (
	"sync"
	"testing"
	"time"
)

func TestBatcher_AccumulatesPatches(t *testing.T) {
	var flushed []FilePatches
	var mu sync.Mutex
	b := NewBatcher(BatcherConfig{
		FlushInterval: 1 * time.Hour, // long interval — won't fire
		MaxPatches:    1000,
		MaxBytes:      1 << 20,
	}, func(batch []FilePatches) error {
		mu.Lock()
		flushed = append(flushed, batch...)
		mu.Unlock()
		return nil
	})
	defer b.Stop()

	for i := 0; i < 5; i++ {
		b.Add(FilePatches{Seq: int64(i), Patches: []Patch{{Type: PatchInsert, Data: []byte("x")}}})
	}

	mu.Lock()
	count := len(flushed)
	mu.Unlock()
	if count != 0 {
		t.Fatalf("expected no flush, got %d", count)
	}
}

func TestBatcher_FlushOnTimer(t *testing.T) {
	var flushed []FilePatches
	var mu sync.Mutex
	done := make(chan struct{})

	b := NewBatcher(BatcherConfig{
		FlushInterval: 50 * time.Millisecond,
		MaxPatches:    1000,
		MaxBytes:      1 << 20,
	}, func(batch []FilePatches) error {
		mu.Lock()
		flushed = append(flushed, batch...)
		mu.Unlock()
		close(done)
		return nil
	})
	defer b.Stop()

	b.Add(FilePatches{Seq: 1, Patches: []Patch{{Type: PatchInsert, Data: []byte("x")}}})

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("flush timeout")
	}

	mu.Lock()
	if len(flushed) != 1 {
		t.Fatalf("got %d flushed, want 1", len(flushed))
	}
	mu.Unlock()
}

func TestBatcher_FlushOnThreshold(t *testing.T) {
	var flushed []FilePatches
	var mu sync.Mutex
	done := make(chan struct{}, 1)

	b := NewBatcher(BatcherConfig{
		FlushInterval: 1 * time.Hour,
		MaxPatches:    5,
		MaxBytes:      1 << 20,
	}, func(batch []FilePatches) error {
		mu.Lock()
		flushed = append(flushed, batch...)
		mu.Unlock()
		select {
		case done <- struct{}{}:
		default:
		}
		return nil
	})
	defer b.Stop()

	for i := 0; i < 5; i++ {
		b.Add(FilePatches{Seq: int64(i), Patches: []Patch{{Type: PatchInsert, Data: []byte("x")}}})
	}

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("flush timeout")
	}

	mu.Lock()
	if len(flushed) != 5 {
		t.Fatalf("got %d flushed, want 5", len(flushed))
	}
	mu.Unlock()
}

func TestBatcher_FlushOnSizeThreshold(t *testing.T) {
	var flushed []FilePatches
	var mu sync.Mutex
	done := make(chan struct{}, 1)

	b := NewBatcher(BatcherConfig{
		FlushInterval: 1 * time.Hour,
		MaxPatches:    1000,
		MaxBytes:      100,
	}, func(batch []FilePatches) error {
		mu.Lock()
		flushed = append(flushed, batch...)
		mu.Unlock()
		select {
		case done <- struct{}{}:
		default:
		}
		return nil
	})
	defer b.Stop()

	// Add patches that exceed 100 bytes total
	b.Add(FilePatches{Seq: 1, Patches: []Patch{{Type: PatchInsert, Data: make([]byte, 60)}}})
	b.Add(FilePatches{Seq: 2, Patches: []Patch{{Type: PatchInsert, Data: make([]byte, 60)}}})

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("flush timeout")
	}
}

func TestBatcher_EmptyFlush(t *testing.T) {
	flushCount := 0
	b := NewBatcher(BatcherConfig{
		FlushInterval: 50 * time.Millisecond,
		MaxPatches:    1000,
		MaxBytes:      1 << 20,
	}, func(batch []FilePatches) error {
		flushCount++
		return nil
	})
	defer b.Stop()

	// Don't add anything, wait for timer
	time.Sleep(150 * time.Millisecond)
	if flushCount != 0 {
		t.Fatalf("expected no flush for empty batch, got %d", flushCount)
	}
}

func TestBatcher_ConfigurableInterval(t *testing.T) {
	done := make(chan struct{})
	start := time.Now()

	b := NewBatcher(BatcherConfig{
		FlushInterval: 100 * time.Millisecond,
		MaxPatches:    1000,
		MaxBytes:      1 << 20,
	}, func(batch []FilePatches) error {
		close(done)
		return nil
	})
	defer b.Stop()

	b.Add(FilePatches{Seq: 1, Patches: []Patch{{Type: PatchInsert, Data: []byte("x")}}})

	<-done
	elapsed := time.Since(start)
	if elapsed < 80*time.Millisecond {
		t.Fatalf("flushed too early: %v", elapsed)
	}
}
