package mediainfo

import (
	"regexp"
	"strings"
)

// FFmpeg's EXIF parser names the standard tags but emits Microsoft / XMP
// extensions as raw hex ids. This table maps the useful ones back to names for
// display and lookup.
var exifNameByTag = map[string]string{
	"0x4746": "Rating",
	"0x4749": "RatingPercent",
	"0x9c9b": "XPTitle",
	"0x9c9c": "XPComment",
	"0x9c9d": "XPAuthor",
	"0x9c9e": "XPKeywords",
	"0x9c9f": "XPSubject",
	"0x9286": "UserComment",
	"0x010e": "ImageDescription",
	"0x013b": "Artist",
	"0x8298": "Copyright",
}

// exifTagByName is the reverse mapping (lowercased name -> hex id).
var exifTagByName = func() map[string]string {
	m := make(map[string]string, len(exifNameByTag))
	for tag, name := range exifNameByTag {
		m[strings.ToLower(name)] = tag
	}
	return m
}()

var (
	reHexTag    = regexp.MustCompile(`(?i)(^|/)0x[0-9a-f]{2,4}$`)
	reByteArray = regexp.MustCompile(`^[\s\d,]+$`)
)

// IsNoiseTag reports whether a metadata entry is an unnamed hex EXIF tag whose
// value is a long raw byte array (e.g. a Windows thumbnail) — not worth showing.
func IsNoiseTag(key, value string) bool {
	return reHexTag.MatchString(key) &&
		len(value) > 80 &&
		reByteArray.MatchString(value)
}

// FriendlyKey maps a raw metadata key to a nicer display name when known,
// otherwise returns it unchanged.
func FriendlyKey(k string) string {
	if name, ok := exifNameByTag[strings.ToLower(k)]; ok {
		return name
	}
	// "ExifIFD/0x4746" -> "Rating"
	if i := strings.LastIndexByte(k, '/'); i >= 0 {
		if name, ok := exifNameByTag[strings.ToLower(k[i+1:])]; ok {
			return name
		}
	}
	return k
}
