package fleetconfig

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
)

// Publish atomically renders a compiled bundle into out.
func (p *Publisher) Publish(bundle Bundle, out string) (result error) {
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
	parentPath := filepath.Dir(root)
	rootName := filepath.Base(root)
	parentInfo, err := p.fs.Lstat(parentPath)
	if err != nil {
		return fmt.Errorf("fleetconfig: inspect output parent %q: %w", parentPath, err)
	}
	if parentInfo.Mode()&os.ModeSymlink != 0 || !parentInfo.IsDir() {
		return fmt.Errorf("fleetconfig: output parent %q is not a directory", parentPath)
	}
	parent, err := p.fs.OpenDir(parentPath)
	if err != nil {
		return fmt.Errorf("fleetconfig: open output parent %q: %w", parentPath, err)
	}
	defer func() {
		if closeErr := parent.Close(); closeErr != nil {
			result = joinPublicationErrors(result, fmt.Errorf("fleetconfig: close output parent %q: %w", parentPath, closeErr))
		}
	}()
	if err := p.rejectLeftoversAt(parent, parentPath, rootName); err != nil {
		return err
	}
	snapshot, err := p.snapshotAt(parent, rootName, root)
	if err != nil {
		return err
	}
	if snapshot.exists && len(snapshot.entries) > 0 && !snapshot.owned {
		return fmt.Errorf("fleetconfig: output %q is non-empty and is not a fleetgen-owned tree", out)
	}

	stageName, err := p.reserveSiblingAt(parent, parentPath, rootName, "stage")
	if err != nil {
		return err
	}
	backupName, err := p.reserveSiblingAt(parent, parentPath, rootName, "backup")
	if err != nil {
		return err
	}
	stagePath := filepath.Join(parentPath, stageName)
	backupPath := filepath.Join(parentPath, backupName)

	stageCreated := false
	cleanupStage := func() error {
		if !stageCreated {
			return nil
		}
		return p.removeEntry(parent, stageName, stagePath)
	}
	if err := parent.Mkdir(stageName, 0o700); err != nil {
		return fmt.Errorf("fleetconfig: create private staging directory: %w", err)
	}
	stageCreated = true
	stageDir, err := parent.OpenDir(stageName)
	if err != nil {
		cleanupErr := cleanupStage()
		return joinPublicationErrors(fmt.Errorf("fleetconfig: open staging directory: %w", err), cleanupErr)
	}
	writeErr := p.writeStageDir(stageDir, expected)
	closeStageErr := stageDir.Close()
	if writeErr != nil || closeStageErr != nil {
		cleanupErr := cleanupStage()
		return joinPublicationErrors(joinPublicationErrors(writeErr, closeStageErr), cleanupErr)
	}

	movedOld := false
	if snapshot.exists {
		// Recheck the root after staging. A newly appeared unowned tree must
		// never be replaced just because the earlier preflight saw absence.
		current, err := p.snapshotAt(parent, rootName, root)
		if err != nil {
			cleanupErr := cleanupStage()
			return joinPublicationErrors(err, cleanupErr)
		}
		if !sameRootState(snapshot, current) {
			cleanupErr := cleanupStage()
			return joinPublicationErrors(errors.New("fleetconfig: output changed during staging; refusing publication"), cleanupErr)
		}
		if err := p.requireParentStable(parentPath, parentInfo); err != nil {
			cleanupErr := cleanupStage()
			return joinPublicationErrors(err, cleanupErr)
		}
		if err := parent.Rename(rootName, backupName); err != nil {
			cleanupErr := cleanupStage()
			return joinPublicationErrors(fmt.Errorf("fleetconfig: move existing output to backup: %w", err), cleanupErr)
		}
		movedOld = true
		if err := p.requireParentStable(parentPath, parentInfo); err != nil {
			rollbackErr := parent.Rename(backupName, rootName)
			movedOld = false
			cleanupErr := cleanupStage()
			return joinPublicationErrors(err, joinPublicationErrors(rollbackErr, cleanupErr))
		}
	} else {
		// Avoid replacing a path that appeared after the absent-root
		// preflight. Rename itself does not have a portable no-replace mode.
		current, err := p.snapshotAt(parent, rootName, root)
		if err != nil {
			cleanupErr := cleanupStage()
			return joinPublicationErrors(err, cleanupErr)
		}
		if current.exists {
			cleanupErr := cleanupStage()
			return joinPublicationErrors(errors.New("fleetconfig: output appeared during staging; refusing publication"), cleanupErr)
		}
	}

	if err := p.requireParentStable(parentPath, parentInfo); err != nil {
		cleanupErr := cleanupStage()
		return joinPublicationErrors(err, cleanupErr)
	}
	if err := parent.Rename(stageName, rootName); err != nil {
		var rollbackErr error
		if movedOld {
			rollbackErr = parent.Rename(backupName, rootName)
			movedOld = false
		}
		cleanupErr := cleanupStage()
		return joinPublicationErrors(
			fmt.Errorf("fleetconfig: publish staged output: %w", err),
			joinPublicationErrors(rollbackErr, cleanupErr),
		)
	}
	stageCreated = false
	if err := p.requireParentStable(parentPath, parentInfo); err != nil {
		var rollbackErr error
		removeErr := p.removeEntry(parent, rootName, root)
		if movedOld {
			rollbackErr = parent.Rename(backupName, rootName)
			movedOld = false
		}
		return joinPublicationErrors(err, joinPublicationErrors(removeErr, rollbackErr))
	}

	if movedOld {
		if err := p.requireParentStable(parentPath, parentInfo); err != nil {
			return &CommittedPublicationError{Artifact: backupPath, Err: err}
		}
		if err := p.removeEntry(parent, backupName, backupPath); err != nil {
			// The swap is committed. Recursive cleanup may have already
			// removed part of backup, so attempting rollback here could
			// silently lose bytes from the old publication.
			return &CommittedPublicationError{Artifact: backupPath, Err: err}
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
func (p *Publisher) Check(bundle Bundle, out string) (drift Drift, result error) {
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
	parentPath := filepath.Dir(root)
	rootName := filepath.Base(root)
	parentInfo, err := p.fs.Lstat(parentPath)
	if err != nil {
		return Drift{}, fmt.Errorf("fleetconfig: inspect output parent %q: %w", parentPath, err)
	}
	if parentInfo.Mode()&os.ModeSymlink != 0 || !parentInfo.IsDir() {
		return Drift{}, fmt.Errorf("fleetconfig: output parent %q is not a directory", parentPath)
	}
	parent, err := p.fs.OpenDir(parentPath)
	if err != nil {
		return Drift{}, fmt.Errorf("fleetconfig: open output parent %q: %w", parentPath, err)
	}
	defer func() {
		if closeErr := parent.Close(); closeErr != nil {
			result = joinPublicationErrors(result, fmt.Errorf("fleetconfig: close output parent %q: %w", parentPath, closeErr))
		}
	}()
	if err := p.rejectLeftoversAt(parent, parentPath, rootName); err != nil {
		return Drift{}, err
	}
	snapshot, err := p.snapshotAt(parent, rootName, root)
	if err != nil {
		return Drift{}, err
	}
	if snapshot.exists && len(snapshot.entries) > 0 && !snapshot.owned {
		return Drift{}, fmt.Errorf("fleetconfig: output %q is non-empty and is not a fleetgen-owned tree", out)
	}
	return comparePublication(expected, snapshot), nil
}

func (p *Publisher) rejectLeftoversAt(parent publicationDir, parentPath, base string) error {
	entries, err := parent.ReadDir()
	if err != nil {
		return fmt.Errorf("fleetconfig: inspect output siblings: %w", err)
	}
	stagePrefix := "." + base + ".fleetgen-stage-"
	backupPrefix := "." + base + ".fleetgen-backup-"
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasPrefix(name, stagePrefix) || strings.HasPrefix(name, backupPrefix) {
			return fmt.Errorf("fleetconfig: interrupted fleetgen transaction artifact %q exists; remove it only after inspection", filepath.Join(parentPath, name))
		}
	}
	return nil
}

func (p *Publisher) reserveSiblingAt(parent publicationDir, parentPath, base, kind string) (string, error) {
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
		candidate := prefix + suffix
		_, err := parent.Lstat(candidate)
		if errors.Is(err, os.ErrNotExist) {
			return candidate, nil
		}
		if err != nil {
			return "", fmt.Errorf("fleetconfig: inspect transaction sibling %q: %w", filepath.Join(parentPath, candidate), err)
		}
	}
	return "", errors.New("fleetconfig: could not reserve a transaction sibling")
}

// Check compares a compiled bundle with out using a host-filesystem
// Publisher.
func Check(bundle Bundle, out string) (Drift, error) {
	return NewPublisher().Check(bundle, out)
}

func (p *Publisher) writeStageDir(stage publicationDir, files []File) error {
	for _, file := range files {
		if err := p.writeStageFile(stage, file.Path, file.Data); err != nil {
			return err
		}
	}
	return nil
}

func (p *Publisher) writeStageFile(stage publicationDir, path string, data []byte) (result error) {
	parts := strings.Split(filepath.ToSlash(path), "/")
	if len(parts) == 0 {
		return errors.New("fleetconfig: empty staging path")
	}
	current := stage
	opened := make([]publicationDir, 0, len(parts)-1)
	closeOpened := func() error {
		var closeErr error
		for index := len(opened) - 1; index >= 0; index-- {
			closeErr = joinPublicationErrors(closeErr, opened[index].Close())
		}
		return closeErr
	}
	defer func() {
		result = joinPublicationErrors(result, closeOpened())
	}()
	for _, part := range parts[:len(parts)-1] {
		if part == "" || part == "." || part == ".." || strings.Contains(part, "\\") {
			return errors.New("fleetconfig: unsafe generated parent path")
		}
		info, err := current.Lstat(part)
		if errors.Is(err, os.ErrNotExist) {
			if err := current.Mkdir(part, 0o700); err != nil {
				return fmt.Errorf("fleetconfig: create staging parent %q: %w", path, err)
			}
			info, err = current.Lstat(part)
		}
		if err != nil {
			return fmt.Errorf("fleetconfig: inspect staging parent %q: %w", path, err)
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return fmt.Errorf("fleetconfig: staging parent %q is not a directory", path)
		}
		child, err := current.OpenDir(part)
		if err != nil {
			return fmt.Errorf("fleetconfig: open staging parent %q: %w", path, err)
		}
		opened = append(opened, child)
		current = child
	}
	name := parts[len(parts)-1]
	if name == "" || name == "." || name == ".." || strings.Contains(name, "\\") {
		return errors.New("fleetconfig: unsafe generated file path")
	}
	handle, err := current.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		return fmt.Errorf("fleetconfig: stage %q: %w", path, err)
	}
	writeErr := writeAll(handle, data)
	syncErr := error(nil)
	if writeErr == nil {
		syncErr = handle.Sync()
	}
	closeErr := handle.Close()
	if writeErr != nil {
		return fmt.Errorf("fleetconfig: stage %q: write: %w", path, writeErr)
	}
	if syncErr != nil {
		return fmt.Errorf("fleetconfig: stage %q: sync: %w", path, syncErr)
	}
	if closeErr != nil {
		return fmt.Errorf("fleetconfig: stage %q: close: %w", path, closeErr)
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

func (p *Publisher) removeTree(path string) error {
	parentPath := filepath.Dir(path)
	name := filepath.Base(path)
	parentInfo, err := p.fs.Lstat(parentPath)
	if err != nil {
		return fmt.Errorf("fleetconfig: inspect transaction parent %q: %w", parentPath, err)
	}
	if parentInfo.Mode()&os.ModeSymlink != 0 || !parentInfo.IsDir() {
		return fmt.Errorf("fleetconfig: transaction parent %q is not a directory", parentPath)
	}
	parent, err := p.fs.OpenDir(parentPath)
	if err != nil {
		return fmt.Errorf("fleetconfig: open transaction parent %q: %w", parentPath, err)
	}
	if err := p.requireParentStable(parentPath, parentInfo); err != nil {
		_ = parent.Close()
		return err
	}
	removeErr := p.removeEntry(parent, name, path)
	stableErr := p.requireParentStable(parentPath, parentInfo)
	closeErr := parent.Close()
	if removeErr != nil {
		return joinPublicationErrors(removeErr, joinPublicationErrors(stableErr, closeErr))
	}
	if stableErr != nil {
		return joinPublicationErrors(stableErr, closeErr)
	}
	if closeErr != nil {
		return fmt.Errorf("fleetconfig: close transaction parent %q: %w", parentPath, closeErr)
	}
	return nil
}

func (p *Publisher) removeEntry(parent publicationDir, name, display string) error {
	info, err := parent.Lstat(name)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("fleetconfig: inspect transaction entry %q: %w", display, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("fleetconfig: transaction entry %q became a symlink", display)
	}
	if !info.IsDir() {
		current, err := parent.Lstat(name)
		if err != nil {
			return fmt.Errorf("fleetconfig: inspect transaction entry %q before removal: %w", display, err)
		}
		if !os.SameFile(info, current) {
			return fmt.Errorf("fleetconfig: transaction entry %q changed before removal", display)
		}
		return parent.Remove(name, false)
	}
	childDir, err := parent.OpenDir(name)
	if err != nil {
		return fmt.Errorf("fleetconfig: open transaction directory %q: %w", display, err)
	}
	removeErr := p.removeDirectory(childDir, display)
	closeErr := childDir.Close()
	if removeErr != nil {
		return joinPublicationErrors(removeErr, closeErr)
	}
	if closeErr != nil {
		return fmt.Errorf("fleetconfig: close transaction directory %q: %w", display, closeErr)
	}
	current, err := parent.Lstat(name)
	if err != nil {
		return fmt.Errorf("fleetconfig: inspect transaction directory %q after cleanup: %w", display, err)
	}
	if !os.SameFile(info, current) {
		return fmt.Errorf("fleetconfig: transaction directory %q changed during cleanup", display)
	}
	return parent.Remove(name, true)
}

func (p *Publisher) removeDirectory(dir publicationDir, display string) error {
	children, err := dir.ReadDir()
	if err != nil {
		return fmt.Errorf("fleetconfig: read transaction directory %q: %w", display, err)
	}
	sort.Slice(children, func(i, j int) bool { return children[i].Name() < children[j].Name() })
	for _, child := range children {
		name := child.Name()
		if name == "" || name == "." || name == ".." || strings.Contains(name, "/") || strings.Contains(name, "\\") {
			return errors.New("fleetconfig: unsafe transaction entry name")
		}
		entryDisplay := filepath.ToSlash(filepath.Join(display, name))
		if err := p.removeEntry(dir, name, entryDisplay); err != nil {
			return err
		}
	}
	return nil
}

func (p *Publisher) requireParentStable(path string, want os.FileInfo) error {
	current, err := p.fs.Lstat(path)
	if err != nil {
		return fmt.Errorf("fleetconfig: output parent %q changed during publication: %w", path, err)
	}
	if current.Mode()&os.ModeSymlink != 0 || !current.IsDir() || !os.SameFile(want, current) {
		return fmt.Errorf("fleetconfig: output parent %q changed during publication", path)
	}
	return nil
}

func sameRootState(left, right rootSnapshot) bool {
	if left.exists != right.exists || left.owned != right.owned {
		return false
	}
	if left.exists && (left.identity == nil || right.identity == nil || !os.SameFile(left.identity, right.identity)) {
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
