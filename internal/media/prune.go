package media

import (
	"io/fs"
	"os"
	"path/filepath"
)

// PruneEmptyDirs walks each root and removes every directory that is empty —
// including a directory emptied only by removing its now-empty children, and a
// root itself when nothing is left in it. It works bottom-up: the deepest
// directories are considered first.
//
// It is best-effort: a directory it cannot remove (not empty, permission
// denied) is silently skipped. A filesystem root and the process's current
// working directory are never removed. Roots that are not existing directories
// are ignored. It returns the directory paths it removed, for logging.
func PruneEmptyDirs(roots ...string) []string {
	cwd, _ := os.Getwd()
	if cwd != "" {
		cwd = resolve(cwd)
	}

	var removed []string
	for _, root := range roots {
		info, err := os.Stat(root)
		if err != nil || !info.IsDir() {
			continue
		}

		// WalkDir yields a parent before its children, so iterating the
		// collected list in reverse visits the deepest directories first.
		var dirs []string
		_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return nil
			}
			if d.IsDir() {
				dirs = append(dirs, path)
			}
			return nil
		})

		for i := len(dirs) - 1; i >= 0; i-- {
			dir := dirs[i]
			abs := resolve(dir)
			if abs == filepath.Dir(abs) || abs == cwd { // filesystem root / cwd
				continue
			}
			entries, err := os.ReadDir(dir)
			if err != nil || len(entries) > 0 {
				continue
			}
			if os.Remove(dir) == nil {
				removed = append(removed, dir)
			}
		}
	}
	return removed
}

// resolve returns path as an absolute, symlink-resolved path so comparisons
// hold on platforms where the temp dir or cwd is reached through a symlink
// (e.g. macOS /var -> /private/var). It falls back to the plain absolute path
// when the target cannot be resolved.
func resolve(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		return path
	}
	if r, err := filepath.EvalSymlinks(abs); err == nil {
		return r
	}
	return abs
}
