// Package listcmd implements `mc list`: a recursive, filterable, sortable
// listing of media files with size and selected metadata.
package listcmd

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"path/filepath"
	"sort"
	"strings"

	"github.com/xdustinw/media-cli/internal/media"
	"github.com/xdustinw/media-cli/internal/mediainfo"
	"github.com/xdustinw/media-cli/internal/query"
	"github.com/xdustinw/media-cli/internal/render"
)

// Options configures a Run.
type Options struct {
	Root    string
	HashKey string   // freeform hash tag key shown as a default column ("mc.hash")
	Meta    []string // extra metadata columns
	SortBy  string
	Select  string
	Format  render.Format

	Stdout io.Writer
	Stderr io.Writer
	Logger *slog.Logger
}

// fixedColumns precede the configurable hash column and the --meta columns.
var fixedColumns = []string{"filename", "size", "rating", "authors", "tags"}

func (o Options) hashKey() string {
	if o.HashKey == "" {
		return "mc.hash"
	}
	return o.HashKey
}

// columns is the full ordered column list: filename, size, <hash key>, rating,
// authors, tags, then any extra --meta columns.
func (o Options) columns(extra []string) []string {
	cols := []string{"filename", "size", o.hashKey(), "rating", "authors", "tags"}
	return append(cols, extra...)
}

// Run walks Root, filters and sorts, and writes the listing.
func Run(ctx context.Context, o Options) error {
	log := o.Logger
	if log == nil {
		log = slog.Default()
	}

	sel, err := query.ParseSelect(o.Select)
	if err != nil {
		return fmt.Errorf("--select: %w", err)
	}
	sortKeys, err := query.ParseSort(o.SortBy)
	if err != nil {
		return fmt.Errorf("--sort-by: %w", err)
	}
	if len(sortKeys) == 0 {
		sortKeys = []query.SortKey{{Field: "name"}}
	}

	paths, err := discover(ctx, o.Root)
	if err != nil {
		return err
	}
	log.Info("listing", "root", o.Root, "candidates", len(paths))

	needDeep := selectorNeedsDeep(sel) || metaNeedsDeep(o.Meta)

	files := make([]*mediainfo.File, 0, len(paths))
	for _, p := range paths {
		if err := ctx.Err(); err != nil {
			return err
		}
		deep := needDeep || media.KindOf(p) == media.KindImage
		mf, err := mediainfo.Load(p, deep)
		if err != nil {
			fmt.Fprintf(o.Stderr, "  ! %s: %v\n", p, err)
			continue
		}
		if sel.Match(mf) {
			files = append(files, mf)
		}
	}

	less := query.Less(sortKeys)
	sort.SliceStable(files, func(i, j int) bool { return less(files[i], files[j]) })

	extra := dedupeMeta(o.Meta, o.hashKey())
	columns := o.columns(extra)
	absRoot, _ := filepath.Abs(o.Root)
	tbl := render.Table{Columns: columns}
	for _, f := range files {
		tbl.Rows = append(tbl.Rows, row(f, absRoot, o.hashKey(), extra, o.Format))
	}

	if o.Format == render.TOON {
		fmt.Fprintf(o.Stdout, "%d file(s)\n", len(files))
	}
	return tbl.Encode(o.Stdout, o.Format)
}

func row(f *mediainfo.File, absRoot, hashKey string, meta []string, format render.Format) []any {
	name := f.Abs
	if format != render.CSV { // CSV always carries the absolute path
		if rel, err := filepath.Rel(absRoot, f.Abs); err == nil {
			name = rel
		}
	}

	var rating any
	if r, ok := f.Rating(); ok {
		rating = r
	}
	hash, _ := f.Meta(hashKey)

	cells := []any{
		name,
		mediainfo.HumanSize(f.Size),
		hash,
		rating,
		cell(f.Authors(), format),
		cell(f.Tags(), format),
	}
	for _, m := range meta {
		if v, ok := f.Meta(m); ok {
			cells = append(cells, v)
		} else {
			cells = append(cells, nil)
		}
	}
	return cells
}

// cell flattens a string list so every column stays a single scalar value: this
// keeps CSV valid and lets TOON render one flat row per file (rather than a
// nested object). JSON keeps the array.
func cell(v []string, format render.Format) any {
	if format == render.JSON {
		return v
	}
	return strings.Join(v, "; ")
}

func dedupeMeta(meta []string, hashKey string) []string {
	seen := map[string]struct{}{strings.ToLower(hashKey): {}}
	for _, c := range fixedColumns {
		seen[strings.ToLower(c)] = struct{}{}
	}
	var out []string
	for _, m := range meta {
		k := strings.ToLower(m)
		if _, ok := seen[k]; ok {
			continue
		}
		seen[k] = struct{}{}
		out = append(out, m)
	}
	return out
}

func discover(ctx context.Context, root string) ([]string, error) {
	var out []string
	walkErr := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if d.IsDir() {
			return nil
		}
		if d.Type().IsRegular() {
			out = append(out, filepath.Clean(p))
		}
		return nil
	})
	if walkErr != nil {
		return nil, walkErr
	}
	return out, nil
}

func selectorNeedsDeep(sel *query.Selector) bool {
	for _, field := range sel.Fields() {
		if fieldNeedsDeep(field) {
			return true
		}
	}
	return false
}

func metaNeedsDeep(meta []string) bool {
	for _, m := range meta {
		if fieldNeedsDeep(m) {
			return true
		}
	}
	return false
}

func fieldNeedsDeep(field string) bool {
	switch strings.ToLower(field) {
	case "name", "path", "size", "modifiedat", "modified", "mtime", "kind", "format":
		return false
	default:
		// rating / authors / tags / arbitrary EXIF -> decode a frame.
		return true
	}
}
