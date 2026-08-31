// Package media provides filesystem helpers for locating and mutating media
// files independent of any particular command.
package media

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/xdustinw/media-cli/internal/query"
)

// ErrNoMediaFiles is returned when a target contains no files matching the
// configured extensions (and the --select filter, when given).
var ErrNoMediaFiles = errors.New("no media files found")

// Discover resolves target into a sorted list of media file paths.
//
//   - If target is a regular file it is returned as-is (extension is not
//     enforced for an explicitly named file).
//   - If target is a directory, files whose extension (case-insensitive) is in
//     exts are returned. When recursive is true subdirectories are descended
//     into; otherwise only the directory's own files are considered.
//
// The walk stops early and returns ctx.Err() if ctx is cancelled.
func Discover(ctx context.Context, target string, exts []string, recursive bool) ([]string, error) {
	out, err := discoverOne(ctx, target, exts, recursive)
	if err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, ErrNoMediaFiles
	}
	sort.Strings(out)
	return out, nil
}

// DiscoverMany resolves several targets (each a file or folder) into one sorted,
// de-duplicated file list. Folders are filtered by exts; sel (when non-nil) is
// matched against each file's filesystem Facts. recursive controls folder
// descent for every target.
func DiscoverMany(ctx context.Context, targets []string, exts []string, recursive bool, sel *query.Selector) ([]string, error) {
	seen := map[string]struct{}{}
	var out []string
	for _, t := range targets {
		one, err := discoverOne(ctx, t, exts, recursive)
		if err != nil {
			return nil, err
		}
		for _, f := range one {
			if _, dup := seen[f]; dup {
				continue
			}
			if sel != nil {
				facts, ferr := StatFacts(f)
				if ferr != nil || !sel.Match(facts) {
					continue
				}
			}
			seen[f] = struct{}{}
			out = append(out, f)
		}
	}
	if len(out) == 0 {
		return nil, ErrNoMediaFiles
	}
	sort.Strings(out)
	return out, nil
}

func discoverOne(ctx context.Context, target string, exts []string, recursive bool) ([]string, error) {
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
	if recursive {
		walkErr := filepath.WalkDir(target, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if !d.IsDir() {
				if _, ok := allow[strings.ToLower(filepath.Ext(path))]; ok {
					out = append(out, filepath.Clean(path))
				}
			}
			return nil
		})
		if walkErr != nil {
			return nil, walkErr
		}
	} else {
		entries, rerr := os.ReadDir(target)
		if rerr != nil {
			return nil, rerr
		}
		for _, e := range entries {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			if e.IsDir() {
				continue
			}
			p := filepath.Join(target, e.Name())
			if _, ok := allow[strings.ToLower(filepath.Ext(p))]; ok {
				out = append(out, filepath.Clean(p))
			}
		}
	}
	return out, nil
}

// WalkFiles returns the regular files under root, sorted. When root is a single
// file it returns just that file. When recursive is false only root's own files
// are returned. The tool's own temp files (".mc-*") are skipped. Unlike Discover
// it does not filter by extension.
func WalkFiles(ctx context.Context, root string, recursive bool) ([]string, error) {
	info, err := os.Stat(root)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return []string{filepath.Clean(root)}, nil
	}
	var out []string
	walkErr := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if strings.HasPrefix(d.Name(), ".mc-") {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			if !recursive && path != filepath.Clean(root) {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Type().IsRegular() {
			out = append(out, filepath.Clean(path))
		}
		return nil
	})
	if walkErr != nil {
		return nil, walkErr
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

// Summary formats the per-command wrap-up line, e.g.
// "processed 12 file(s) in 3.4s (28.1 MB/s)". Runs over a minute are shown as
// "2m 5s" for readability. The rate is omitted when it cannot be computed.
func Summary(files int, bytes int64, d time.Duration) string {
	line := fmt.Sprintf("processed %d file(s) in %s", files, humanDuration(d))
	if secs := d.Seconds(); secs > 0 && bytes > 0 {
		line += fmt.Sprintf(" (%.1f MB/s)", float64(bytes)/(1024*1024)/secs)
	}
	return line
}

// humanDuration renders d as "2m 5s" once it reaches a minute, otherwise as a
// sub-minute value rounded to the millisecond (e.g. "3.4s", "820ms").
func humanDuration(d time.Duration) string {
	if d < time.Minute {
		return d.Round(time.Millisecond).String()
	}
	d = d.Round(time.Second)
	return fmt.Sprintf("%dm %ds", d/time.Minute, (d%time.Minute)/time.Second)
}
