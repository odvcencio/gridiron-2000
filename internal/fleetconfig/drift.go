package fleetconfig

import (
	"path/filepath"
	"sort"
	"strings"
)

// Drift describes a comparison between the expected publication and the
// current output tree. Added means an expected path is missing; removed means
// an unexpected path exists.
type Drift struct {
	Added   []string
	Changed []string
	Removed []string
}

// Clean reports whether the current output is byte-for-byte equivalent to the
// expected owned publication.
func (d Drift) Clean() bool {
	return len(d.Added) == 0 && len(d.Changed) == 0 && len(d.Removed) == 0
}

// IsClean is a descriptive alias for Clean.
func (d Drift) IsClean() bool { return d.Clean() }

// String returns stable operator-facing drift output. Paths are sorted by the
// comparison operation; sorting again here keeps a manually constructed Drift
// stable as well.
func (d Drift) String() string {
	added := sortedCopy(d.Added)
	changed := sortedCopy(d.Changed)
	removed := sortedCopy(d.Removed)
	var b strings.Builder
	b.WriteString("fleetgen check: drift\n")
	writeDriftPaths(&b, "added (expected missing)", added)
	writeDriftPaths(&b, "changed", changed)
	writeDriftPaths(&b, "removed (unexpected existing)", removed)
	return b.String()
}

func writeDriftPaths(b *strings.Builder, label string, paths []string) {
	if len(paths) == 0 {
		return
	}
	b.WriteString(label)
	b.WriteString(":\n")
	for _, path := range paths {
		b.WriteString("  ")
		b.WriteString(path)
		b.WriteByte('\n')
	}
}
func comparePublication(expected []File, snapshot rootSnapshot) Drift {
	expectedFilesByPath := make(map[string][]byte, len(expected))
	expectedPaths := make(map[string]publicationEntryKind, len(expected))
	for _, file := range expected {
		expectedFilesByPath[file.Path] = file.Data
		expectedPaths[file.Path] = entryFile
		for parent := filepath.ToSlash(filepath.Dir(file.Path)); parent != "."; parent = filepath.ToSlash(filepath.Dir(parent)) {
			expectedPaths[parent] = entryDirectory
		}
	}
	var drift Drift
	for path, kind := range expectedPaths {
		actual, ok := snapshot.entries[path]
		if !ok {
			// Parent directories are structural; their absence is already
			// represented by the missing generated files below.
			if kind == entryFile {
				drift.Added = append(drift.Added, path)
			}
			continue
		}
		if actual.kind != kind {
			drift.Changed = append(drift.Changed, path)
			continue
		}
		if kind == entryFile && !equalBytes(actual.data, expectedFilesByPath[path]) {
			drift.Changed = append(drift.Changed, path)
		}
	}
	for path := range snapshot.entries {
		if _, ok := expectedPaths[path]; !ok {
			drift.Removed = append(drift.Removed, path)
		}
	}
	sort.Strings(drift.Added)
	sort.Strings(drift.Changed)
	sort.Strings(drift.Removed)
	return drift
}
func equalBytes(left, right []byte) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func sortedCopy(values []string) []string {
	copyValues := append([]string(nil), values...)
	sort.Strings(copyValues)
	return copyValues
}
