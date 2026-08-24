package fleetconfig

// Shared publication types and expected-file construction. Path confinement,
// transaction swapping, and drift comparison live in their focused files.

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// OwnershipMarkerPath is the fixed root-relative file that permits fleetgen
// to replace a non-empty output tree.
const OwnershipMarkerPath = ".fleetgen-owner"

// OwnershipMarker is written byte-for-byte into every rendered bundle. It is
// intentionally boring and contains no input-derived values.
const OwnershipMarker = "fleetgen ownership marker v1\n"

// Publisher performs safe, object-scoped publication and read-only checks.
// The zero value is not ready for use; construct one with NewPublisher.
//
// The filesystem implementation is held on the Publisher rather than in
// package-global hooks. This keeps tests able to inject a rename failure
// without changing behavior in another concurrent Publisher.
type Publisher struct {
	fs    publicationFS
	nonce func() string
}

// CommittedPublicationError means the new output tree was swapped into place,
// but cleanup of the old owned backup failed. The old tree is deliberately not
// restored: recursive cleanup may already have removed part of that backup.
// Callers should surface the artifact path and let the next invocation refuse
// the interrupted transaction until an operator inspects it.
type CommittedPublicationError struct {
	Artifact string
	Err      error
}

func (e *CommittedPublicationError) Error() string {
	if e == nil {
		return "fleetconfig: publication committed with an uncleared transaction artifact"
	}
	return fmt.Sprintf("fleetconfig: publication committed; transaction artifact %q requires inspection: %v", e.Artifact, e.Err)
}

func (e *CommittedPublicationError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// NewPublisher returns a Publisher backed by the host filesystem.
func NewPublisher() *Publisher {
	return &Publisher{
		fs:    osPublicationFS{},
		nonce: func() string { return strconv.FormatInt(time.Now().UnixNano(), 10) },
	}
}

// ExpectedFiles returns the complete deterministic publication, including the
// fixed ownership marker. It validates every generated relative path before
// returning a copy of the file bytes.
func ExpectedFiles(bundle Bundle) ([]File, error) {
	return expectedFiles(bundle)
}

type publicationEntryKind uint8

const (
	entryFile publicationEntryKind = iota + 1
	entryDirectory
)

type publicationEntry struct {
	kind publicationEntryKind
	data []byte
}

type rootSnapshot struct {
	exists   bool
	owned    bool
	identity os.FileInfo
	entries  map[string]publicationEntry
}

func expectedFiles(bundle Bundle) ([]File, error) {
	files := make([]File, 0, len(bundle.Files)+1)
	files = append(files, File{Path: OwnershipMarkerPath, Data: []byte(OwnershipMarker)})
	seen := map[string]struct{}{OwnershipMarkerPath: {}}
	for _, file := range bundle.Files {
		if err := validateGeneratedPath(file.Path); err != nil {
			return nil, fmt.Errorf("fleetconfig: generated file %q: %w", file.Path, err)
		}
		if _, ok := seen[file.Path]; ok {
			return nil, fmt.Errorf("fleetconfig: duplicate generated file path %q", file.Path)
		}
		seen[file.Path] = struct{}{}
		files = append(files, File{Path: file.Path, Data: append([]byte(nil), file.Data...)})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	if err := validateExpectedShape(files); err != nil {
		return nil, err
	}
	return files, nil
}

func validateGeneratedPath(path string) error {
	if path == "" || filepath.IsAbs(path) || filepath.VolumeName(path) != "" {
		return errors.New("path must be a clean relative path")
	}
	if filepath.Clean(path) != path || strings.Contains(path, "\\") {
		return errors.New("path must be a clean relative path")
	}
	parts := strings.Split(path, "/")
	for _, part := range parts {
		if part == "" || part == "." || part == ".." {
			return errors.New("path must be a clean relative path")
		}
		for _, r := range part {
			if r < 0x20 || r == 0x7f || r == 0 {
				return errors.New("path contains an unsafe character")
			}
		}
	}
	return nil
}

func validateExpectedShape(files []File) error {
	expected := map[string]publicationEntryKind{}
	for _, file := range files {
		if previous, ok := expected[file.Path]; ok && previous != entryFile {
			return fmt.Errorf("fleetconfig: generated path %q conflicts with a directory", file.Path)
		}
		expected[file.Path] = entryFile
		for parent := filepath.Dir(file.Path); parent != "."; parent = filepath.Dir(parent) {
			parent = filepath.ToSlash(parent)
			if previous, ok := expected[parent]; ok && previous == entryFile {
				return fmt.Errorf("fleetconfig: generated path %q is both a file and a parent directory", parent)
			}
			expected[parent] = entryDirectory
		}
	}
	return nil
}
