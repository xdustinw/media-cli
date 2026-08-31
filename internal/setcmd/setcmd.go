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

	// MP4/MOV metadata is normally written as iTunes-style atoms so Windows
	// Explorer and QuickTime read it, but that only keeps a fixed set of
	// standard keys. When a non-standard key is being set, that file's MP4/MOV
	// metadata is written as the freeform "mdta" box instead so nothing is lost.
	freeform := freeformKeys(o.Tags)
	if len(freeform) > 0 && anyMP4Target(targets) {
		fmt.Fprintf(o.Stderr, "  note: %s %s non-standard; MP4/MOV metadata will be written as freeform "+
			"(read by mc / ffmpeg / QuickTime, may not show in Windows Explorer)\n",
			strings.Join(freeform, ", "), plural(len(freeform)))
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
	movFreeform := len(freeform) > 0
	var applyErrs int
	for _, f := range targets {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := apply(f, o.Tags, movFreeform); err != nil {
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
// movFreeform switches MP4/MOV metadata to the freeform mdta box (see Run).
func apply(f *mediainfo.File, tags tag.Set, movFreeform bool) error {
	tmp := filepath.Join(filepath.Dir(f.Abs),
		fmt.Sprintf(".mc-set-%d%s", os.Getpid(), filepath.Ext(f.Abs)))
	_ = os.Remove(tmp)

	var werr error
	switch f.Kind {
	case media.KindVideo:
		// Standard keys go to iTunes-style atoms (Windows/QuickTime read them);
		// a non-standard key forces the freeform mdta box so it is not dropped.
		werr = ffmpeg.WriteTags(f.Abs, tmp, tags.Keys(), tags.Values(), movFreeform)
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

// mp4StandardKeys are the global metadata keys FFmpeg's mp4/mov muxer keeps in
// the iTunes-style ilst atom (see libavformat/movenc.c mov_write_ilst_tag), the
// ones Windows Explorer and QuickTime read. Any other key needs the freeform
// mdta box to survive the remux.
var mp4StandardKeys = map[string]bool{
	"title": true, "artist": true, "album_artist": true, "composer": true,
	"album": true, "date": true, "comment": true, "genre": true,
	"copyright": true, "grouping": true, "lyrics": true, "description": true,
	"synopsis": true, "show": true, "episode_id": true, "network": true,
	"keywords": true, "encoding_tool": true,
}

// freeformKeys returns the sorted, de-duplicated tag keys that are not standard
// MP4/MOV fields — their presence forces the freeform mdta metadata box.
func freeformKeys(tags tag.Set) []string {
	var out []string
	seen := map[string]bool{}
	for _, p := range tags {
		k := strings.ToLower(p.Key)
		if !mp4StandardKeys[k] && !seen[k] {
			seen[k] = true
			out = append(out, p.Key)
		}
	}
	sort.Strings(out)
	return out
}

// anyMP4Target reports whether any target is an MP4/MOV-family video.
func anyMP4Target(targets []*mediainfo.File) bool {
	for _, f := range targets {
		if f.Kind != media.KindVideo {
			continue
		}
		c := f.ContainerFormat()
		if strings.Contains(c, "mp4") || strings.Contains(c, "mov") ||
			strings.Contains(c, "m4a") || strings.Contains(c, "3gp") {
			return true
		}
	}
	return false
}

func plural(n int) string {
	if n == 1 {
		return "is"
	}
	return "are"
}

func summaryErr(scanErrs int) error {
	if scanErrs > 0 {
		return fmt.Errorf("%d file(s) could not be read", scanErrs)
	}
	return nil
}
