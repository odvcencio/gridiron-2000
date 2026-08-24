package fleetconfig

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// Publish atomically renders a compiled bundle into out.
func (p *Publisher) Publish(bundle Bundle, out string) error {
	if p == nil || p.fs == nil {
		return errors.New("fleetconfig: nil publisher")
	}
	expected, err := expectedFiles(bundle)
	if err != nil {
		return err
	}
	root, err := p.prepareRoot(out)
	if err != nil {
		return err
	}
	parent := filepath.Dir(root)
	base := filepath.Base(root)
	if err := p.rejectLeftovers(parent, base); err != nil {
		return err
	}
	snapshot, err := p.snapshot(root)
	if err != nil {
		return err
	}
	if snapshot.exists && len(snapshot.entries) > 0 && !snapshot.owned {
		return fmt.Errorf("fleetconfig: output %q is non-empty and is not a fleetgen-owned tree", out)
	}

	stage, err := p.reserveSibling(parent, base, "stage")
	if err != nil {
		return err
	}
	backup, err := p.reserveSibling(parent, base, "backup")
	if err != nil {
		return err
	}

	stageCreated := false
	cleanupStage := func() error {
		if !stageCreated {
			return nil
		}
		return p.removeTree(stage)
	}
	if err := p.fs.Mkdir(stage, 0o700); err != nil {
		return fmt.Errorf("fleetconfig: create private staging directory: %w", err)
	}
	stageCreated = true
	if info, err := p.fs.Lstat(stage); err != nil {
		_ = cleanupStage()
		return fmt.Errorf("fleetconfig: inspect staging directory: %w", err)
	} else if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		_ = cleanupStage()
		return errors.New("fleetconfig: staging path is not a directory")
	}
	if err := p.writeStage(stage, expected); err != nil {
		cleanupErr := cleanupStage()
		return joinPublicationErrors(err, cleanupErr)
	}

	movedOld := false
	if snapshot.exists {
		// Recheck the root after staging. A newly appeared unowned tree must
		// never be replaced just because the earlier preflight saw absence.
		current, err := p.snapshot(root)
		if err != nil {
			cleanupErr := cleanupStage()
			return joinPublicationErrors(err, cleanupErr)
		}
		if !sameRootState(snapshot, current) {
			cleanupErr := cleanupStage()
			return joinPublicationErrors(errors.New("fleetconfig: output changed during staging; refusing publication"), cleanupErr)
		}
		if err := p.fs.Rename(root, backup); err != nil {
			cleanupErr := cleanupStage()
			return joinPublicationErrors(fmt.Errorf("fleetconfig: move existing output to backup: %w", err), cleanupErr)
		}
		movedOld = true
	} else {
		// Avoid replacing a path that appeared after the absent-root
		// preflight. Rename itself does not have a portable no-replace mode.
		current, err := p.snapshot(root)
		if err != nil {
			cleanupErr := cleanupStage()
			return joinPublicationErrors(err, cleanupErr)
		}
		if current.exists {
			cleanupErr := cleanupStage()
			return joinPublicationErrors(errors.New("fleetconfig: output appeared during staging; refusing publication"), cleanupErr)
		}
	}

	if err := p.fs.Rename(stage, root); err != nil {
		var rollbackErr error
		if movedOld {
			rollbackErr = p.fs.Rename(backup, root)
			movedOld = false
		}
		cleanupErr := cleanupStage()
		return joinPublicationErrors(
			fmt.Errorf("fleetconfig: publish staged output: %w", err),
			joinPublicationErrors(rollbackErr, cleanupErr),
		)
	}
	stageCreated = false

	if movedOld {
		if err := p.removeTree(backup); err != nil {
			// The swap is committed. Recursive cleanup may have already
			// removed part of backup, so attempting rollback here could
			// silently lose bytes from the old publication.
			return &CommittedPublicationError{Artifact: backup, Err: err}
		}
	}
	return nil
}

// Render is a descriptive alias for Publisher.Publish.
func (p *Publisher) Render(bundle Bundle, out string) error {
	return p.Publish(bundle, out)
}

// Publish renders a compiled bundle with a new host-filesystem Publisher.
func Publish(bundle Bundle, out string) error {
	return NewPublisher().Publish(bundle, out)
}

// Render renders a compiled bundle with a new host-filesystem Publisher.
func Render(bundle Bundle, out string) error {
	return Publish(bundle, out)
}

// Check compares a compiled bundle with out using only lstat, directory reads,
// and file reads. It never creates, chmods, writes, renames, or removes.
func (p *Publisher) Check(bundle Bundle, out string) (Drift, error) {
	if p == nil || p.fs == nil {
		return Drift{}, errors.New("fleetconfig: nil publisher")
	}
	expected, err := expectedFiles(bundle)
	if err != nil {
		return Drift{}, err
	}
	root, err := p.prepareRoot(out)
	if err != nil {
		return Drift{}, err
	}
	parent := filepath.Dir(root)
	base := filepath.Base(root)
	if err := p.rejectLeftovers(parent, base); err != nil {
		return Drift{}, err
	}
	snapshot, err := p.snapshot(root)
	if err != nil {
		return Drift{}, err
	}
	if snapshot.exists && len(snapshot.entries) > 0 && !snapshot.owned {
		return Drift{}, fmt.Errorf("fleetconfig: output %q is non-empty and is not a fleetgen-owned tree", out)
	}
	return comparePublication(expected, snapshot), nil
}

// Check compares a compiled bundle with out using a host-filesystem
// Publisher.
func Check(bundle Bundle, out string) (Drift, error) {
	return NewPublisher().Check(bundle, out)
}

func (p *Publisher) writeStage(stage string, files []File) error {
	for _, file := range files {
		target := filepath.Join(stage, filepath.FromSlash(file.Path))
		if err := p.makeStageParents(stage, filepath.Dir(filepath.FromSlash(file.Path))); err != nil {
			return err
		}
		handle, err := p.fs.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL|syscall.O_NOFOLLOW, 0o600)
		if err != nil {
			return fmt.Errorf("fleetconfig: stage %q: %w", file.Path, err)
		}
		writeErr := writeAll(handle, file.Data)
		syncErr := error(nil)
		if writeErr == nil {
			syncErr = handle.Sync()
		}
		closeErr := handle.Close()
		if writeErr != nil {
			return fmt.Errorf("fleetconfig: stage %q: write: %w", file.Path, writeErr)
		}
		if syncErr != nil {
			return fmt.Errorf("fleetconfig: stage %q: sync: %w", file.Path, syncErr)
		}
		if closeErr != nil {
			return fmt.Errorf("fleetconfig: stage %q: close: %w", file.Path, closeErr)
		}
	}
	return nil
}

func writeAll(file publicationFile, data []byte) error {
	for len(data) > 0 {
		n, err := file.Write(data)
		if n > 0 {
			data = data[n:]
		}
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrShortWrite
		}
	}
	return nil
}

func (p *Publisher) makeStageParents(stage, relative string) error {
	if relative == "." || relative == "" {
		return nil
	}
	parts := strings.Split(filepath.ToSlash(relative), "/")
	current := stage
	for _, part := range parts {
		if part == "" || part == "." || part == ".." {
			return errors.New("fleetconfig: unsafe generated parent path")
		}
		current = filepath.Join(current, part)
		info, err := p.fs.Lstat(current)
		if err == nil {
			if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
				return fmt.Errorf("fleetconfig: staging parent %q is not a directory", current)
			}
			continue
		}
		if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("fleetconfig: inspect staging parent %q: %w", current, err)
		}
		if err := p.fs.Mkdir(current, 0o700); err != nil {
			return fmt.Errorf("fleetconfig: create staging parent %q: %w", current, err)
		}
		info, err = p.fs.Lstat(current)
		if err != nil {
			return fmt.Errorf("fleetconfig: inspect staging parent %q: %w", current, err)
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return fmt.Errorf("fleetconfig: staging parent %q is not a directory", current)
		}
	}
	return nil
}

func (p *Publisher) rejectLeftovers(parent, base string) error {
	entries, err := p.fs.ReadDir(parent)
	if err != nil {
		return fmt.Errorf("fleetconfig: inspect output siblings: %w", err)
	}
	stagePrefix := "." + base + ".fleetgen-stage-"
	backupPrefix := "." + base + ".fleetgen-backup-"
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasPrefix(name, stagePrefix) || strings.HasPrefix(name, backupPrefix) {
			return fmt.Errorf("fleetconfig: interrupted fleetgen transaction artifact %q exists; remove it only after inspection", filepath.Join(parent, name))
		}
	}
	return nil
}

func (p *Publisher) reserveSibling(parent, base, kind string) (string, error) {
	prefix := "." + base + ".fleetgen-" + kind + "-"
	for attempt := 0; attempt < 100; attempt++ {
		suffix := ""
		if p.nonce != nil {
			suffix = p.nonce()
		}
		if suffix == "" {
			suffix = "transaction"
		}
		if attempt > 0 {
			suffix += "-" + strconv.Itoa(attempt)
		}
		candidate := filepath.Join(parent, prefix+suffix)
		_, err := p.fs.Lstat(candidate)
		if errors.Is(err, os.ErrNotExist) {
			return candidate, nil
		}
		if err != nil {
			return "", fmt.Errorf("fleetconfig: inspect transaction sibling %q: %w", candidate, err)
		}
	}
	return "", errors.New("fleetconfig: could not reserve a transaction sibling")
}

func (p *Publisher) removeTree(path string) error {
	info, err := p.fs.Lstat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return p.fs.Remove(path)
	}
	children, err := p.fs.ReadDir(path)
	if err != nil {
		return err
	}
	for _, child := range children {
		if child.Name() == "" || child.Name() == "." || child.Name() == ".." {
			return errors.New("fleetconfig: unsafe transaction entry name")
		}
		if err := p.removeTree(filepath.Join(path, child.Name())); err != nil {
			return err
		}
	}
	return p.fs.Remove(path)
}

func sameRootState(left, right rootSnapshot) bool {
	if left.exists != right.exists || left.owned != right.owned {
		return false
	}
	// The snapshot bytes are intentionally compared here. If an operator
	// edits a file while staging, publication must not replace that edit.
	if len(left.entries) != len(right.entries) {
		return false
	}
	for path, leftEntry := range left.entries {
		rightEntry, ok := right.entries[path]
		if !ok || leftEntry.kind != rightEntry.kind || !equalBytes(leftEntry.data, rightEntry.data) {
			return false
		}
	}
	return true
}

func joinPublicationErrors(left, right error) error {
	if left == nil {
		return right
	}
	if right == nil {
		return left
	}
	return errors.Join(left, right)
}

type publicationFile interface {
	io.Reader
	io.Writer
	Stat() (os.FileInfo, error)
	Sync() error
	Close() error
}

type publicationFS interface {
	Lstat(string) (os.FileInfo, error)
	ReadDir(string) ([]os.DirEntry, error)
	OpenFile(string, int, os.FileMode) (publicationFile, error)
	Mkdir(string, os.FileMode) error
	Rename(string, string) error
	Remove(string) error
}

type osPublicationFS struct{}

func (osPublicationFS) Lstat(path string) (os.FileInfo, error) {
	return os.Lstat(path)
}

func (osPublicationFS) ReadDir(path string) ([]os.DirEntry, error) {
	return os.ReadDir(path)
}

func (osPublicationFS) OpenFile(path string, flags int, mode os.FileMode) (publicationFile, error) {
	return os.OpenFile(path, flags, mode)
}

func (osPublicationFS) Mkdir(path string, mode os.FileMode) error {
	return os.Mkdir(path, mode)
}

func (osPublicationFS) Rename(oldPath, newPath string) error {
	return os.Rename(oldPath, newPath)
}

func (osPublicationFS) Remove(path string) error {
	return os.Remove(path)
}
