package media

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/xdustinw/media-cli/internal/query"
)

// Facts implements query.Fields for the filesystem-only fields, enough to
// evaluate a --select filter (name, path, ext, size, modifiedAt, kind) on the
// bulk commands without opening or probing the file.
type Facts struct {
	Path    string
	Size    int64
	ModTime time.Time
}

// StatFacts builds Facts for path with a single stat call.
func StatFacts(path string) (Facts, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return Facts{}, err
	}
	return Facts{Path: filepath.Clean(path), Size: fi.Size(), ModTime: fi.ModTime()}, nil
}

// QueryField implements query.Fields.
func (f Facts) QueryField(name string) query.Value {
	switch strings.ToLower(name) {
	case "name":
		return query.String(filepath.Base(f.Path))
	case "path":
		return query.String(f.Path)
	case "ext":
		return query.String(strings.TrimPrefix(strings.ToLower(filepath.Ext(f.Path)), "."))
	case "size":
		return query.Number(float64(f.Size))
	case "modifiedat", "modified", "mtime":
		return query.Time(f.ModTime)
	case "kind":
		return query.String(KindOf(f.Path).String())
	default:
		return query.Absent
	}
}

// SelectableFields names the fields Facts supports, for help text and errors.
var SelectableFields = []string{"name", "path", "ext", "size", "modifiedAt", "kind"}
