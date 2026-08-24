//go:build !linux

package fleetconfig

import (
	"errors"
)

func openDirectoryNoFollow(path string) (publicationDir, error) {
	return nil, errors.New("fleetconfig: fd-relative no-follow publication is unsupported on this platform")
}
