package media

import (
	"slices"
	"testing"
)

func TestKindOf(t *testing.T) {
	for path, want := range map[string]Kind{
		"a.mp4":       KindVideo,
		"a.MKV":       KindVideo,
		"clip.wmv":    KindVideo,
		"clip.asf":    KindVideo,
		"movie.flv":   KindVideo,
		"x.ts":        KindVideo,
		"pic.jpg":     KindImage,
		"pic.WEBP":    KindImage,
		"notes.txt":   KindUnknown,
		"archive.zip": KindUnknown,
		"noext":       KindUnknown,
	} {
		if got := KindOf(path); got != want {
			t.Errorf("KindOf(%q) = %v, want %v", path, got, want)
		}
	}
}

func TestDefaultExtensionsCoversKnownKinds(t *testing.T) {
	exts := DefaultExtensions()
	for _, e := range []string{".mp4", ".wmv", ".avi", ".jpg", ".webp"} {
		if !slices.Contains(exts, e) {
			t.Errorf("DefaultExtensions() missing %q", e)
		}
	}
	// Every entry must classify as a real media kind.
	for _, e := range exts {
		if KindOf("x"+e) == KindUnknown {
			t.Errorf("DefaultExtensions() has %q which KindOf does not recognise", e)
		}
	}
}
