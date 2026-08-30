package media

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// HashedName returns the path a file should have once tagged: the original name
// with ".<prefix>" inserted before the extension. For example
// HashedName("/v/a.mp4", "1a2b3c") == "/v/a.1a2b3c.mp4".
func HashedName(path, prefix string) string {
	dir := filepath.Dir(path)
	ext := filepath.Ext(path)
	stem := strings.TrimSuffix(filepath.Base(path), ext)
	return filepath.Join(dir, fmt.Sprintf("%s.%s%s", stem, prefix, ext))
}

// AlreadyTagged reports whether path's stem already ends with ".<prefix>",
// meaning a previous run tagged and renamed it.
func AlreadyTagged(path, prefix string) bool {
	ext := filepath.Ext(path)
	stem := strings.TrimSuffix(filepath.Base(path), ext)
	return strings.HasSuffix(stem, "."+prefix)
}

// SwapInPlace atomically replaces dst with src (both assumed on the same
// filesystem) and removes src. It refuses to clobber an existing, different
// file at dst unless overwrite is set.
func SwapInPlace(src, dst string, overwrite bool) error {
	if src == dst {
		return nil
	}
	if !overwrite {
		if _, err := os.Stat(dst); err == nil {
			return fmt.Errorf("refusing to overwrite existing file: %s", dst)
		}
	}
	return os.Rename(src, dst)
}
