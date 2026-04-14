package metadata

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

// ConflictType describes the kind of conflict between two operations.
type ConflictType int

const (
	ConflictEditEdit     ConflictType = iota
	ConflictEditDelete
	ConflictCreateCreate
)

// Conflict represents a detected conflict between two operations.
type Conflict struct {
	OpA  MetadataOperation
	OpB  MetadataOperation
	Type ConflictType
}

// Resolution describes how a conflict was resolved.
type Resolution struct {
	Applied      MetadataOperation
	ConflictCopy *string
}

// DetectConflicts finds conflicts between two sets of operations.
func DetectConflicts(opsA, opsB []MetadataOperation) []Conflict {
	// Index opsB by path
	pathB := make(map[string]MetadataOperation)
	for _, op := range opsB {
		pathB[op.Path] = op
	}

	var conflicts []Conflict
	for _, opA := range opsA {
		opB, overlap := pathB[opA.Path]
		if !overlap {
			continue
		}

		// Both delete same file → not a conflict
		if opA.Type == OpFileDelete && opB.Type == OpFileDelete {
			continue
		}

		// Both create same path
		if opA.Type == OpFileCreate && opB.Type == OpFileCreate {
			conflicts = append(conflicts, Conflict{OpA: opA, OpB: opB, Type: ConflictCreateCreate})
			continue
		}

		// Both update same file
		if opA.Type == OpFileUpdate && opB.Type == OpFileUpdate {
			conflicts = append(conflicts, Conflict{OpA: opA, OpB: opB, Type: ConflictEditEdit})
			continue
		}

		// One edits, one deletes
		if (opA.Type == OpFileUpdate && opB.Type == OpFileDelete) ||
			(opA.Type == OpFileDelete && opB.Type == OpFileUpdate) {
			conflicts = append(conflicts, Conflict{OpA: opA, OpB: opB, Type: ConflictEditDelete})
			continue
		}
	}

	return conflicts
}

// ResolveConflicts applies resolution strategy to detected conflicts.
func ResolveConflicts(conflicts []Conflict, store *Store) []Resolution {
	var resolutions []Resolution

	for _, c := range conflicts {
		switch c.Type {
		case ConflictEditEdit, ConflictCreateCreate:
			// Latest wins the path, other gets a conflict copy
			copyPath := ConflictCopyName(c.OpA.Path, c.OpB.DeviceID)
			resolutions = append(resolutions, Resolution{
				Applied:      c.OpA, // A wins the path
				ConflictCopy: &copyPath,
			})

		case ConflictEditDelete:
			// Edit wins (safer default)
			var editOp MetadataOperation
			if c.OpA.Type == OpFileUpdate {
				editOp = c.OpA
			} else {
				editOp = c.OpB
			}
			resolutions = append(resolutions, Resolution{
				Applied: editOp,
			})
		}
	}

	return resolutions
}

// ConflictCopyName generates a conflict copy filename.
func ConflictCopyName(originalPath, deviceID string) string {
	ext := filepath.Ext(originalPath)
	base := strings.TrimSuffix(originalPath, ext)
	date := time.Now().Format("2006-01-02")
	return fmt.Sprintf("%s (conflict from %s %s)%s", base, deviceID, date, ext)
}
