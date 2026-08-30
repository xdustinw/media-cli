package media

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// HashedName returns the path a file should have once tagged: the original name
// with ".<prefix>" inserted before the extension. If the name already ends with
// a ".<hex>" segment of the same length (a hash from a previous run), that
// segment is replaced rather than a second one appended:
//
//	HashedName("/v/a.mp4",        "1a2b3c") == "/v/a.1a2b3c.mp4"
//	HashedName("/v/a.9f8e7d.mp4", "1a2b3c") == "/v/a.1a2b3c.mp4"
func HashedName(path, prefix string) string {
	dir := filepath.Dir(path)
	ext := filepath.Ext(path)
	stem := strings.TrimSuffix(filepath.Base(path), ext)
	stem = strings.TrimSuffix(stem, "."+existingHashSuffix(stem, len(prefix)))
	return filepath.Join(dir, fmt.Sprintf("%s.%s%s", stem, prefix, ext))
}

// AlreadyTagged reports whether path's stem already ends with ".<prefix>",
// meaning a previous run tagged and renamed it.
func AlreadyTagged(path, prefix string) bool {
	ext := filepath.Ext(path)
	stem := strings.TrimSuffix(filepath.Base(path), ext)
	return strings.HasSuffix(stem, "."+prefix)
}

// existingHashSuffix returns the trailing ".<hex>" run of stem (without the
// dot) when it is exactly n hex characters, else "".
func existingHashSuffix(stem string, n int) string {
	i := strings.LastIndexByte(stem, '.')
	if i < 0 {
		return ""
	}
	suffix := stem[i+1:]
	if len(suffix) != n {
		return ""
	}
	for _, c := range suffix {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') && (c < 'A' || c > 'F') {
			return ""
		}
	}
	return suffix
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
