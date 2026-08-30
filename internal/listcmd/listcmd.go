// Package listcmd implements `mc list`: a recursive, filterable, sortable
// listing of media files with size and selected metadata.
package listcmd

import (
	"context"
	"encoding/json"
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

// Run walks Root, filters and sorts, and writes the listing. CSV output is a
// flat table with absolute paths; TOON and JSON nest files under their folders.
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

	if o.Format == render.CSV {
		tbl := render.Table{Columns: columns}
		for _, f := range files {
			tbl.Rows = append(tbl.Rows, csvRow(f, absRoot, o.hashKey(), extra))
		}
		return tbl.Encode(o.Stdout, render.CSV)
	}

	// TOON / JSON: nest files beneath their folders.
	root := &treeNode{}
	for _, f := range files {
		node := root.walk(relDirParts(absRoot, f.Abs))
		node.files = append(node.files, f)
	}
	doc := render.NewOM(rootLabel(absRoot), root.render(o.hashKey(), extra))

	if o.Format == render.JSON {
		enc := json.NewEncoder(o.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(doc)
	}

	fmt.Fprintf(o.Stdout, "%d file(s)\n", len(files))
	s, err := doc.TOON()
	if err != nil {
		return err
	}
	_, err = io.WriteString(o.Stdout, s+"\n")
	return err
}

// treeNode is one directory in the output hierarchy.
type treeNode struct {
	subs  map[string]*treeNode
	files []*mediainfo.File
}

func (n *treeNode) child(name string) *treeNode {
	if n.subs == nil {
		n.subs = map[string]*treeNode{}
	}
	c := n.subs[name]
	if c == nil {
		c = &treeNode{}
		n.subs[name] = c
	}
	return c
}

func (n *treeNode) walk(parts []string) *treeNode {
	cur := n
	for _, p := range parts {
		cur = cur.child(p)
	}
	return cur
}

// render turns the node into an ordered map: sub-directories (sorted) as keys,
// then a `files` key holding the tabular rows for this directory's own files.
func (n *treeNode) render(hashKey string, extra []string) *render.OM {
	o := render.NewOM()

	names := make([]string, 0, len(n.subs))
	for name := range n.subs {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		o.Set(name+"/", n.subs[name].render(hashKey, extra))
	}

	if len(n.files) > 0 {
		rows := make([]*render.OM, len(n.files))
		for i, f := range n.files {
			rows[i] = fileOM(f, hashKey, extra)
		}
		o.Set("files", rows)
	}
	return o
}

func fileOM(f *mediainfo.File, hashKey string, extra []string) *render.OM {
	hash, _ := f.Meta(hashKey)
	var rating any = ""
	if r, ok := f.Rating(); ok {
		rating = r
	}
	o := render.NewOM(
		"filename", f.Name,
		"size", mediainfo.HumanSize(f.Size),
		hashKey, hash,
		"rating", rating,
		"authors", strings.Join(f.Authors(), "; "),
		"tags", strings.Join(f.Tags(), "; "),
	)
	for _, m := range extra {
		v, _ := f.Meta(m)
		o.Set(m, v)
	}
	return o
}

func csvRow(f *mediainfo.File, absRoot, hashKey string, meta []string) []any {
	hash, _ := f.Meta(hashKey)
	var rating any
	if r, ok := f.Rating(); ok {
		rating = r
	}
	cells := []any{
		f.Abs, // CSV always carries the absolute path
		mediainfo.HumanSize(f.Size),
		hash,
		rating,
		strings.Join(f.Authors(), "; "),
		strings.Join(f.Tags(), "; "),
	}
	for _, m := range meta {
		v, _ := f.Meta(m)
		cells = append(cells, v)
	}
	return cells
}

// relDirParts returns the path components of abs's directory relative to
// absRoot ("video", "video/clips", or nil for a file directly in the root).
func relDirParts(absRoot, abs string) []string {
	rel, err := filepath.Rel(absRoot, abs)
	if err != nil {
		return nil
	}
	dir := filepath.Dir(rel)
	if dir == "." || dir == "" {
		return nil
	}
	var parts []string
	for _, p := range strings.Split(filepath.ToSlash(dir), "/") {
		if p != "" && p != "." {
			parts = append(parts, p)
		}
	}
	return parts
}

func rootLabel(absRoot string) string {
	base := filepath.Base(absRoot)
	if base == "." || base == string(filepath.Separator) || base == "" {
		return absRoot + "/"
	}
	return base + "/"
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
