// Package listmissingcmd implements `mc list-missing`: report which files in a
// source folder are absent from one or more target folders, comparing by the
// ".<6-hex>" short hash embedded in each file name (written by `mc hash`).
//
// When files on either side carry no short hash the command offers to hash them
// first (in place); if declined, the comparison falls back to the base file
// name. Nothing is modified except an accepted hash pass — the result is a TOON
// table printed to stdout.
package listmissingcmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/xdustinw/media-cli/internal/media"
	"github.com/xdustinw/media-cli/internal/query"
	"github.com/xdustinw/media-cli/internal/toon"
)

const hashLen = 6

// Options configures a Run.
type Options struct {
	Source    string
	Targets   []string
	Select    string
	Recursive bool // descend into source subfolders (targets are always recursive)

	AssumeYes bool
	Stdout    io.Writer
	Stderr    io.Writer
	Confirm   func(prompt string) (bool, error)
	// PreHash fingerprints and renames the given unhashed files in place (see
	// hashcmd.HashInPlace). When nil the comparison always uses file names.
	PreHash func(ctx context.Context, files []string) (int, error)
	Logger  *slog.Logger
}

type fileRec struct {
	path string
	rel  string
	size int64
}

// Run resolves the source and target file lists, optionally hashes the unhashed
// ones, then prints every source file that no target folder contains.
func Run(ctx context.Context, o Options) error {
	log := o.Logger
	if log == nil {
		log = slog.Default()
	}
	if o.Source == "" {
		return fmt.Errorf("no source folder given")
	}
	if len(o.Targets) == 0 {
		return fmt.Errorf("no target folder given")
	}

	sel, err := query.ParseSelect(o.Select)
	if err != nil {
		return fmt.Errorf("--select: %w", err)
	}

	gather := func() ([]fileRec, []string, error) {
		sfiles, ferr := media.WalkFiles(ctx, o.Source, o.Recursive)
		if ferr != nil {
			return nil, nil, fmt.Errorf("reading source %s: %w", o.Source, ferr)
		}
		base := media.DisplayBase(o.Source)
		var src []fileRec
		for _, f := range sfiles {
			facts, e := media.StatFacts(f)
			if e != nil {
				continue
			}
			if sel != nil && !sel.Match(facts) {
				continue
			}
			src = append(src, fileRec{path: f, rel: media.RelTo(base, f), size: facts.Size})
		}

		seen := map[string]struct{}{}
		var targets []string
		for _, t := range o.Targets {
			tf, terr := media.WalkFiles(ctx, t, true)
			if terr != nil {
				if errors.Is(terr, os.ErrNotExist) {
					fmt.Fprintf(o.Stderr, "  ! target %s does not exist; treated as empty\n", t)
					continue
				}
				return nil, nil, fmt.Errorf("reading target %s: %w", t, terr)
			}
			for _, f := range tf {
				if _, dup := seen[f]; dup {
					continue
				}
				seen[f] = struct{}{}
				targets = append(targets, f)
			}
		}
		return src, targets, nil
	}

	src, targetFiles, err := gather()
	if err != nil {
		return err
	}
	if len(src) == 0 {
		fmt.Fprintln(o.Stdout, "No source files.")
		return nil
	}

	// Offer to hash the files that carry no short hash yet.
	byName := false
	if noHash := unhashed(src, targetFiles); len(noHash) > 0 {
		doHash := o.AssumeYes && o.PreHash != nil
		if !o.AssumeYes && o.PreHash != nil {
			doHash = confirmed(o, fmt.Sprintf(
				"%d file(s) have no content hash. Hash them first? [y/N]  (n = compare by file name) ", len(noHash)))
		}
		if doHash {
			n, herr := o.PreHash(ctx, noHash)
			if herr != nil {
				return fmt.Errorf("hashing unhashed files: %w", herr)
			}
			fmt.Fprintf(o.Stderr, "  hashed %d file(s)\n", n)
			if src, targetFiles, err = gather(); err != nil {
				return err
			}
		} else {
			byName = true
			fmt.Fprintln(o.Stderr, "  comparing by file name (unhashed files not fingerprinted)")
		}
	}

	// Build the set of keys the targets already hold.
	present := map[string]struct{}{}
	for _, tf := range targetFiles {
		if byName {
			present[filepath.Base(tf)] = struct{}{}
			continue
		}
		if h := media.ShortHashInName(tf, hashLen); h != "" {
			present[h] = struct{}{}
		}
	}

	var missing []fileRec
	var skipped int
	for _, sf := range src {
		if err := ctx.Err(); err != nil {
			return err
		}
		var k string
		switch {
		case byName:
			k = filepath.Base(sf.path)
		default:
			if h := media.ShortHashInName(sf.path, hashLen); h != "" {
				k = h
			} else {
				skipped++
				fmt.Fprintf(o.Stderr, "  ? %s: no content hash, skipped\n", sf.rel)
				continue
			}
		}
		if _, ok := present[k]; !ok {
			missing = append(missing, sf)
		}
	}

	sort.Slice(missing, func(i, j int) bool { return missing[i].rel < missing[j].rel })

	doc := &toon.Document{}
	doc.AddField("source", o.Source)
	doc.AddField("targets", strings.Join(o.Targets, ", "))
	if byName {
		doc.AddField("compared-by", "file name")
	} else {
		doc.AddField("compared-by", "content hash")
	}
	doc.AddField("missing", fmt.Sprint(len(missing)))
	if len(missing) > 0 {
		t := toon.Table{Name: "missing", Columns: []string{"file", "hash", "size"}}
		for _, m := range missing {
			t.Rows = append(t.Rows, []string{
				m.rel, media.ShortHashInName(m.path, hashLen), media.HumanSize(m.size),
			})
		}
		doc.AddTable(t)
	}
	fmt.Fprint(o.Stdout, doc.String())

	line := fmt.Sprintf("%d of %d source file(s) missing from the target(s)", len(missing), len(src))
	if skipped > 0 {
		line += fmt.Sprintf("; %d skipped (no hash)", skipped)
	}
	fmt.Fprintln(o.Stderr, line)
	log.Info("list-missing", "source", o.Source, "targets", o.Targets, "missing", len(missing))
	return nil
}

// unhashed returns the source and target paths that carry no short hash slot.
func unhashed(src []fileRec, targetFiles []string) []string {
	var out []string
	for _, sf := range src {
		if media.ShortHashInName(sf.path, hashLen) == "" {
			out = append(out, sf.path)
		}
	}
	for _, tf := range targetFiles {
		if media.ShortHashInName(tf, hashLen) == "" {
			out = append(out, tf)
		}
	}
	return out
}

// confirmed runs o.Confirm; a nil callback or any error counts as "no".
func confirmed(o Options, prompt string) bool {
	if o.Confirm == nil {
		return false
	}
	ok, err := o.Confirm(prompt)
	return ok && err == nil
}
