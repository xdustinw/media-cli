package ffmpeg

import (
	"path/filepath"
	"testing"
)

// samplePair returns two files under ../../tmp/<sub> that hold identical media
// content but differ in metadata, skipping the test when fewer than two match.
func samplePair(t *testing.T, sub, glob string) (string, string) {
	t.Helper()
	matches, _ := filepath.Glob(filepath.Join("..", "..", "tmp", sub, glob))
	if len(matches) < 2 {
		t.Skipf("need >=2 samples matching tmp/%s/%s, found %d", sub, glob, len(matches))
	}
	return matches[0], matches[1]
}

// TestStreamHashMetadataInvariant checks that two video files with identical A/V
// content but different container metadata hash to the same value.
func TestStreamHashMetadataInvariant(t *testing.T) {
	a, b := samplePair(t, "video", "sample-r*.mp4")

	ha, err := StreamHash(a)
	if err != nil {
		t.Fatalf("hash %s: %v", a, err)
	}
	hb, err := StreamHash(b)
	if err != nil {
		t.Fatalf("hash %s: %v", b, err)
	}
	t.Logf("%s=%s  %s=%s", filepath.Base(a), ha, filepath.Base(b), hb)
	if len(ha) != 32 || ha != hb {
		t.Fatalf("metadata changed the hash: %s != %s", ha, hb)
	}
}

// TestImageHashMetadataInvariant checks the same for still images (e.g.
// image-3.jpg vs image-3-r3t.jpg, which differ only by an EXIF rating tag).
func TestImageHashMetadataInvariant(t *testing.T) {
	a, b := samplePair(t, "img", "image-3*.jpg")

	ha, err := ImageHash(a)
	if err != nil {
		t.Fatalf("hash %s: %v", a, err)
	}
	hb, err := ImageHash(b)
	if err != nil {
		t.Fatalf("hash %s: %v", b, err)
	}
	t.Logf("%s=%s  %s=%s", filepath.Base(a), ha, filepath.Base(b), hb)
	if len(ha) != 32 || ha != hb {
		t.Fatalf("metadata changed the hash: %s != %s", ha, hb)
	}
}
