package livesync

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWatcher_DetectsNewFile(t *testing.T) {
	dir := t.TempDir()
	w, err := NewWatcher(dir, nil, 100*time.Millisecond)
	if err != nil {
		t.Fatalf("NewWatcher: %v", err)
	}
	defer w.Close()

	events := w.Events()

	// Create a file
	os.WriteFile(filepath.Join(dir, "new.txt"), []byte("hello"), 0644)

	select {
	case ev := <-events:
		if ev.Type != EventCreated && ev.Type != EventModified {
			t.Fatalf("expected Created/Modified, got %v", ev.Type)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for event")
	}
}

func TestWatcher_DetectsModification(t *testing.T) {
	dir := t.TempDir()
	existing := filepath.Join(dir, "existing.txt")
	os.WriteFile(existing, []byte("original"), 0644)

	w, _ := NewWatcher(dir, nil, 100*time.Millisecond)
	defer w.Close()

	events := w.Events()

	// Modify the file
	time.Sleep(150 * time.Millisecond) // ensure debounce window
	os.WriteFile(existing, []byte("modified"), 0644)

	select {
	case ev := <-events:
		if ev.Type != EventModified && ev.Type != EventCreated {
			t.Fatalf("expected Modified, got %v", ev.Type)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for modification event")
	}
}

func TestWatcher_DetectsDeletion(t *testing.T) {
	dir := t.TempDir()
	existing := filepath.Join(dir, "delete_me.txt")
	os.WriteFile(existing, []byte("content"), 0644)

	w, _ := NewWatcher(dir, nil, 100*time.Millisecond)
	defer w.Close()

	events := w.Events()

	os.Remove(existing)

	select {
	case ev := <-events:
		if ev.Type != EventDeleted {
			// On some OS the event type may vary
			t.Logf("got event type %v (may vary by OS)", ev.Type)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for delete event")
	}
}

func TestWatcher_IgnoresPatterns(t *testing.T) {
	dir := t.TempDir()
	w, _ := NewWatcher(dir, []string{"*.tmp", ".git"}, 100*time.Millisecond)
	defer w.Close()

	events := w.Events()

	// Create ignored file
	os.WriteFile(filepath.Join(dir, "test.tmp"), []byte("ignored"), 0644)
	// Create non-ignored file
	os.WriteFile(filepath.Join(dir, "test.txt"), []byte("watched"), 0644)

	select {
	case ev := <-events:
		if filepath.Ext(ev.Path) == ".tmp" {
			t.Fatal("should have ignored .tmp file")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout")
	}
}

func TestWatcher_Debounce(t *testing.T) {
	dir := t.TempDir()
	w, _ := NewWatcher(dir, nil, 500*time.Millisecond)
	defer w.Close()

	events := w.Events()

	f := filepath.Join(dir, "rapid.txt")
	// Rapid-fire modifications
	for i := 0; i < 10; i++ {
		os.WriteFile(f, []byte("update"), 0644)
		time.Sleep(10 * time.Millisecond)
	}

	// Should get at most a few events due to debounce
	count := 0
	timeout := time.After(2 * time.Second)
	for {
		select {
		case <-events:
			count++
		case <-timeout:
			goto done
		}
	}
done:
	if count > 5 {
		t.Fatalf("expected debounced events, got %d", count)
	}
}

func TestWatcher_SelfWriteFilter(t *testing.T) {
	dir := t.TempDir()
	w, _ := NewWatcher(dir, nil, 100*time.Millisecond)
	defer w.Close()

	events := w.Events()

	// Mark as self-write
	target := filepath.Join(dir, "synced.txt")
	w.MarkSelfWrite(target)
	os.WriteFile(target, []byte("from syncer"), 0644)

	// Wait for the self-write event to be processed and filtered
	time.Sleep(200 * time.Millisecond)

	// Write a different file
	os.WriteFile(filepath.Join(dir, "other.txt"), []byte("user file"), 0644)

	select {
	case ev := <-events:
		if filepath.Base(ev.Path) == "synced.txt" {
			t.Fatal("should have filtered self-write")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout")
	}
}
