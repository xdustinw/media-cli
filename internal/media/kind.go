package media

import (
	"path/filepath"
	"strings"
)

// Kind classifies a media file by extension.
type Kind int

const (
	KindUnknown Kind = iota
	KindVideo
	KindImage
)

func (k Kind) String() string {
	switch k {
	case KindVideo:
		return "video"
	case KindImage:
		return "image"
	default:
		return "unknown"
	}
}

var (
	videoExts = map[string]struct{}{
		".mp4": {}, ".mkv": {}, ".mov": {}, ".m4v": {}, ".webm": {}, ".avi": {},
	}
	imageExts = map[string]struct{}{
		".jpg": {}, ".jpeg": {}, ".jpe": {}, ".jfif": {},
		".png": {}, ".apng": {}, ".gif": {}, ".webp": {},
	}
)

// KindOf returns the Kind for path based on its extension (case-insensitive).
func KindOf(path string) Kind {
	ext := strings.ToLower(filepath.Ext(path))
	if _, ok := videoExts[ext]; ok {
		return KindVideo
	}
	if _, ok := imageExts[ext]; ok {
		return KindImage
	}
	return KindUnknown
}

// DefaultExtensions is the union of recognised video and image extensions,
// used as the default scan filter.
func DefaultExtensions() []string {
	out := make([]string, 0, len(videoExts)+len(imageExts))
	for e := range videoExts {
		out = append(out, e)
	}
	for e := range imageExts {
		out = append(out, e)
	}
	return out
}
