//go:build windows

package media

import (
	"errors"
	"syscall"
)

// errorNotSameDevice is Windows' ERROR_NOT_SAME_DEVICE (0x11): the error
// os.Rename surfaces when src and dst are on different drives. Go maps Unix
// EXDEV to a different numeric value on Windows, so both must be checked.
const errorNotSameDevice = syscall.Errno(0x11)

// isCrossDevice reports whether err is the "rename across volumes" error that
// os.Rename returns when src and dst live on different drives.
func isCrossDevice(err error) bool {
	return errors.Is(err, syscall.EXDEV) || errors.Is(err, errorNotSameDevice)
}
