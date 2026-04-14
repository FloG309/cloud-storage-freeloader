package livesync

import (
	difflib "github.com/sergi/go-diff/diffmatchpatch"
)

// PatchType describes the kind of change.
type PatchType int

const (
	PatchInsert  PatchType = iota
	PatchDelete
	PatchReplace
)

// Patch represents a single diff operation.
type Patch struct {
	Type   PatchType
	Offset int
	Data   []byte
	Length int
}

// Diff computes patches to transform old into new.
func Diff(old, new []byte) []Patch {
	if bytesEqual(old, new) {
		return nil
	}

	// For text-safe content, use Myers diff
	if isTextSafe(old) && isTextSafe(new) {
		return textDiff(old, new)
	}

	// For binary content, use byte-level diff
	return byteDiff(old, new)
}

func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func isTextSafe(data []byte) bool {
	for _, b := range data {
		if b == 0 {
			return false
		}
	}
	return true
}

func textDiff(old, new []byte) []Patch {
	dmp := difflib.New()
	diffs := dmp.DiffMain(string(old), string(new), true)

	if len(diffs) == 1 && diffs[0].Type == difflib.DiffEqual {
		return nil
	}

	var patches []Patch
	offset := 0

	for _, d := range diffs {
		switch d.Type {
		case difflib.DiffEqual:
			offset += len(d.Text)
		case difflib.DiffInsert:
			patches = append(patches, Patch{
				Type:   PatchInsert,
				Offset: offset,
				Data:   []byte(d.Text),
			})
		case difflib.DiffDelete:
			patches = append(patches, Patch{
				Type:   PatchDelete,
				Offset: offset,
				Length: len(d.Text),
			})
			offset += len(d.Text)
		}
	}

	return patches
}

func byteDiff(old, new []byte) []Patch {
	// Find changed regions and produce minimal patches
	if len(old) == 0 {
		return []Patch{{Type: PatchInsert, Offset: 0, Data: new}}
	}
	if len(new) == 0 {
		return []Patch{{Type: PatchDelete, Offset: 0, Length: len(old)}}
	}

	// Find common prefix
	prefix := 0
	minLen := len(old)
	if len(new) < minLen {
		minLen = len(new)
	}
	for prefix < minLen && old[prefix] == new[prefix] {
		prefix++
	}

	// Find common suffix
	suffix := 0
	for suffix < minLen-prefix && old[len(old)-1-suffix] == new[len(new)-1-suffix] {
		suffix++
	}

	oldMid := old[prefix : len(old)-suffix]
	newMid := new[prefix : len(new)-suffix]

	if len(oldMid) == 0 && len(newMid) == 0 {
		return nil
	}

	if len(oldMid) > 0 && len(newMid) > 0 {
		return []Patch{{Type: PatchReplace, Offset: prefix, Length: len(oldMid), Data: append([]byte(nil), newMid...)}}
	}
	if len(oldMid) > 0 {
		return []Patch{{Type: PatchDelete, Offset: prefix, Length: len(oldMid)}}
	}
	return []Patch{{Type: PatchInsert, Offset: prefix, Data: append([]byte(nil), newMid...)}}
}

// ApplyPatches applies a series of patches to a base to produce the new version.
func ApplyPatches(base []byte, patches []Patch) []byte {
	// Rebuild using the diff approach: apply forward with offset adjustments
	result := append([]byte(nil), base...)

	adjustment := 0
	for _, p := range patches {
		pos := p.Offset + adjustment
		switch p.Type {
		case PatchInsert:
			tail := append([]byte(nil), result[pos:]...)
			result = append(result[:pos], p.Data...)
			result = append(result, tail...)
			adjustment += len(p.Data)
		case PatchDelete:
			result = append(result[:pos], result[pos+p.Length:]...)
			adjustment -= p.Length
		case PatchReplace:
			result = append(result[:pos], append(p.Data, result[pos+p.Length:]...)...)
			adjustment += len(p.Data) - p.Length
		}
	}

	return result
}
