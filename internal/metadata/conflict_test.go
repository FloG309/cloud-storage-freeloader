package metadata

import (
	"strings"
	"testing"
)

func makeOp(deviceID string, opType OpType, path string) MetadataOperation {
	return MetadataOperation{
		OpID:     "op-" + deviceID + "-" + path,
		DeviceID: deviceID,
		Type:     opType,
		Path:     path,
	}
}

func TestConflict_NoConflict(t *testing.T) {
	opsA := []MetadataOperation{makeOp("A", OpFileUpdate, "/x.txt")}
	opsB := []MetadataOperation{makeOp("B", OpFileUpdate, "/y.txt")}

	conflicts := DetectConflicts(opsA, opsB)
	if len(conflicts) != 0 {
		t.Fatalf("got %d conflicts, want 0", len(conflicts))
	}
}

func TestConflict_EditEdit(t *testing.T) {
	opsA := []MetadataOperation{makeOp("A", OpFileUpdate, "/shared.txt")}
	opsB := []MetadataOperation{makeOp("B", OpFileUpdate, "/shared.txt")}

	conflicts := DetectConflicts(opsA, opsB)
	if len(conflicts) != 1 {
		t.Fatalf("got %d conflicts, want 1", len(conflicts))
	}
	if conflicts[0].Type != ConflictEditEdit {
		t.Fatalf("got conflict type %v, want EditEdit", conflicts[0].Type)
	}
}

func TestConflict_EditDelete(t *testing.T) {
	opsA := []MetadataOperation{makeOp("A", OpFileUpdate, "/shared.txt")}
	opsB := []MetadataOperation{makeOp("B", OpFileDelete, "/shared.txt")}

	conflicts := DetectConflicts(opsA, opsB)
	if len(conflicts) != 1 {
		t.Fatalf("got %d conflicts, want 1", len(conflicts))
	}
	if conflicts[0].Type != ConflictEditDelete {
		t.Fatalf("got conflict type %v, want EditDelete", conflicts[0].Type)
	}
}

func TestConflict_DeleteDelete(t *testing.T) {
	opsA := []MetadataOperation{makeOp("A", OpFileDelete, "/shared.txt")}
	opsB := []MetadataOperation{makeOp("B", OpFileDelete, "/shared.txt")}

	conflicts := DetectConflicts(opsA, opsB)
	if len(conflicts) != 0 {
		t.Fatalf("got %d conflicts, want 0 (both delete is not a conflict)", len(conflicts))
	}
}

func TestConflict_CreateCreate(t *testing.T) {
	opsA := []MetadataOperation{makeOp("A", OpFileCreate, "/shared.txt")}
	opsB := []MetadataOperation{makeOp("B", OpFileCreate, "/shared.txt")}

	conflicts := DetectConflicts(opsA, opsB)
	if len(conflicts) != 1 {
		t.Fatalf("got %d conflicts, want 1", len(conflicts))
	}
	if conflicts[0].Type != ConflictCreateCreate {
		t.Fatalf("got conflict type %v, want CreateCreate", conflicts[0].Type)
	}
}

func TestConflict_ConflictCopyNaming(t *testing.T) {
	name := ConflictCopyName("/docs/notes.txt", "DeviceB")
	if !strings.Contains(name, "conflict from DeviceB") {
		t.Fatalf("unexpected conflict copy name: %s", name)
	}
	if !strings.HasSuffix(name, ".txt") {
		t.Fatalf("conflict copy should preserve extension: %s", name)
	}
}

func TestConflict_ResolveEditEdit(t *testing.T) {
	s := newTestStore(t)
	s.CreateFile(&FileMeta{Path: "/shared.txt", Size: 100})

	conflicts := []Conflict{{
		OpA:  makeOp("A", OpFileUpdate, "/shared.txt"),
		OpB:  makeOp("B", OpFileUpdate, "/shared.txt"),
		Type: ConflictEditEdit,
	}}

	resolutions := ResolveConflicts(conflicts, s)
	if len(resolutions) != 1 {
		t.Fatalf("got %d resolutions, want 1", len(resolutions))
	}
	if resolutions[0].ConflictCopy == nil {
		t.Fatal("expected conflict copy path")
	}
}
