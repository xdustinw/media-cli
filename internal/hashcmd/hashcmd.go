// Package hashcmd implements the business logic behind `mc hash`: compute a
// metadata-independent MD5 for each media file, then (on confirmation) persist
// it as a freeform tag and rename the file.
//
// Video/audio files are hashed over their encoded video+audio elementary
// streams (container metadata excluded). Still images are hashed over their
// decoded pixels (EXIF/XMP/ICC/text excluded). Either way, editing metadata
// does not change the hash.
//
// The preview of each file's hash and planned rename is printed as that file is
// processed (rather than as one batch afterwards) so progress is visible on
// large folders.
package hashcmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/xdustinw/media-cli/internal/ffmpeg"
	"github.com/xdustinw/media-cli/internal/imgmeta"
	"github.com/xdustinw/media-cli/internal/media"
)

// Options configures a Run.
type Options struct {
	Target      string // file or directory (required)
	Extensions  []string
	MetadataKey string // freeform tag key, e.g. "mc.hash"
	NameLength  int    // hash prefix length used in the renamed filename
	AssumeYes   bool   // skip the confirmation prompt
	Force       bool   // re-hash files that already carry the tag (and compare)

	Stdout  io.Writer
	Stderr  io.Writer
	Confirm func(prompt string) (bool, error) // nil => treated as "no"
	Logger  *slog.Logger
}

type action int

const (
	actionSkip  action = iota // already tagged and named
	actionTag                 // write tag + rename
	actionRetag               // rewrite tag, name already correct
)

type item struct {
	path        string // current absolute/clean path
	rel         string // display path relative to the target
	kind        media.Kind
	tag         tagger
	hash        string
	prefix      string
	newPath     string
	existingTag string // current mc.hash value in the file, "" if none
	hashChanged bool   // a mc.hash tag exists but no longer matches the content
	trusted     bool   // hash taken from the existing tag, not recomputed
	act         action
}

// Run executes the hash workflow. It returns an error only for fatal problems;
// per-file failures are reported and folded into a non-nil summary at the end.
func Run(ctx context.Context, o Options) error {
	log := o.Logger
	if log == nil {
		log = slog.Default()
	}
	if o.NameLength <= 0 || o.NameLength > 32 {
		o.NameLength = 6
	}
	if o.MetadataKey == "" {
		o.MetadataKey = "mc.hash"
	}

	files, err := media.Discover(ctx, o.Target, o.Extensions)
	if err != nil {
		return err
	}
	base := media.DisplayBase(o.Target)
	log.Info("scanning", "target", o.Target, "files", len(files))

	// Preview each file the moment it is hashed, so progress is visible.
	fmt.Fprintf(o.Stdout, "Preview (%s):\n", o.MetadataKey)

	items := make([]item, 0, len(files))
	var scanErrs int
	for _, f := range files {
		if err := ctx.Err(); err != nil {
			return err
		}
		kind := media.KindOf(f)
		tg, err := taggerFor(kind)
		if err != nil {
			scanErrs++
			fmt.Fprintf(o.Stderr, "  ! %s: %v\n", media.RelTo(base, f), err)
			continue
		}

		// Fast path: trust an existing, well-formed mc.hash tag and skip the
		// (expensive) hashing entirely, unless --force asks to re-verify.
		var h string
		var trusted bool
		if !o.Force {
			if v, terr := tg.ReadTag(f, o.MetadataKey); terr == nil && looksLikeHash(v) {
				h, trusted = v, true
			}
		}
		if !trusted {
			log.Debug("hashing", "file", f, "kind", kind.String())
			hh, herr := tg.Hash(f)
			if herr != nil {
				scanErrs++
				fmt.Fprintf(o.Stderr, "  ! %s: %v\n", media.RelTo(base, f), herr)
				continue
			}
			h = hh
		}

		it := item{
			path:    f,
			rel:     media.RelTo(base, f),
			kind:    kind,
			tag:     tg,
			hash:    h,
			prefix:  h[:o.NameLength],
			trusted: trusted,
		}
		if trusted {
			it.existingTag = h // avoids a second ReadTag in classify
		}
		if media.AlreadyTagged(f, it.prefix) {
			it.newPath = f // filename already carries the right short hash
		} else {
			it.newPath = media.HashedName(f, it.prefix)
		}
		classify(o.MetadataKey, &it)
		items = append(items, it)

		fmt.Fprintln(o.Stdout, previewLine(base, o.MetadataKey, it))
	}

	if len(items) == 0 {
		return fmt.Errorf("no files could be hashed (%d error(s))", scanErrs)
	}

	pending := filterPending(items)
	skipped := len(items) - len(pending)

	fmt.Fprintf(o.Stdout, "\n%d file(s) — %d to update, %d up to date", len(items), len(pending), skipped)
	if scanErrs > 0 {
		fmt.Fprintf(o.Stdout, ", %d error(s)", scanErrs)
	}
	fmt.Fprintln(o.Stdout)

	if len(pending) == 0 {
		fmt.Fprintln(o.Stdout, "Nothing to do.")
		return summaryErr(scanErrs)
	}

	// Confirmation, unless -y.
	if !o.AssumeYes {
		ok := false
		if o.Confirm != nil {
			var cErr error
			ok, cErr = o.Confirm(fmt.Sprintf("Write '%s' metadata and rename %d file(s)? [y/N] ", o.MetadataKey, len(pending)))
			if cErr != nil {
				return cErr
			}
		}
		if !ok {
			fmt.Fprintln(o.Stdout, "Aborted; no files changed.")
			return summaryErr(scanErrs)
		}
	}

	// Apply.
	var applyErrs int
	for _, it := range pending {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := apply(o.MetadataKey, it); err != nil {
			applyErrs++
			fmt.Fprintf(o.Stderr, "  ! %s: %v\n", it.rel, err)
			continue
		}
		log.Info("tagged", "file", it.newPath, "hash", it.hash)
		fmt.Fprintf(o.Stdout, "  ✓ %s  ->  %s\n", it.rel, media.RelTo(base, it.newPath))
	}

	if applyErrs > 0 {
		return fmt.Errorf("%d file(s) failed to update", applyErrs)
	}
	return summaryErr(scanErrs)
}

// classify inspects the file's current tag and name and fills in it.existingTag,
// it.hashChanged and it.act.
func classify(key string, it *item) {
	if it.existingTag == "" {
		if v, err := it.tag.ReadTag(it.path, key); err == nil {
			it.existingTag = v
		}
	}
	tagMatches := it.existingTag == it.hash
	it.hashChanged = it.existingTag != "" && !tagMatches

	namedRight := media.AlreadyTagged(it.path, it.prefix)
	switch {
	case tagMatches && namedRight:
		// Tag and filename both already reflect this content: nothing to do.
		it.act = actionSkip
	case namedRight:
		it.act = actionRetag
	default:
		it.act = actionTag
	}
}

func filterPending(items []item) []item {
	out := make([]item, 0, len(items))
	for _, it := range items {
		if it.act != actionSkip {
			out = append(out, it)
		}
	}
	return out
}

// previewLine is the one line printed for a file the moment it is processed,
// e.g. "  + video/a.mp4  <hash>  ->  video/a.8f9b6e.mp4". Leading glyph:
//
//	=  already tagged and named — nothing to do
//	»  tag already correct, only the filename is stale (plain rename)
//	~  mc.hash present but stale — rewritten
//	+  no mc.hash yet — write it (and rename)
func previewLine(base, key string, it item) string {
	renameOnly := it.act != actionSkip && it.existingTag == it.hash
	glyph := "+"
	switch {
	case it.act == actionSkip:
		glyph = "="
	case renameOnly:
		glyph = "»"
	case it.act == actionRetag:
		glyph = "~"
	}

	line := fmt.Sprintf("  %s %s  %s", glyph, it.rel, it.hash)
	switch {
	case it.act == actionSkip:
		// already tagged and named — nothing more to say
	case it.newPath != it.path:
		line += "  ->  " + media.RelTo(base, it.newPath)
	default:
		line += "  (tag only)"
	}
	if it.hashChanged {
		line += fmt.Sprintf("   (stale %s %s replaced)", key, short(it.existingTag))
	}
	return line
}

// short returns the first 8 characters of a hex hash for display.
func short(h string) string {
	if len(h) > 8 {
		return h[:8] + "…"
	}
	return h
}

// looksLikeHash reports whether s is a 32-character lowercase-hex MD5 digest.
func looksLikeHash(s string) bool {
	if len(s) != 32 {
		return false
	}
	for _, c := range s {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

// apply writes the tag into a sibling temp file, then moves it into place and
// removes the original when the name changes.
func apply(key string, it item) error {
	// Fast path: the file already carries the exact tag we would write, so only
	// the name is out of date — a plain rename, no remux / metadata rewrite.
	if it.existingTag == it.hash && it.newPath != it.path {
		return media.SwapInPlace(it.path, it.newPath, false)
	}

	tmp := filepath.Join(filepath.Dir(it.path),
		fmt.Sprintf(".mc-%s%s", it.prefix, filepath.Ext(it.path)))
	_ = os.Remove(tmp)

	if err := it.tag.WriteTag(it.path, tmp, key, it.hash); err != nil {
		_ = os.Remove(tmp)
		return err
	}

	overwrite := it.newPath == it.path
	if err := media.SwapInPlace(tmp, it.newPath, overwrite); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if it.newPath != it.path {
		if err := os.Remove(it.path); err != nil {
			return fmt.Errorf("new file written (%s) but removing original failed: %w",
				filepath.Base(it.newPath), err)
		}
	}
	return nil
}

func summaryErr(scanErrs int) error {
	if scanErrs > 0 {
		return fmt.Errorf("%d file(s) could not be hashed", scanErrs)
	}
	return nil
}

// tagger abstracts hashing and freeform-tag I/O for one media kind.
type tagger interface {
	Hash(path string) (string, error)
	ReadTag(path, key string) (string, error) // returns errTagAbsent when unset
	WriteTag(src, dst, key, value string) error
}

var errTagAbsent = errors.New("tag not present")

func taggerFor(k media.Kind) (tagger, error) {
	switch k {
	case media.KindVideo:
		return videoTagger{}, nil
	case media.KindImage:
		return imageTagger{}, nil
	default:
		return nil, fmt.Errorf("unsupported file type")
	}
}

type videoTagger struct{}

func (videoTagger) Hash(p string) (string, error) { return ffmpeg.StreamHash(p) }

func (videoTagger) ReadTag(p, key string) (string, error) {
	v, err := ffmpeg.ReadTag(p, key)
	if errors.Is(err, ffmpeg.ErrTagAbsent) {
		return "", errTagAbsent
	}
	return v, err
}

func (videoTagger) WriteTag(src, dst, key, value string) error {
	return ffmpeg.WriteTag(src, dst, key, value)
}

type imageTagger struct{}

func (imageTagger) Hash(p string) (string, error) { return ffmpeg.ImageHash(p) }

func (imageTagger) ReadTag(p, key string) (string, error) {
	v, err := imgmeta.Read(p, key)
	if errors.Is(err, imgmeta.ErrTagAbsent) {
		return "", errTagAbsent
	}
	return v, err
}

func (imageTagger) WriteTag(src, dst, key, value string) error {
	return imgmeta.Write(src, dst, key, value)
}
