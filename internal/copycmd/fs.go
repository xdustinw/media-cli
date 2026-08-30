package copycmd

import (
	"errors"
	"os"
)

func isNotExist(err error) bool { return errors.Is(err, os.ErrNotExist) }

func statFile(path string) (os.FileInfo, error) { return os.Stat(path) }

func removeFile(path string) error { return os.Remove(path) }

func renameFile(oldPath, newPath string) error { return os.Rename(oldPath, newPath) }

func fileSize(path string) int64 {
	if fi, err := os.Stat(path); err == nil {
		return fi.Size()
	}
	return 0
}
