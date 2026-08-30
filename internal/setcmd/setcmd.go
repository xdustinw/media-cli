// Package setcmd implements `mc set`: write chosen metadata key/values onto the
// media files in a folder that match a --select filter.
package setcmd

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/xdustinw/media-cli/internal/ffmpeg"
	"github.com/xdustinw/media-cli/internal/imgmeta"
	"github.com/xdustinw/media-cli/internal/media"
	"github.com/xdustinw/media-cli/internal/mediainfo"
	"github.com/xdustinw/media-cli/internal/query"
	"github.com/xdustinw/media-cli/internal/tag"
)

// Options configures a Run.
type Options struct {
	Target     string
	Tags       tag.Set
	Select     string
	Extensions []string
	AssumeYes  bool

	Stdout  io.Writer
	Stderr  io.Writer
	Confirm func(prompt string) (bool, error)
	Logger  *slog.Logger
}

// Run selects matching files, previews the metadata change and (on
// confirmation) applies it.
func Run(ctx context.Context, o Options) error {
	log := o.Logger
	if log == nil {
		log = slog.Default()
	}
	if len(o.Tags) == 0 {
		return fmt.Errorf("no key=value pairs given")
	}

	sel, err := query.ParseSelect(o.Select)
	if err != nil {
		return fmt.Errorf("--select: %w", err)
	}

	files, err := media.Discover(ctx, o.Target, o.Extensions)
	if err != nil {
		return err
	}
	base := media.DisplayBase(o.Target)
	log.Info("scanning", "target", o.Target, "files", len(files), "select", o.Select)

	var targets []*mediainfo.File
	var scanErrs int
	for _, f := range files {
		if err := ctx.Err(); err != nil {
			return err
		}
		mf, err := mediainfo.Load(f, true)
		if err != nil {
			scanErrs++
			fmt.Fprintf(o.Stderr, "  ! %s: %v\n", media.RelTo(base, f), err)
			continue
		}
		if sel.Match(mf) {
			targets = append(targets, mf)
		}
	}

	if len(targets) == 0 {
		fmt.Fprintf(o.Stdout, "No files match; nothing to set.\n")
		return summaryErr(scanErrs)
	}

	// Preview: per file, "key: <current> -> <new>" for each tag.
	fmt.Fprintf(o.Stdout, "Set  %s  on %d file(s):\n", o.Tags, len(targets))
	for _, f := range targets {
		fmt.Fprintf(o.Stdout, "  %s\n", media.RelTo(base, f.Abs))
		for _, p := range o.Tags {
			cur, ok := f.Meta(p.Key)
			from := "(unset)"
			if ok {
				from = fmt.Sprintf("%q", cur)
			}
			fmt.Fprintf(o.Stdout, "      %s: %s -> %q\n", p.Key, from, p.Value)
		}
	}

	if !o.AssumeYes {
		ok := false
		if o.Confirm != nil {
			var cErr error
			ok, cErr = o.Confirm(fmt.Sprintf("\nUpdate metadata on %d file(s)? [y/N] ", len(targets)))
			if cErr != nil {
				return cErr
			}
		}
		if !ok {
			fmt.Fprintln(o.Stdout, "Aborted; no files changed.")
			return summaryErr(scanErrs)
		}
	}

	var applyErrs int
	for _, f := range targets {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := apply(f, o.Tags); err != nil {
			applyErrs++
			fmt.Fprintf(o.Stderr, "  ! %s: %v\n", media.RelTo(base, f.Abs), err)
			continue
		}
		log.Info("set", "file", f.Abs, "tags", o.Tags.String())
		fmt.Fprintf(o.Stdout, "  ✓ %s\n", media.RelTo(base, f.Abs))
	}

	if applyErrs > 0 {
		return fmt.Errorf("%d file(s) failed to update", applyErrs)
	}
	return summaryErr(scanErrs)
}

// apply writes the tags into a sibling temp file, then moves it over the
// original. Video is remuxed with stream copy; images keep their pixel data.
func apply(f *mediainfo.File, tags tag.Set) error {
	tmp := filepath.Join(filepath.Dir(f.Abs),
		fmt.Sprintf(".mc-set-%d%s", os.Getpid(), filepath.Ext(f.Abs)))
	_ = os.Remove(tmp)

	var werr error
	switch f.Kind {
	case media.KindVideo:
		werr = ffmpeg.WriteTags(f.Abs, tmp, tags.Keys(), tags.Values())
	case media.KindImage:
		if !imgmeta.Supported(f.Abs) {
			return fmt.Errorf("setting metadata on %s images is not supported", filepath.Ext(f.Abs))
		}
		werr = imgmeta.WriteMany(f.Abs, tmp, tags.Keys(), tags.Values())
	default:
		return fmt.Errorf("unsupported file type")
	}
	if werr != nil {
		_ = os.Remove(tmp)
		return werr
	}

	if err := media.SwapInPlace(tmp, f.Abs, true); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

func summaryErr(scanErrs int) error {
	if scanErrs > 0 {
		return fmt.Errorf("%d file(s) could not be read", scanErrs)
	}
	return nil
}
