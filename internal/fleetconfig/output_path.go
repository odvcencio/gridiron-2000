package fleetconfig

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
)

func (p *Publisher) prepareRoot(raw string) (string, error) {
	if strings.TrimSpace(raw) == "" {
		return "", errors.New("fleetconfig: output directory is required")
	}
	cleanInput := filepath.Clean(raw)
	if cleanInput == "." {
		return "", errors.New("fleetconfig: output directory may not be .")
	}
	root, err := filepath.Abs(raw)
	if err != nil {
		return "", fmt.Errorf("fleetconfig: resolve output directory: %w", err)
	}
	root = filepath.Clean(root)
	if root == string(filepath.Separator) {
		return "", errors.New("fleetconfig: output directory may not be the filesystem root")
	}
	if cwd, err := os.Getwd(); err == nil && root == filepath.Clean(cwd) {
		return "", errors.New("fleetconfig: output directory may not be the current directory")
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" && root == filepath.Clean(home) {
		return "", errors.New("fleetconfig: output directory may not be the user home")
	}
	if err := p.validateRootComponents(root); err != nil {
		return "", err
	}
	return root, nil
}

func (p *Publisher) validateRootComponents(root string) error {
	current := string(filepath.Separator)
	volume := filepath.VolumeName(root)
	if volume != "" {
		current = volume + string(filepath.Separator)
		root = strings.TrimPrefix(root, volume)
	}
	parts := strings.Split(strings.TrimPrefix(root, string(filepath.Separator)), string(filepath.Separator))
	for index, part := range parts {
		if part == "" {
			continue
		}
		current = filepath.Join(current, part)
		info, err := p.fs.Lstat(current)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) && index == len(parts)-1 {
				// The output root may be absent, but its parent must be a
				// real directory so publication never creates an implicit
				// hierarchy outside the explicit root.
				parent := filepath.Dir(current)
				parentInfo, parentErr := p.fs.Lstat(parent)
				if parentErr != nil {
					return fmt.Errorf("fleetconfig: output parent %q: %w", parent, parentErr)
				}
				if parentInfo.Mode()&os.ModeSymlink != 0 || !parentInfo.IsDir() {
					return fmt.Errorf("fleetconfig: output parent %q is not a directory", parent)
				}
				return nil
			}
			return fmt.Errorf("fleetconfig: inspect output path component %q: %w", current, err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("fleetconfig: output path component %q is a symlink", current)
		}
		if index == len(parts)-1 {
			if !info.IsDir() {
				return fmt.Errorf("fleetconfig: output %q is not a directory", root)
			}
			return nil
		}
		if !info.IsDir() {
			return fmt.Errorf("fleetconfig: output path component %q is not a directory", current)
		}
	}
	return errors.New("fleetconfig: output directory is invalid")
}

func (p *Publisher) snapshot(root string) (rootSnapshot, error) {
	parentPath := filepath.Dir(root)
	name := filepath.Base(root)
	parentInfo, err := p.fs.Lstat(parentPath)
	if err != nil {
		return rootSnapshot{}, fmt.Errorf("fleetconfig: inspect output parent %q: %w", parentPath, err)
	}
	if parentInfo.Mode()&os.ModeSymlink != 0 || !parentInfo.IsDir() {
		return rootSnapshot{}, fmt.Errorf("fleetconfig: output parent %q is not a directory", parentPath)
	}
	parent, err := p.fs.OpenDir(parentPath)
	if err != nil {
		return rootSnapshot{}, fmt.Errorf("fleetconfig: open output parent %q: %w", parentPath, err)
	}
	if err := verifyOpenedDirectory(parentPath, parentInfo, parent); err != nil {
		_ = parent.Close()
		return rootSnapshot{}, err
	}
	snapshot, snapshotErr := p.snapshotAt(parent, name, root)
	closeErr := parent.Close()
	if snapshotErr != nil {
		return rootSnapshot{}, snapshotErr
	}
	if closeErr != nil {
		return rootSnapshot{}, fmt.Errorf("fleetconfig: close output parent %q: %w", parentPath, closeErr)
	}
	return snapshot, nil
}

// snapshotAt inspects a root relative to an already-open parent directory.
// Holding the parent handle prevents a concurrent replacement of the parent
// path from redirecting the walk through a different tree.
func (p *Publisher) snapshotAt(parent publicationDir, name, display string) (rootSnapshot, error) {
	info, err := parent.Lstat(name)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return rootSnapshot{entries: map[string]publicationEntry{}}, nil
		}
		return rootSnapshot{}, fmt.Errorf("fleetconfig: inspect output %q: %w", display, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return rootSnapshot{}, fmt.Errorf("fleetconfig: output %q is a symlink", display)
	}
	if !info.IsDir() {
		return rootSnapshot{}, fmt.Errorf("fleetconfig: output %q is not a directory", display)
	}
	dir, err := parent.OpenDir(name)
	if err != nil {
		return rootSnapshot{}, fmt.Errorf("fleetconfig: read output directory %q: %w", display, err)
	}
	if err := verifyOpenedDirectory(display, info, dir); err != nil {
		_ = dir.Close()
		return rootSnapshot{}, err
	}
	entries := map[string]publicationEntry{}
	walkErr := p.walkDirectory(dir, "", entries)
	closeErr := dir.Close()
	if walkErr != nil {
		return rootSnapshot{}, walkErr
	}
	if closeErr != nil {
		return rootSnapshot{}, fmt.Errorf("fleetconfig: close output directory %q: %w", display, closeErr)
	}
	current, err := parent.Lstat(name)
	if err != nil {
		return rootSnapshot{}, fmt.Errorf("fleetconfig: inspect output %q after walk: %w", display, err)
	}
	if !os.SameFile(info, current) {
		return rootSnapshot{}, fmt.Errorf("fleetconfig: output %q changed during walk", display)
	}
	marker, ok := entries[OwnershipMarkerPath]
	owned := ok && marker.kind == entryFile && equalBytes(marker.data, []byte(OwnershipMarker))
	return rootSnapshot{exists: true, owned: owned, identity: info, entries: entries}, nil
}

func (p *Publisher) walkDirectory(dir publicationDir, relative string, entries map[string]publicationEntry) error {
	children, err := dir.ReadDir()
	if err != nil {
		return fmt.Errorf("fleetconfig: read output directory %q: %w", relative, err)
	}
	sort.Slice(children, func(i, j int) bool { return children[i].Name() < children[j].Name() })
	for _, child := range children {
		name := child.Name()
		if name == "" || name == "." || name == ".." || strings.Contains(name, "/") || strings.Contains(name, "\\") {
			return fmt.Errorf("fleetconfig: output contains an unsafe entry name %q", name)
		}
		rel := name
		if relative != "" {
			rel = filepath.ToSlash(filepath.Join(relative, name))
		}
		info, err := dir.Lstat(name)
		if err != nil {
			return fmt.Errorf("fleetconfig: inspect output entry %q: %w", rel, err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("fleetconfig: output entry %q is a symlink", rel)
		}
		if info.IsDir() {
			childDir, err := dir.OpenDir(name)
			if err != nil {
				return fmt.Errorf("fleetconfig: open output directory %q: %w", rel, err)
			}
			if err := verifyOpenedDirectory(rel, info, childDir); err != nil {
				_ = childDir.Close()
				return err
			}
			walkErr := p.walkDirectory(childDir, rel, entries)
			closeErr := childDir.Close()
			if walkErr != nil {
				return walkErr
			}
			if closeErr != nil {
				return fmt.Errorf("fleetconfig: close output directory %q: %w", rel, closeErr)
			}
			current, err := dir.Lstat(name)
			if err != nil {
				return fmt.Errorf("fleetconfig: inspect output entry %q after walk: %w", rel, err)
			}
			if !os.SameFile(info, current) {
				return fmt.Errorf("fleetconfig: output entry %q changed during walk", rel)
			}
			entries[rel] = publicationEntry{kind: entryDirectory}
			continue
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("fleetconfig: output entry %q is not a regular file or directory", rel)
		}
		file, err := dir.OpenFile(name, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
		if err != nil {
			return fmt.Errorf("fleetconfig: open output entry %q: %w", rel, err)
		}
		data, err := readOpenedFile(file, info)
		if err != nil {
			return fmt.Errorf("fleetconfig: read output entry %q: %w", rel, err)
		}
		current, err := dir.Lstat(name)
		if err != nil {
			return fmt.Errorf("fleetconfig: inspect output entry %q after read: %w", rel, err)
		}
		if !os.SameFile(info, current) {
			return fmt.Errorf("fleetconfig: output entry %q changed during read", rel)
		}
		entries[rel] = publicationEntry{kind: entryFile, data: data}
	}
	return nil
}

func readOpenedFile(file publicationFile, expected os.FileInfo) ([]byte, error) {
	info, statErr := file.Stat()
	if statErr != nil {
		_ = file.Close()
		return nil, statErr
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		_ = file.Close()
		return nil, errors.New("entry is not a regular file")
	}
	if expected == nil || !os.SameFile(expected, info) {
		_ = file.Close()
		return nil, errors.New("entry changed during open")
	}
	data, readErr := io.ReadAll(file)
	closeErr := file.Close()
	if readErr != nil {
		return nil, readErr
	}
	if closeErr != nil {
		return nil, closeErr
	}
	return data, nil
}
