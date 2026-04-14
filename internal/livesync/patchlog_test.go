package livesync

import (
	"testing"
)

func newTestPatchLog(t *testing.T) *PatchLog {
	t.Helper()
	pl, err := NewPatchLog(":memory:")
	if err != nil {
		t.Fatalf("NewPatchLog: %v", err)
	}
	t.Cleanup(func() { pl.Close() })
	return pl
}

func TestPatchLog_Append(t *testing.T) {
	pl := newTestPatchLog(t)
	pl.Append("/notes.md", "desktop", []Patch{{Type: PatchInsert, Offset: 0, Data: []byte("a")}})
	pl.Append("/notes.md", "desktop", []Patch{{Type: PatchInsert, Offset: 1, Data: []byte("b")}})

	patches, _ := pl.PatchesSince("/notes.md", VersionVector{})
	if len(patches) != 2 {
		t.Fatalf("got %d patches, want 2", len(patches))
	}
}

func TestPatchLog_PerFileLog(t *testing.T) {
	pl := newTestPatchLog(t)
	pl.Append("/a.md", "dev1", []Patch{{Type: PatchInsert, Offset: 0, Data: []byte("a")}})
	pl.Append("/b.md", "dev1", []Patch{{Type: PatchInsert, Offset: 0, Data: []byte("b")}})

	patchesA, _ := pl.PatchesSince("/a.md", VersionVector{})
	patchesB, _ := pl.PatchesSince("/b.md", VersionVector{})

	if len(patchesA) != 1 || len(patchesB) != 1 {
		t.Fatalf("got A=%d B=%d, want 1,1", len(patchesA), len(patchesB))
	}
}

func TestPatchLog_VersionVector(t *testing.T) {
	pl := newTestPatchLog(t)
	for i := 0; i < 5; i++ {
		pl.Append("/notes.md", "desktop", []Patch{{Type: PatchInsert, Offset: 0, Data: []byte("x")}})
	}

	vv := pl.GetVersionVector()
	if vv["desktop"] != 5 {
		t.Fatalf("got desktop=%d, want 5", vv["desktop"])
	}
}

func TestPatchLog_MergeVectors(t *testing.T) {
	a := VersionVector{"desktop": 5, "laptop": 3}
	b := VersionVector{"desktop": 3, "laptop": 7, "phone": 1}

	merged := MergeVersionVectors(a, b)
	if merged["desktop"] != 5 || merged["laptop"] != 7 || merged["phone"] != 1 {
		t.Fatalf("merge failed: %v", merged)
	}
}

func TestPatchLog_PatchesSince(t *testing.T) {
	pl := newTestPatchLog(t)
	for i := 0; i < 5; i++ {
		pl.Append("/notes.md", "desktop", []Patch{{Type: PatchInsert, Offset: 0, Data: []byte("x")}})
	}

	// Get only patches since seq 3
	patches, _ := pl.PatchesSince("/notes.md", VersionVector{"desktop": 3})
	if len(patches) != 2 {
		t.Fatalf("got %d patches, want 2", len(patches))
	}
}

func TestPatchLog_GarbageCollect(t *testing.T) {
	pl := newTestPatchLog(t)
	for i := 0; i < 10; i++ {
		pl.Append("/notes.md", "desktop", []Patch{{Type: PatchInsert, Offset: 0, Data: []byte("x")}})
	}

	pl.GarbageCollect("/notes.md", VersionVector{"desktop": 5})

	patches, _ := pl.PatchesSince("/notes.md", VersionVector{})
	if len(patches) != 5 {
		t.Fatalf("got %d patches after GC, want 5", len(patches))
	}
}
