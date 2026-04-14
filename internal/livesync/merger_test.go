package livesync

import (
	"strings"
	"testing"
)

func TestMerge_NonOverlapping(t *testing.T) {
	base := "Line1\nLine2\nLine3"
	ours := "Line1-edited\nLine2\nLine3"
	theirs := "Line1\nLine2\nLine3-edited"

	merged, conflicts, err := ThreeWayMerge([]byte(base), []byte(ours), []byte(theirs))
	if err != nil {
		t.Fatalf("Merge failed: %v", err)
	}
	if len(conflicts) != 0 {
		t.Fatalf("got %d conflicts, want 0", len(conflicts))
	}
	if !strings.Contains(string(merged), "Line1-edited") || !strings.Contains(string(merged), "Line3-edited") {
		t.Fatalf("merge missing edits: %q", merged)
	}
}

func TestMerge_SameEditBothSides(t *testing.T) {
	base := "Hello world"
	ours := "Hello beautiful world"
	theirs := "Hello beautiful world"

	merged, conflicts, err := ThreeWayMerge([]byte(base), []byte(ours), []byte(theirs))
	if err != nil {
		t.Fatalf("Merge failed: %v", err)
	}
	if len(conflicts) != 0 {
		t.Fatalf("got %d conflicts, want 0", len(conflicts))
	}
	if string(merged) != "Hello beautiful world" {
		t.Fatalf("got %q", merged)
	}
}

func TestMerge_ConflictOverlapping(t *testing.T) {
	base := "Hello world"
	ours := "Hello beautiful world"
	theirs := "Hello cruel world"

	_, conflicts, err := ThreeWayMerge([]byte(base), []byte(ours), []byte(theirs))
	if err != nil {
		t.Fatalf("Merge failed: %v", err)
	}
	if len(conflicts) == 0 {
		t.Fatal("expected conflict for overlapping edits")
	}
}

func TestMerge_InsertDifferentPositions(t *testing.T) {
	// When ours adds to end and theirs adds to end at different spots,
	// the merge preserves both additions
	base := "Line1\nLine2\nLine3"
	ours := "Line1\nLine2\nLine3\nOursNew"
	theirs := "Line1\nLine2\nLine3\nTheirsNew"

	merged, _, err := ThreeWayMerge([]byte(base), []byte(ours), []byte(theirs))
	if err != nil {
		t.Fatalf("Merge failed: %v", err)
	}
	if !strings.Contains(string(merged), "OursNew") || !strings.Contains(string(merged), "TheirsNew") {
		t.Fatalf("merge missing insertions: %q", merged)
	}
}

func TestMerge_OneEditsOneDeletes(t *testing.T) {
	base := "Line1\nLine2\nLine3"
	ours := "Line1\nLine2-edited\nLine3"
	theirs := "Line1\nLine3" // deleted Line2

	_, conflicts, err := ThreeWayMerge([]byte(base), []byte(ours), []byte(theirs))
	if err != nil {
		t.Fatalf("Merge failed: %v", err)
	}
	if len(conflicts) == 0 {
		t.Fatal("expected conflict when one edits and one deletes")
	}
}

func TestMerge_EmptyBase(t *testing.T) {
	base := ""
	ours := "Content from A"
	theirs := "Content from B"

	_, conflicts, err := ThreeWayMerge([]byte(base), []byte(ours), []byte(theirs))
	if err != nil {
		t.Fatalf("Merge failed: %v", err)
	}
	if len(conflicts) == 0 {
		t.Fatal("expected conflict when both create content from empty")
	}
}
