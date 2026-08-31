// Package copycmd implements `mc copy` and `mc move`: bring every file from one
// or more sources (files or folders) into a target folder, resolving content
// duplicates first.
//
// A "duplicate" is decided purely from the file name: the source file carries a
// ".<6-hex>" short hash (written by `mc hash`) that some file already anywhere
// under the target also carries. The --mode flag says what to do with every
// such match: skip-duplicate (default), overwrite, or keep-both. Files with no
// name collision are copied/moved into the target under their path relative to
// their source.
package copycmd

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"time"

	"github.com/xdustinw/media-cli/internal/media"
	"github.com/xdustinw/media-cli/internal/query"
	"github.com/xdustinw/media-cli/internal/toon"
)

// hashLen is the short-hash length `mc hash` embeds in file names.
const hashLen = 6

// Mode is how a duplicate is resolved.
type Mode string

const (
	ModeSkipDuplicate Mode = "skip-duplicate" // default: leave the target, keep the source where it is
	ModeOverwrite     Mode = "overwrite"      // copy the source bytes over the matching target file
	ModeKeepBoth      Mode = "keep-both"      // bring the source in too, under its own path
)

// ParseMode accepts the short or long spelling (s/skip-duplicate, o/overwrite,
// k/keep-both); "" is the default, skip-duplicate.
func ParseMode(s string) (Mode, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "s", "skip", "skip-duplicate":
		return ModeSkipDuplicate, nil
	case "o", "overwrite":
		return ModeOverwrite, nil
	case "k", "keep-both", "keepboth":
		return ModeKeepBoth, nil
	default:
		return "", fmt.Errorf("unknown --mode %q (want skip-duplicate|overwrite|keep-both)", s)
	}
}

// Options configures a Run.
type Options struct {
	Sources   []string
	Target    string
	Move      bool // false = copy
	Mode      Mode // "" => ModeSkipDuplicate
	Select    string
	Recursive bool // descend into source subfolders

	AssumeYes bool
	Stdout    io.Writer
	Stderr    io.Writer
	Confirm   func(prompt string) (bool, error)
	Logger    *slog.Logger
}

func (o Options) verb() string {
	if o.Move {
		return "move"
	}
	return "copy"
}

func (o Options) verbTitle() string {
	if o.Move {
		return "Move"
	}
	return "Copy"
}

type plan struct {
	src    string // absolute source path
	rel    string // path relative to its source root (display + destination)
	dst    string // computed destination under the target
	dup    bool   // a hash duplicate exists in the target
	tgt    string // the matching target file (duplicates only)
	tgtRel string
}

// Run resolves the file list, previews the plan and (on confirmation) applies
// it.
func Run(ctx context.Context, o Options) error {
	log := o.Logger
	if log == nil {
		log = slog.Default()
	}
	if o.Mode == "" {
		o.Mode = ModeSkipDuplicate
	}
	if len(o.Sources) == 0 {
		return fmt.Errorf("no source given")
	}

	sel, err := query.ParseSelect(o.Select)
	if err != nil {
		return fmt.Errorf("--select: %w", err)
	}

	absTarget, _ := filepath.Abs(o.Target)

	// Index the target's existing short hashes (name-based, at any depth).
	targetFiles, terr := media.WalkFiles(ctx, o.Target, true)
	if terr != nil && !isNotExist(terr) {
		return fmt.Errorf("reading target: %w", terr)
	}
	byHash := map[string]string{}
	for _, tf := range targetFiles {
		if h := media.ShortHashInName(tf, hashLen); h != "" {
			if _, seen := byHash[h]; !seen {
				byHash[h] = tf
			}
		}
	}

	// Collect the source files across every source root.
	type srcFile struct{ path, rel string }
	var srcs []srcFile
	var selected int
	for _, s := range o.Sources {
		absS, _ := filepath.Abs(s)
		if absS == absTarget {
			return fmt.Errorf("source and target are the same path: %s", s)
		}
		files, ferr := media.WalkFiles(ctx, s, o.Recursive)
		if ferr != nil {
			return fmt.Errorf("reading source %s: %w", s, ferr)
		}
		base := media.DisplayBase(s)
		for _, f := range files {
			if sel != nil {
				if facts, e := media.StatFacts(f); e != nil || !sel.Match(facts) {
					continue
				}
				selected++
			}
			srcs = append(srcs, srcFile{path: f, rel: media.RelTo(base, f)})
		}
	}
	if len(srcs) == 0 {
		fmt.Fprintln(o.Stdout, "No source files.")
		return nil
	}

	// With --select, confirm the file list before hashing/comparing.
	if o.Select != "" && !o.AssumeYes {
		fmt.Fprintf(o.Stdout, "%d file(s) match %q:\n", selected, o.Select)
		for _, sf := range srcs {
			fmt.Fprintf(o.Stdout, "  %s\n", sf.rel)
		}
		if !confirmed(o, fmt.Sprintf("%s these %d file(s)? [y/N] ", o.verbTitle(), selected)) {
			fmt.Fprintln(o.Stdout, "Aborted; nothing changed.")
			return nil
		}
	}

	var plains, dups []plan
	var skipConflicts int
	for _, sf := range srcs {
		if err := ctx.Err(); err != nil {
			return err
		}
		p := plan{src: sf.path, rel: sf.rel, dst: filepath.Join(o.Target, sf.rel)}
		if h := media.ShortHashInName(sf.path, hashLen); h != "" {
			if tf, ok := byHash[h]; ok {
				p.dup, p.tgt, p.tgtRel = true, tf, media.RelTo(absTarget, tf)
				dups = append(dups, p)
				continue
			}
		}
		if _, err := statFile(p.dst); err == nil {
			fmt.Fprintf(o.Stderr, "  ! %s: already at the destination (not a hash duplicate); skipped\n", sf.rel)
			skipConflicts++
			continue
		}
		plains = append(plains, p)
	}

	if len(plains) == 0 && len(dups) == 0 {
		fmt.Fprintln(o.Stdout, "Nothing to do.")
		return conflictErr(skipConflicts)
	}

	preview(o, plains, dups)

	if !o.AssumeYes {
		if !confirmed(o, fmt.Sprintf("\nProceed with %s? [y/N] ", o.verb())) {
			fmt.Fprintln(o.Stdout, "Aborted; nothing changed.")
			return conflictErr(skipConflicts)
		}
	}

	start := time.Now()
	var done, skipped int
	var bytes int64
	var applyErrs int
	for _, p := range append(append([]plan{}, plains...), dups...) {
		if err := ctx.Err(); err != nil {
			return err
		}
		if p.dup && o.Mode == ModeSkipDuplicate {
			skipped++
			continue
		}
		n, aerr := apply(o, p)
		if aerr != nil {
			applyErrs++
			fmt.Fprintf(o.Stderr, "  ! %s: %v\n", p.rel, aerr)
			continue
		}
		bytes += n
		done++
		fmt.Fprintf(o.Stdout, "  ✓ %s\n", p.rel)
		log.Info(o.verb(), "src", p.src, "dup", p.dup, "mode", string(o.Mode))
	}

	line := media.Summary(done, bytes, time.Since(start))
	if skipped > 0 {
		line += fmt.Sprintf("; %d duplicate(s) skipped", skipped)
	}
	fmt.Fprintln(o.Stderr, line)
	if applyErrs > 0 {
		return fmt.Errorf("%d file(s) failed", applyErrs)
	}
	return conflictErr(skipConflicts)
}

func preview(o Options, plains, dups []plan) {
	doc := &toon.Document{}
	doc.AddField("operation", o.verb())
	doc.AddField("sources", strings.Join(o.Sources, ", "))
	doc.AddField("target", o.Target)
	doc.AddField("duplicate-mode", string(o.Mode))

	if len(plains) > 0 {
		t := toon.Table{Name: o.verb(), Columns: []string{"from", "to"}}
		for _, p := range plains {
			t.Rows = append(t.Rows, []string{p.rel, filepath.Join(filepath.Base(o.Target), p.rel)})
		}
		doc.AddTable(t)
	}
	if len(dups) > 0 {
		t := toon.Table{Name: "duplicates", Columns: []string{"source", "matches", "action"}}
		for _, p := range dups {
			t.Rows = append(t.Rows, []string{p.rel, p.tgtRel, dupAction(o, p)})
		}
		doc.AddTable(t)
	}
	fmt.Fprint(o.Stdout, doc.String())
}

// dupAction is the human phrase shown in the preview for a duplicate.
func dupAction(o Options, p plan) string {
	switch o.Mode {
	case ModeOverwrite:
		if o.Move {
			return "overwrite target, delete source"
		}
		return "overwrite target"
	case ModeKeepBoth:
		if o.Move {
			return "keep both (move source in as " + filepath.Base(p.dst) + ")"
		}
		return "keep both (copy source in as " + filepath.Base(p.dst) + ")"
	default:
		if o.Move {
			return "skip (source kept, not deleted)"
		}
		return "skip source"
	}
}

// apply performs one plan entry and returns the number of bytes written.
func apply(o Options, p plan) (int64, error) {
	size := fileSize(p.src)

	if !p.dup || o.Mode == ModeKeepBoth {
		// Plain bring-in (keep-both duplicates behave like plain files, landing
		// at their own destination path).
		if _, err := statFile(p.dst); err == nil {
			return 0, fmt.Errorf("destination %s already exists", filepath.Base(p.dst))
		}
		if o.Move {
			return size, media.MoveFile(p.src, p.dst)
		}
		return size, media.CopyFile(p.src, p.dst)
	}

	switch o.Mode {
	case ModeOverwrite:
		if err := media.CopyFile(p.src, p.tgt); err != nil {
			return 0, err
		}
		if o.Move {
			return size, removeFile(p.src)
		}
		return size, nil
	default: // ModeSkipDuplicate — handled by the caller; nothing to do
		return 0, nil
	}
}

// confirmed runs o.Confirm; a nil callback or any error counts as "no".
func confirmed(o Options, prompt string) bool {
	if o.Confirm == nil {
		return false
	}
	ok, err := o.Confirm(prompt)
	return ok && err == nil
}

func conflictErr(n int) error {
	if n > 0 {
		return fmt.Errorf("%d file(s) skipped (destination path already in use)", n)
	}
	return nil
}
