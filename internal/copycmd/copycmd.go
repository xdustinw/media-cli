// Package copycmd implements `mc copy` and `mc move`: bring every file from a
// source (file or folder) into a target folder, resolving content duplicates
// first.
//
// A "duplicate" is decided purely from the file name: the source file carries a
// ".<6-hex>" short hash (written by `mc hash`) that some file already in the
// target — anywhere under it — also carries. For each one the user chooses to
// overwrite the target's bytes, skip the source, or rename the target file to
// the source's name. Files with no name collision are copied/moved into the
// target under their path relative to the source.
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
	"github.com/xdustinw/media-cli/internal/toon"
)

// hashLen is the short-hash length `mc hash` embeds in file names.
const hashLen = 6

// Mode is how a duplicate is resolved.
type Mode string

const (
	ModeAsk           Mode = ""               // prompt per duplicate
	ModeOverwrite     Mode = "overwrite"      // copy source bytes over the target file
	ModeSkipDuplicate Mode = "skip-duplicate" // leave the target, do not bring the source
	ModeRename        Mode = "rename"         // rename the target file to the source's name
)

// ParseMode accepts the short or long spelling (o/overwrite, s/skip-duplicate,
// r/rename); "" stays ModeAsk.
func ParseMode(s string) (Mode, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "":
		return ModeAsk, nil
	case "o", "overwrite":
		return ModeOverwrite, nil
	case "s", "skip", "skip-duplicate":
		return ModeSkipDuplicate, nil
	case "r", "rename":
		return ModeRename, nil
	default:
		return "", fmt.Errorf("unknown --mode %q (want overwrite|skip-duplicate|rename)", s)
	}
}

// Options configures a Run.
type Options struct {
	Source string
	Target string
	Move   bool // false = copy
	Mode   Mode // "" => ask per duplicate (or overwrite under AssumeYes)

	AssumeYes bool
	Stdout    io.Writer
	Stderr    io.Writer
	Confirm   func(prompt string) (bool, error) // final go/no-go
	Ask       func(prompt string) (Mode, error) // per-duplicate choice
	Logger    *slog.Logger
}

func (o Options) verb() string {
	if o.Move {
		return "move"
	}
	return "copy"
}

type plan struct {
	src    string // absolute source path
	rel    string // path relative to the source root (display + destination)
	dst    string // computed destination (plain entries only)
	dup    bool
	tgt    string // matching target file (duplicates only)
	tgtRel string
	mode   Mode // resolved action (duplicates only)
}

// Run resolves the file list, previews the plan and (on confirmation) applies
// it.
func Run(ctx context.Context, o Options) error {
	log := o.Logger
	if log == nil {
		log = slog.Default()
	}

	absSource, _ := filepath.Abs(o.Source)
	absTarget, _ := filepath.Abs(o.Target)
	if absSource == absTarget {
		return fmt.Errorf("source and target are the same path")
	}

	srcFiles, err := media.WalkFiles(ctx, o.Source)
	if err != nil {
		return fmt.Errorf("reading source: %w", err)
	}
	srcBase := media.DisplayBase(o.Source)

	// Index the target's existing short hashes (name-based, any depth).
	targetFiles, terr := media.WalkFiles(ctx, o.Target)
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

	var plains, dups []plan
	var skipConflicts int
	for _, sf := range srcFiles {
		if err := ctx.Err(); err != nil {
			return err
		}
		rel := media.RelTo(srcBase, sf)
		if h := media.ShortHashInName(sf, hashLen); h != "" {
			if tf, ok := byHash[h]; ok {
				dups = append(dups, plan{
					src: sf, rel: rel, dup: true,
					tgt: tf, tgtRel: media.RelTo(absTarget, tf),
				})
				continue
			}
		}
		dst := filepath.Join(o.Target, rel)
		if _, err := statFile(dst); err == nil {
			fmt.Fprintf(o.Stderr, "  ! %s: already exists at destination and is not a hash duplicate; skipped\n", rel)
			skipConflicts++
			continue
		}
		plains = append(plains, plan{src: sf, rel: rel, dst: dst})
	}

	if len(plains) == 0 && len(dups) == 0 {
		fmt.Fprintln(o.Stdout, "Nothing to do.")
		return conflictErr(skipConflicts)
	}

	// Resolve each duplicate's action.
	for i := range dups {
		mode, rerr := o.resolve(&dups[i])
		if rerr != nil {
			return rerr
		}
		dups[i].mode = mode
	}

	preview(o, plains, dups)

	if !o.AssumeYes {
		ok := false
		if o.Confirm != nil {
			var cErr error
			ok, cErr = o.Confirm(fmt.Sprintf("\nProceed with %s? [y/N] ", o.verb()))
			if cErr != nil {
				return cErr
			}
		}
		if !ok {
			fmt.Fprintln(o.Stdout, "Aborted; nothing changed.")
			return conflictErr(skipConflicts)
		}
	}

	start := time.Now()
	var done, skipped int
	var bytes int64
	var applyErrs int
	all := append(append([]plan{}, plains...), dups...)
	for _, p := range all {
		if err := ctx.Err(); err != nil {
			return err
		}
		if p.dup && p.mode == ModeSkipDuplicate {
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
		log.Info(o.verb(), "src", p.src, "dup", p.dup, "mode", string(p.mode))
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

// resolve picks the Mode for one duplicate.
func (o Options) resolve(p *plan) (Mode, error) {
	if o.Mode != ModeAsk {
		return o.Mode, nil
	}
	if o.AssumeYes {
		return ModeOverwrite, nil
	}
	if o.Ask == nil {
		return ModeSkipDuplicate, nil
	}
	return o.Ask(fmt.Sprintf("  duplicate: %s  <->  %s\n    [o]verwrite target / [s]kip source / [r]ename target ? ",
		p.rel, p.tgtRel))
}

func preview(o Options, plains, dups []plan) {
	doc := &toon.Document{}
	doc.AddField("operation", o.verb())
	doc.AddField("source", o.Source)
	doc.AddField("target", o.Target)

	if len(plains) > 0 {
		t := toon.Table{Name: o.verb(), Columns: []string{"from", "to"}}
		for _, p := range plains {
			t.Rows = append(t.Rows, []string{p.rel, filepath.Join(filepath.Base(o.Target), p.rel)})
		}
		doc.AddTable(t)
	}
	if len(dups) > 0 {
		t := toon.Table{Name: "duplicates", Columns: []string{"source", "target", "action"}}
		for _, p := range dups {
			t.Rows = append(t.Rows, []string{p.rel, p.tgtRel, dupAction(o, p)})
		}
		doc.AddTable(t)
	}
	fmt.Fprint(o.Stdout, doc.String())
}

// dupAction is the human phrase shown in the preview for a resolved duplicate.
func dupAction(o Options, p plan) string {
	switch p.mode {
	case ModeOverwrite:
		s := "overwrite target bytes"
		if o.Move {
			s += ", delete source"
		}
		return s
	case ModeRename:
		s := "rename target -> " + filepath.Base(p.src)
		if o.Move {
			s += ", delete source"
		}
		return s
	default:
		return "skip source"
	}
}

// apply performs one plan entry and returns the number of bytes written.
func apply(o Options, p plan) (int64, error) {
	if !p.dup {
		size := fileSize(p.src)
		if o.Move {
			return size, media.MoveFile(p.src, p.dst)
		}
		return size, media.CopyFile(p.src, p.dst)
	}

	switch p.mode {
	case ModeOverwrite:
		size := fileSize(p.src)
		if err := media.CopyFile(p.src, p.tgt); err != nil {
			return 0, err
		}
		if o.Move {
			return size, removeFile(p.src)
		}
		return size, nil

	case ModeRename:
		newName := filepath.Join(filepath.Dir(p.tgt), filepath.Base(p.src))
		if newName != p.tgt {
			if _, err := statFile(newName); err == nil {
				return 0, fmt.Errorf("cannot rename target to %s: already exists", filepath.Base(newName))
			}
			if err := renameFile(p.tgt, newName); err != nil {
				return 0, err
			}
		}
		if o.Move {
			return 0, removeFile(p.src)
		}
		return 0, nil

	default: // ModeSkipDuplicate
		return 0, nil
	}
}

func conflictErr(n int) error {
	if n > 0 {
		return fmt.Errorf("%d file(s) skipped (destination path already in use)", n)
	}
	return nil
}
