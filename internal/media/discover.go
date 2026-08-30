// Package media provides filesystem helpers for locating and mutating media
// files independent of any particular command.
package media

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ErrNoMediaFiles is returned by Discover when the target contains no files
// matching the configured extensions.
var ErrNoMediaFiles = errors.New("no media files found")

// Discover resolves target into a sorted list of media file paths.
//
//   - If target is a regular file it is returned as-is (extension is not
//     enforced for an explicitly named file).
//   - If target is a directory it is walked recursively and every file whose
//     extension (case-insensitive) is in exts is returned.
//
// The walk stops early and returns ctx.Err() if ctx is cancelled.
func Discover(ctx context.Context, target string, exts []string) ([]string, error) {
	info, err := os.Stat(target)
	if err != nil {
		return nil, err
	}

	if !info.IsDir() {
		return []string{filepath.Clean(target)}, nil
	}

	allow := make(map[string]struct{}, len(exts))
	for _, e := range exts {
		allow[strings.ToLower(e)] = struct{}{}
	}

	var out []string
	walkErr := filepath.WalkDir(target, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if d.IsDir() {
			return nil
		}
		if _, ok := allow[strings.ToLower(filepath.Ext(path))]; ok {
			out = append(out, filepath.Clean(path))
		}
		return nil
	})
	if walkErr != nil {
		return nil, walkErr
	}
	if len(out) == 0 {
		return nil, ErrNoMediaFiles
	}

	sort.Strings(out)
	return out, nil
}

// RelTo returns path relative to base, falling back to path on failure. Used for
// display only.
func RelTo(base, path string) string {
	if rel, err := filepath.Rel(base, path); err == nil {
		return rel
	}
	return path
}

// DisplayBase returns the directory used to compute relative display paths for
// target: target itself when it is a directory, otherwise its parent.
func DisplayBase(target string) string {
	if info, err := os.Stat(target); err == nil && info.IsDir() {
		return filepath.Clean(target)
	}
	return filepath.Dir(filepath.Clean(target))
}
