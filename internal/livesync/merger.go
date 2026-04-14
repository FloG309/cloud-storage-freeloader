package livesync

import (
	"strings"
)

// MergeConflict describes a conflicting region.
type MergeConflict struct {
	BaseContent   string
	OursContent   string
	TheirsContent string
	LineRange     [2]int
}

// ThreeWayMerge performs a line-level three-way merge.
func ThreeWayMerge(base, ours, theirs []byte) (merged []byte, conflicts []MergeConflict, err error) {
	baseLines := splitLines(string(base))
	oursLines := splitLines(string(ours))
	theirsLines := splitLines(string(theirs))

	// If ours == theirs, no conflict
	if strings.Join(oursLines, "\n") == strings.Join(theirsLines, "\n") {
		return ours, nil, nil
	}

	// Compute diffs from base
	oursDiff := lineDiff(baseLines, oursLines)
	theirsDiff := lineDiff(baseLines, theirsLines)

	// Check for overlapping changes
	var result []string
	maxLen := len(baseLines)
	if len(oursLines) > maxLen {
		maxLen = len(oursLines)
	}
	if len(theirsLines) > maxLen {
		maxLen = len(theirsLines)
	}

	// Simple approach: check each base line
	i := 0
	for i < len(baseLines) || i < maxLen {
		oursChanged := lineChanged(oursDiff, i)
		theirsChanged := lineChanged(theirsDiff, i)

		if oursChanged && theirsChanged {
			// Both changed the same region — conflict
			oursLine := getLine(oursLines, i)
			theirsLine := getLine(theirsLines, i)
			baseLine := getLine(baseLines, i)

			conflicts = append(conflicts, MergeConflict{
				BaseContent:   baseLine,
				OursContent:   oursLine,
				TheirsContent: theirsLine,
				LineRange:     [2]int{i, i + 1},
			})
			// Take ours as default
			if oursLine != "" {
				result = append(result, oursLine)
			}
		} else if oursChanged {
			oursLine := getLine(oursLines, i)
			if oursLine != "" || i < len(oursLines) {
				result = append(result, oursLine)
			}
		} else if theirsChanged {
			theirsLine := getLine(theirsLines, i)
			if theirsLine != "" || i < len(theirsLines) {
				result = append(result, theirsLine)
			}
		} else {
			if i < len(baseLines) {
				result = append(result, baseLines[i])
			}
		}
		i++
		if i >= len(baseLines) && i >= len(oursLines) && i >= len(theirsLines) {
			break
		}
	}

	// Handle insertions beyond base length
	for i := len(baseLines); i < len(oursLines); i++ {
		if !containsLine(result, oursLines[i]) {
			result = append(result, oursLines[i])
		}
	}
	for i := len(baseLines); i < len(theirsLines); i++ {
		if !containsLine(result, theirsLines[i]) {
			result = append(result, theirsLines[i])
		}
	}

	return []byte(strings.Join(result, "\n")), conflicts, nil
}

func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

type lineChange struct {
	index   int
	changed bool
}

func lineDiff(base, modified []string) map[int]bool {
	changes := make(map[int]bool)
	maxLen := len(base)
	if len(modified) > maxLen {
		maxLen = len(modified)
	}
	for i := 0; i < maxLen; i++ {
		baseLine := getLine(base, i)
		modLine := getLine(modified, i)
		if baseLine != modLine {
			changes[i] = true
		}
	}
	return changes
}

func lineChanged(diff map[int]bool, i int) bool {
	return diff[i]
}

func getLine(lines []string, i int) string {
	if i < len(lines) {
		return lines[i]
	}
	return ""
}

func containsLine(lines []string, line string) bool {
	for _, l := range lines {
		if l == line {
			return true
		}
	}
	return false
}
