package fleetconfig

import (
	"io"
	"os"
)

type publicationFile interface {
	io.Reader
	io.Writer
	Stat() (os.FileInfo, error)
	Sync() error
	Close() error
}

// publicationDir is a stable handle to one directory. All child operations
// are relative to that handle, so replacing the directory's path cannot make
// a traversal follow an attacker-controlled symlink.
type publicationDir interface {
	Stat() (os.FileInfo, error)
	ReadDir() ([]os.DirEntry, error)
	Lstat(name string) (os.FileInfo, error)
	OpenDir(name string) (publicationDir, error)
	OpenFile(name string, flags int, mode os.FileMode) (publicationFile, error)
	Mkdir(name string, mode os.FileMode) error
	Rename(oldName, newName string) error
	Remove(name string, directory bool) error
	Close() error
}

type publicationFS interface {
	Lstat(string) (os.FileInfo, error)
	ReadDir(string) ([]os.DirEntry, error)
	OpenDir(string) (publicationDir, error)
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
	dir, err := osPublicationFS{}.OpenDir(path)
	if err != nil {
		return nil, err
	}
	defer dir.Close()
	return dir.ReadDir()
}

func (osPublicationFS) OpenDir(path string) (publicationDir, error) {
	return openDirectoryNoFollow(path)
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
