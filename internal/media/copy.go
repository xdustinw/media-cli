package media

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

// CopyFile copies src to dst byte-for-byte, creating dst's parent directories.
// The write goes to a temp file in dst's directory and is renamed into place, so
// dst is either the old file or the complete new one, never a partial mix.
func CopyFile(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	tmp := filepath.Join(filepath.Dir(dst),
		".mc-copy-"+strconv.FormatInt(time.Now().UnixNano(), 36))
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(tmp)
		return err
	}
	if err := out.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	if fi, serr := os.Stat(src); serr == nil {
		_ = os.Chmod(tmp, fi.Mode().Perm())
	}
	if err := os.Rename(tmp, dst); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

// MoveFile renames src to dst, creating dst's parent directories.
//
// When src and dst sit on different volumes os.Rename fails — with EXDEV on
// Unix, ERROR_NOT_SAME_DEVICE on Windows — and MoveFile copies the bytes across
// instead, removing src only after the copied file is confirmed to be the same
// size. Any other rename error is returned unchanged and src is left in place,
// so a failed move never destroys the original.
func MoveFile(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	if err := os.Rename(src, dst); err == nil {
		return nil
	} else if !isCrossDevice(err) {
		return err
	}

	srcInfo, err := os.Stat(src)
	if err != nil {
		return err
	}
	if err := CopyFile(src, dst); err != nil {
		return err
	}
	dstInfo, err := os.Stat(dst)
	if err != nil {
		return fmt.Errorf("verifying copied file (source kept at %s): %w", src, err)
	}
	if dstInfo.Size() != srcInfo.Size() {
		return fmt.Errorf("copied file is %d bytes, expected %d; source kept at %s",
			dstInfo.Size(), srcInfo.Size(), src)
	}
	return os.Remove(src)
}
