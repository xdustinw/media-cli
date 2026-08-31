//go:build !windows

package media

import (
	"errors"
	"syscall"
)

// isCrossDevice reports whether err is the "rename across filesystems" error
// that os.Rename returns when src and dst live on different volumes.
func isCrossDevice(err error) bool {
	return errors.Is(err, syscall.EXDEV)
}
