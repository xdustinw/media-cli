package media

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"syscall"
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

// MoveFile renames src to dst (creating dst's parent directories), falling back
// to CopyFile followed by removing src when the two are on different
// filesystems (os.Rename returns EXDEV).
func MoveFile(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	err := os.Rename(src, dst)
	if err == nil {
		return nil
	}
	if !errors.Is(err, syscall.EXDEV) {
		return err
	}
	if cerr := CopyFile(src, dst); cerr != nil {
		return cerr
	}
	return os.Remove(src)
}
