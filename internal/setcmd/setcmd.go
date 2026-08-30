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
	"sort"
	"strings"
	"time"

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
	Recursive  bool

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

	files, err := media.Discover(ctx, o.Target, o.Extensions, o.Recursive)
	if err != nil {
		return err
	}
	base := media.DisplayBase(o.Target)
	log.Info("scanning", "target", o.Target, "files", len(files), "select", o.Select)
	start := time.Now()

	// Probe cheaply while filtering; only pay for a deep probe when --select
	// actually references a field that needs one (image EXIF etc.).
	deepFilter := mediainfo.AnyFieldNeedsDeepProbe(sel.Fields())

	var targets []*mediainfo.File
	var scanErrs int
	for _, f := range files {
		if err := ctx.Err(); err != nil {
			return err
		}
		mf, err := mediainfo.Load(f, deepFilter)
		if err != nil {
			scanErrs++
			fmt.Fprintf(o.Stderr, "  ! %s: %v\n", media.RelTo(base, f), err)
			continue
		}
		if !sel.Match(mf) {
			continue
		}
		// For the preview's "current value" column, an image's EXIF needs a
		// deep probe (cheap for images); a video's tags are already in the
		// shallow probe's container metadata.
		if !deepFilter && mf.Kind == media.KindImage {
			if deep, derr := mediainfo.Load(f, true); derr == nil {
				mf = deep
			}
		}
		targets = append(targets, mf)
	}

	scanElapsed := time.Since(start)

	if len(targets) == 0 {
		fmt.Fprintf(o.Stdout, "No files match; nothing to set.\n")
		fmt.Fprintln(o.Stderr, media.Summary(len(files), 0, scanElapsed))
		return summaryErr(scanErrs)
	}

	var bytesProcessed int64
	for _, f := range targets {
		bytesProcessed += f.Size
	}

	// Preview: per file, "key: <current> -> <new>" for each tag.
	fmt.Fprintf(o.Stdout, "Set  %s  on %d file(s):\n", o.Tags, len(targets))
	for _, f := range targets {
		fmt.Fprintf(o.Stdout, "  %s\n", media.RelTo(base, f.Path))
		for _, p := range o.Tags {
			cur, ok := f.Meta(p.Key)
			from := "(unset)"
			if ok {
				from = fmt.Sprintf("%q", cur)
			}
			fmt.Fprintf(o.Stdout, "      %s: %s -> %q\n", p.Key, from, p.Value)
		}
	}

	// MP4/MOV metadata is written as iTunes-style atoms so Windows can read it;
	// keys outside that vocabulary are silently dropped by the muxer. Warn once.
	if dropped := unsupportedMP4Keys(targets, o.Tags); len(dropped) > 0 {
		fmt.Fprintf(o.Stderr, "  ! MP4/MOV keeps only standard fields; %s will not be stored on those files\n",
			strings.Join(dropped, ", "))
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
			fmt.Fprintln(o.Stderr, media.Summary(len(targets), bytesProcessed, scanElapsed))
			return summaryErr(scanErrs)
		}
	}

	applyStart := time.Now()
	var applyErrs int
	for _, f := range targets {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := apply(f, o.Tags); err != nil {
			applyErrs++
			fmt.Fprintf(o.Stderr, "  ! %s: %v\n", media.RelTo(base, f.Path), err)
			continue
		}
		log.Info("set", "file", f.Abs, "tags", o.Tags.String())
		fmt.Fprintf(o.Stdout, "  ✓ %s\n", media.RelTo(base, f.Path))
	}

	fmt.Fprintln(o.Stderr, media.Summary(len(targets), bytesProcessed, scanElapsed+time.Since(applyStart)))

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
		// Write standard iTunes-style atoms (no mdta freeform box) so the
		// fields are visible to Windows Explorer and QuickTime.
		werr = ffmpeg.WriteTags(f.Abs, tmp, tags.Keys(), tags.Values(), false)
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

// mp4RetainedKeys are the global metadata keys FFmpeg's mp4/mov muxer keeps in
// the iTunes-style ilst atom (see libavformat/movenc.c mov_write_ilst_tag).
// Anything else is dropped when metadata is not written as freeform mdta.
var mp4RetainedKeys = map[string]bool{
	"title": true, "artist": true, "album_artist": true, "composer": true,
	"album": true, "date": true, "comment": true, "genre": true,
	"copyright": true, "grouping": true, "lyrics": true, "description": true,
	"synopsis": true, "show": true, "episode_id": true, "network": true,
	"keywords": true, "encoding_tool": true,
}

// unsupportedMP4Keys returns the sorted, de-duplicated tag keys that would be
// lost when written onto any MP4/MOV file in targets.
func unsupportedMP4Keys(targets []*mediainfo.File, tags tag.Set) []string {
	hasMP4 := false
	for _, f := range targets {
		if f.Kind != media.KindVideo {
			continue
		}
		c := f.ContainerFormat()
		if strings.Contains(c, "mp4") || strings.Contains(c, "mov") ||
			strings.Contains(c, "m4a") || strings.Contains(c, "3gp") {
			hasMP4 = true
			break
		}
	}
	if !hasMP4 {
		return nil
	}
	var out []string
	seen := map[string]bool{}
	for _, p := range tags {
		k := strings.ToLower(p.Key)
		if !mp4RetainedKeys[k] && !seen[k] {
			seen[k] = true
			out = append(out, p.Key)
		}
	}
	sort.Strings(out)
	return out
}

func summaryErr(scanErrs int) error {
	if scanErrs > 0 {
		return fmt.Errorf("%d file(s) could not be read", scanErrs)
	}
	return nil
}
