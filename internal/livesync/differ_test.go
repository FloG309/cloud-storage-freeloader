package livesync

import (
	"bytes"
	"crypto/rand"
	"testing"
)

func TestDiff_Insert(t *testing.T) {
	old := []byte("Hello world")
	new := []byte("Hello beautiful world")

	patches := Diff(old, new)
	if len(patches) == 0 {
		t.Fatal("expected patches for insertion")
	}

	result := ApplyPatches(old, patches)
	if !bytes.Equal(result, new) {
		t.Fatalf("apply failed: got %q, want %q", result, new)
	}
}

func TestDiff_Delete(t *testing.T) {
	old := []byte("Hello beautiful world")
	new := []byte("Hello world")

	patches := Diff(old, new)
	if len(patches) == 0 {
		t.Fatal("expected patches for deletion")
	}

	result := ApplyPatches(old, patches)
	if !bytes.Equal(result, new) {
		t.Fatalf("apply failed: got %q, want %q", result, new)
	}
}

func TestDiff_Replace(t *testing.T) {
	old := []byte("Hello world")
	new := []byte("Hello earth")

	patches := Diff(old, new)
	result := ApplyPatches(old, patches)
	if !bytes.Equal(result, new) {
		t.Fatalf("apply failed: got %q, want %q", result, new)
	}
}

func TestDiff_NoChange(t *testing.T) {
	data := []byte("unchanged content")
	patches := Diff(data, data)
	if len(patches) != 0 {
		t.Fatalf("got %d patches, want 0", len(patches))
	}
}

func TestDiff_NewFile(t *testing.T) {
	patches := Diff(nil, []byte("new file content"))
	if len(patches) == 0 {
		t.Fatal("expected patch for new file")
	}
	result := ApplyPatches(nil, patches)
	if !bytes.Equal(result, []byte("new file content")) {
		t.Fatalf("got %q", result)
	}
}

func TestDiff_DeletedFile(t *testing.T) {
	patches := Diff([]byte("old content"), nil)
	if len(patches) == 0 {
		t.Fatal("expected patch for deleted file")
	}
	result := ApplyPatches([]byte("old content"), patches)
	if len(result) != 0 {
		t.Fatalf("expected empty result, got %q", result)
	}
}

func TestDiff_BinaryFile(t *testing.T) {
	old := make([]byte, 256)
	rand.Read(old)
	old[0] = 0 // ensure it's treated as binary (contains NUL)
	new := make([]byte, 256)
	copy(new, old)
	new[100] = old[100] ^ 0xFF // flip one byte

	patches := Diff(old, new)
	result := ApplyPatches(old, patches)
	if !bytes.Equal(result, new) {
		t.Fatal("binary diff roundtrip failed")
	}
}

func TestDiff_LargeFile(t *testing.T) {
	old := make([]byte, 1024*1024)
	rand.Read(old)
	old[0] = 0 // ensure binary path
	new := make([]byte, len(old))
	copy(new, old)
	// Small change in the middle
	copy(new[500000:], []byte("CHANGED!!"))

	patches := Diff(old, new)
	result := ApplyPatches(old, patches)
	if !bytes.Equal(result, new) {
		t.Fatal("large file diff roundtrip failed")
	}
}

func TestDiff_ApplyPatch(t *testing.T) {
	old := []byte("The quick brown fox jumps over the lazy dog")
	new := []byte("The quick red fox leaps over the lazy cat")

	patches := Diff(old, new)
	result := ApplyPatches(old, patches)
	if !bytes.Equal(result, new) {
		t.Fatalf("got %q, want %q", result, new)
	}
}
