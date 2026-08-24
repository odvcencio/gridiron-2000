//go:build linux

package fleetconfig

import (
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

type osPublicationDir struct {
	file *os.File
}

func openDirectoryNoFollow(path string) (publicationDir, error) {
	how := &unix.OpenHow{
		Flags:   uint64(unix.O_RDONLY | unix.O_DIRECTORY | unix.O_CLOEXEC),
		Resolve: unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_MAGICLINKS,
	}
	fd, err := unix.Openat2(unix.AT_FDCWD, path, how)
	if err != nil {
		return nil, err
	}
	return &osPublicationDir{file: os.NewFile(uintptr(fd), path)}, nil
}

func (d *osPublicationDir) ReadDir() ([]os.DirEntry, error) {
	return d.file.ReadDir(-1)
}

func (d *osPublicationDir) Stat() (os.FileInfo, error) {
	return d.file.Stat()
}

func (d *osPublicationDir) Lstat(name string) (os.FileInfo, error) {
	fd, err := unix.Openat(int(d.file.Fd()), name, unix.O_PATH|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	child := os.NewFile(uintptr(fd), name)
	info, statErr := child.Stat()
	closeErr := child.Close()
	if statErr != nil {
		return nil, statErr
	}
	if closeErr != nil {
		return nil, closeErr
	}
	return info, nil
}

func (d *osPublicationDir) OpenDir(name string) (publicationDir, error) {
	how := &unix.OpenHow{
		Flags:   uint64(unix.O_RDONLY | unix.O_DIRECTORY | unix.O_CLOEXEC),
		Resolve: unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_MAGICLINKS,
	}
	fd, err := unix.Openat2(int(d.file.Fd()), name, how)
	if err != nil {
		return nil, err
	}
	return &osPublicationDir{file: os.NewFile(uintptr(fd), name)}, nil
}

func (d *osPublicationDir) OpenFile(name string, flags int, mode os.FileMode) (publicationFile, error) {
	fd, err := unix.Openat(int(d.file.Fd()), name, flags, uint32(mode.Perm()))
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(fd), name), nil
}

func (d *osPublicationDir) Mkdir(name string, mode os.FileMode) error {
	return unix.Mkdirat(int(d.file.Fd()), name, uint32(mode.Perm()))
}

func (d *osPublicationDir) Rename(oldName, newName string) error {
	return unix.Renameat(int(d.file.Fd()), oldName, int(d.file.Fd()), newName)
}

func (d *osPublicationDir) Remove(name string, directory bool) error {
	flags := 0
	if directory {
		flags = unix.AT_REMOVEDIR
	}
	return unix.Unlinkat(int(d.file.Fd()), name, flags)
}

func (d *osPublicationDir) Close() error {
	if d == nil || d.file == nil {
		return nil
	}
	if err := d.file.Close(); err != nil {
		return fmt.Errorf("close directory: %w", err)
	}
	return nil
}
