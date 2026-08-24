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
	info, err := p.fs.Lstat(root)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return rootSnapshot{entries: map[string]publicationEntry{}}, nil
		}
		return rootSnapshot{}, fmt.Errorf("fleetconfig: inspect output %q: %w", root, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return rootSnapshot{}, fmt.Errorf("fleetconfig: output %q is a symlink", root)
	}
	if !info.IsDir() {
		return rootSnapshot{}, fmt.Errorf("fleetconfig: output %q is not a directory", root)
	}
	entries := map[string]publicationEntry{}
	if err := p.walkTree(root, "", entries); err != nil {
		return rootSnapshot{}, err
	}
	marker, ok := entries[OwnershipMarkerPath]
	owned := ok && marker.kind == entryFile && equalBytes(marker.data, []byte(OwnershipMarker))
	return rootSnapshot{exists: true, owned: owned, entries: entries}, nil
}

func (p *Publisher) walkTree(directory, relative string, entries map[string]publicationEntry) error {
	children, err := p.fs.ReadDir(directory)
	if err != nil {
		return fmt.Errorf("fleetconfig: read output directory %q: %w", directory, err)
	}
	sort.Slice(children, func(i, j int) bool { return children[i].Name() < children[j].Name() })
	for _, child := range children {
		name := child.Name()
		if name == "" || name == "." || name == ".." || strings.Contains(name, "/") || strings.Contains(name, "\\") {
			return fmt.Errorf("fleetconfig: output contains an unsafe entry name %q", name)
		}
		path := filepath.Join(directory, name)
		rel := name
		if relative != "" {
			rel = filepath.ToSlash(filepath.Join(relative, name))
		}
		info, err := p.fs.Lstat(path)
		if err != nil {
			return fmt.Errorf("fleetconfig: inspect output entry %q: %w", rel, err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("fleetconfig: output entry %q is a symlink", rel)
		}
		if info.IsDir() {
			entries[rel] = publicationEntry{kind: entryDirectory}
			if err := p.walkTree(path, rel, entries); err != nil {
				return err
			}
			continue
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("fleetconfig: output entry %q is not a regular file or directory", rel)
		}
		data, err := p.readFile(path)
		if err != nil {
			return fmt.Errorf("fleetconfig: read output entry %q: %w", rel, err)
		}
		entries[rel] = publicationEntry{kind: entryFile, data: data}
	}
	return nil
}

func (p *Publisher) readFile(path string) ([]byte, error) {
	file, err := p.fs.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	info, statErr := file.Stat()
	if statErr != nil {
		_ = file.Close()
		return nil, statErr
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		_ = file.Close()
		return nil, errors.New("entry is not a regular file")
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
