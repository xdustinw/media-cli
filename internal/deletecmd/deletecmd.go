// Package deletecmd implements `mc delete`: remove the files under one or more
// folders that match a mandatory --select filter.
package deletecmd

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"sort"
	"time"

	"github.com/xdustinw/media-cli/internal/media"
	"github.com/xdustinw/media-cli/internal/query"
	"github.com/xdustinw/media-cli/internal/toon"
)

// Options configures a Run.
type Options struct {
	Folders   []string
	Select    string // required
	Recursive bool

	AssumeYes bool
	Stdout    io.Writer
	Stderr    io.Writer
	Confirm   func(prompt string) (bool, error)
	Logger    *slog.Logger
}

type target struct {
	path string
	rel  string
	size int64
}

// Run resolves the matching files, previews them and (on confirmation) deletes
// them.
func Run(ctx context.Context, o Options) error {
	log := o.Logger
	if log == nil {
		log = slog.Default()
	}
	if o.Select == "" {
		return fmt.Errorf("--select is required for delete")
	}
	if len(o.Folders) == 0 {
		o.Folders = []string{"."}
	}

	sel, err := query.ParseSelect(o.Select)
	if err != nil {
		return fmt.Errorf("--select: %w", err)
	}
	if sel == nil {
		return fmt.Errorf("--select is required for delete")
	}

	var targets []target
	seen := map[string]struct{}{}
	for _, folder := range o.Folders {
		files, ferr := media.WalkFiles(ctx, folder, o.Recursive)
		if ferr != nil {
			return fmt.Errorf("reading %s: %w", folder, ferr)
		}
		base := media.DisplayBase(folder)
		for _, f := range files {
			if _, dup := seen[f]; dup {
				continue
			}
			facts, e := media.StatFacts(f)
			if e != nil || !sel.Match(facts) {
				continue
			}
			seen[f] = struct{}{}
			targets = append(targets, target{path: f, rel: media.RelTo(base, f), size: facts.Size})
		}
	}
	sort.Slice(targets, func(i, j int) bool { return targets[i].path < targets[j].path })

	if len(targets) == 0 {
		fmt.Fprintf(o.Stdout, "No files match %q.\n", o.Select)
		return nil
	}

	doc := &toon.Document{}
	doc.AddField("select", o.Select)
	tbl := toon.Table{Name: "delete", Columns: []string{"file", "size"}}
	for _, t := range targets {
		tbl.Rows = append(tbl.Rows, []string{t.rel, media.HumanSize(t.size)})
	}
	doc.AddTable(tbl)
	fmt.Fprint(o.Stdout, doc.String())

	if !o.AssumeYes {
		ok := false
		if o.Confirm != nil {
			var cErr error
			ok, cErr = o.Confirm(fmt.Sprintf("\nDelete %d file(s)? [y/N] ", len(targets)))
			if cErr != nil {
				return cErr
			}
		}
		if !ok {
			fmt.Fprintln(o.Stdout, "Aborted; nothing deleted.")
			return nil
		}
	}

	start := time.Now()
	var removed, failed int
	var bytes int64
	for _, t := range targets {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := os.Remove(t.path); err != nil {
			failed++
			fmt.Fprintf(o.Stderr, "  ! %s: %v\n", t.rel, err)
			continue
		}
		removed++
		bytes += t.size
		fmt.Fprintf(o.Stdout, "  ✓ deleted %s\n", t.rel)
		log.Info("delete", "file", t.path)
	}

	fmt.Fprintln(o.Stderr, media.Summary(removed, bytes, time.Since(start)))
	if failed > 0 {
		return fmt.Errorf("%d file(s) could not be deleted", failed)
	}
	return nil
}
