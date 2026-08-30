// Package mediainfo joins filesystem facts with FFmpeg probe data into one
// record, and derives the common fields (rating, authors, tags) that `mc list`
// and `mc info` present.
package mediainfo

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf16"

	"github.com/xdustinw/media-cli/internal/ffmpeg"
	"github.com/xdustinw/media-cli/internal/imgmeta"
	"github.com/xdustinw/media-cli/internal/media"
	"github.com/xdustinw/media-cli/internal/query"
)

// File is a single listed/inspected file.
type File struct {
	Path    string // path as supplied (relative for display)
	Abs     string // absolute path
	Name    string // base name
	Size    int64  // bytes
	ModTime time.Time
	Kind    media.Kind
	Probe   *ffmpeg.Probe // nil when the file is not a probeable media file

	imgTags  map[string]string // all imgmeta tags (image files), loaded lazily
	imgTried bool
}

// Load stats path and, when it is a recognised media file, probes it. deep
// controls whether image EXIF / PNG text is decoded (see ffmpeg.Inspect).
func Load(path string, deep bool) (*File, error) {
	st, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	f := &File{
		Path:    path,
		Abs:     abs,
		Name:    filepath.Base(path),
		Size:    st.Size(),
		ModTime: st.ModTime(),
		Kind:    media.KindOf(path),
	}
	if f.Kind != media.KindUnknown {
		if p, perr := ffmpeg.Inspect(path, deep); perr == nil {
			f.Probe = p
		}
	}
	return f, nil
}

// ContainerFormat returns a short container/format name, or "".
func (f *File) ContainerFormat() string {
	if f.Probe == nil {
		return ""
	}
	name := f.Probe.Format.Name
	if i := strings.IndexByte(name, ','); i > 0 {
		name = name[:i]
	}
	return name
}

// Duration returns the media duration, or 0.
func (f *File) Duration() time.Duration {
	if f.Probe == nil {
		return 0
	}
	return f.Probe.Format.Duration
}

// meta looks a key up case-insensitively across container and stream metadata,
// trying the friendly name first and then the raw EXIF tag id.
func (f *File) meta(names ...string) (string, bool) {
	if f.Probe == nil {
		return "", false
	}
	for _, n := range names {
		if v, ok := f.Probe.Meta(n); ok {
			return decodeXP(n, strings.TrimSpace(v)), true
		}
		if tag, ok := exifTagByName[strings.ToLower(n)]; ok {
			if v, ok := f.Probe.Meta(tag); ok {
				return decodeXP(n, strings.TrimSpace(v)), true
			}
		}
	}

	// FFmpeg's image decoders do not expose our imgmeta tags (JPEG COM, PNG
	// tEXt, GIF comment, WebP mcTG), so look there too.
	if f.Kind == media.KindImage {
		for _, n := range names {
			for k, v := range f.imageTags() {
				if strings.EqualFold(k, n) {
					return strings.TrimSpace(v), true
				}
			}
		}
	}
	return "", false
}

// imageTags returns every imgmeta tag on an image file, loaded once.
func (f *File) imageTags() map[string]string {
	if !f.imgTried {
		f.imgTried = true
		if imgmeta.Supported(f.Path) {
			if m, err := imgmeta.ReadAll(f.Path); err == nil {
				f.imgTags = m
			}
		}
	}
	return f.imgTags
}

// ImageTags is imageTags exported for `mc info`.
func (f *File) ImageTags() map[string]string { return f.imageTags() }

// DisplayValue prepares a metadata value for output: Windows XP* tags are
// decoded from their UTF-16LE byte dump, everything else is whitespace-cleaned.
func DisplayValue(key, val string) string {
	if d := decodeXP(FriendlyKey(key), val); d != val {
		return d
	}
	return CleanValue(val)
}

// decodeXP turns FFmpeg's raw byte dump of a Windows XP* EXIF tag
// ("68, 0, 80, 0, 0, 0") into the UTF-16LE string it encodes ("DP").
func decodeXP(name, val string) string {
	if !strings.HasPrefix(strings.ToLower(name), "xp") {
		return val
	}
	parts := strings.Split(val, ",")
	if len(parts) < 2 {
		return val
	}
	u16 := make([]uint16, 0, len(parts)/2)
	var lo int
	for i, p := range parts {
		n, err := strconv.Atoi(strings.TrimSpace(p))
		if err != nil || n < 0 || n > 255 {
			return val
		}
		if i%2 == 0 {
			lo = n
		} else {
			u16 = append(u16, uint16(lo)|uint16(n)<<8)
		}
	}
	for len(u16) > 0 && u16[len(u16)-1] == 0 {
		u16 = u16[:len(u16)-1]
	}
	return string(utf16.Decode(u16))
}

// Meta resolves a single user-requested metadata field to a display string.
func (f *File) Meta(name string) (string, bool) {
	switch strings.ToLower(name) {
	case "rating":
		if r, ok := f.Rating(); ok {
			return strconv.Itoa(r), true
		}
		return "", false
	case "authors", "author", "artist":
		if a := f.Authors(); len(a) > 0 {
			return strings.Join(a, ", "), true
		}
		return "", false
	case "tags", "keywords", "genre":
		if t := f.Tags(); len(t) > 0 {
			return strings.Join(t, ", "), true
		}
		return "", false
	case "duration":
		if d := f.Duration(); d > 0 {
			return d.Round(time.Second).String(), true
		}
		return "", false
	case "format":
		if c := f.ContainerFormat(); c != "" {
			return c, true
		}
		return "", false
	default:
		return f.meta(name)
	}
}

// Rating returns a 0..5 star rating if the file carries one.
func (f *File) Rating() (int, bool) {
	if v, ok := f.meta("rating", "0x4746"); ok {
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && n >= 0 && n <= 5 {
			return n, true
		}
	}
	// Percent form (Windows RatingPercent / xmp:Rating as 0..100).
	if v, ok := f.meta("ratingpercent", "0x4749", "rating_percent"); ok {
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
			return percentToStars(n), true
		}
	}
	return 0, false
}

// Authors returns author/artist names.
func (f *File) Authors() []string {
	for _, key := range []string{"artist", "author", "album_artist", "composer", "Artist", "XPAuthor"} {
		if v, ok := f.meta(key); ok && v != "" {
			return splitList(v)
		}
	}
	return nil
}

// Tags returns keyword/genre tags.
func (f *File) Tags() []string {
	for _, key := range []string{"keywords", "genre", "tags", "subject", "XPKeywords"} {
		if v, ok := f.meta(key); ok && v != "" {
			return splitList(v)
		}
	}
	return nil
}

// QueryField implements query.Fields for --select / --sort-by.
func (f *File) QueryField(name string) query.Value {
	switch strings.ToLower(name) {
	case "name":
		return query.String(f.Name)
	case "path":
		return query.String(f.Abs)
	case "size":
		return query.Number(float64(f.Size))
	case "modifiedat", "modified", "mtime":
		return query.Time(f.ModTime)
	case "kind":
		return query.String(f.Kind.String())
	case "format":
		return query.String(f.ContainerFormat())
	case "rating":
		if r, ok := f.Rating(); ok {
			return query.Number(float64(r))
		}
		return query.Absent
	case "authors", "author", "artist":
		return query.List(f.Authors())
	case "tags", "keywords":
		return query.List(f.Tags())
	default:
		if v, ok := f.Meta(name); ok {
			return query.String(v)
		}
		return query.Absent
	}
}

func percentToStars(p int) int {
	switch {
	case p <= 0:
		return 0
	case p <= 12:
		return 1
	case p <= 37:
		return 2
	case p <= 62:
		return 3
	case p <= 87:
		return 4
	default:
		return 5
	}
}

func splitList(s string) []string {
	f := strings.FieldsFunc(s, func(r rune) bool {
		return r == ';' || r == ',' || r == '/' || r == '|'
	})
	out := f[:0]
	for _, p := range f {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

var reWS = regexp.MustCompile(`\s+`)

// CleanValue trims a metadata value and collapses internal whitespace (FFmpeg
// pads EXIF numbers and wraps byte arrays across lines).
func CleanValue(s string) string {
	return strings.TrimSpace(reWS.ReplaceAllString(s, " "))
}

// HumanSize formats a byte count as e.g. "947KB", "12MB", "1.4GB".
func HumanSize(n int64) string {
	const unit = 1024
	if n < unit {
		return strconv.FormatInt(n, 10) + "B"
	}
	div, exp := int64(unit), 0
	for x := n / unit; x >= unit; x /= unit {
		div *= unit
		exp++
	}
	val := float64(n) / float64(div)
	suffix := []string{"KB", "MB", "GB", "TB", "PB"}[exp]
	if val < 10 {
		return strconv.FormatFloat(val, 'f', 1, 64) + suffix
	}
	return strconv.FormatFloat(val, 'f', 0, 64) + suffix
}

// FieldNeedsDeepProbe reports whether resolving a --select / --sort-by field
// needs a deep probe (decoding a frame for image EXIF and the like). Plain
// filesystem and container-level fields do not.
func FieldNeedsDeepProbe(field string) bool {
	switch strings.ToLower(field) {
	case "name", "path", "size", "modifiedat", "modified", "mtime", "kind", "format":
		return false
	default:
		return true
	}
}

// AnyFieldNeedsDeepProbe is FieldNeedsDeepProbe over a list.
func AnyFieldNeedsDeepProbe(fields []string) bool {
	for _, f := range fields {
		if FieldNeedsDeepProbe(f) {
			return true
		}
	}
	return false
}
