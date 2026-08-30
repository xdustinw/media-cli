package hashcmd

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xdustinw/media-cli/internal/ffmpeg"
	"github.com/xdustinw/media-cli/internal/imgmeta"
)

func gradient() image.Image {
	m := image.NewRGBA(image.Rect(0, 0, 32, 24))
	for y := 0; y < 24; y++ {
		for x := 0; x < 32; x++ {
			m.Set(x, y, color.RGBA{uint8(x * 8), uint8(y * 10), 0x30, 0xff})
		}
	}
	return m
}

func writeImage(t *testing.T, path string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	switch strings.ToLower(filepath.Ext(path)) {
	case ".png":
		err = png.Encode(f, gradient())
	case ".jpg", ".jpeg":
		err = jpeg.Encode(f, gradient(), &jpeg.Options{Quality: 92})
	case ".gif":
		err = gif.Encode(f, gradient(), nil)
	}
	if err != nil {
		t.Fatal(err)
	}
}

// TestImageHashStableThroughTagging is the core guarantee: tagging an image
// (which inserts container-level metadata) must not change its pixel hash, and
// a second run must be a no-op.
func TestImageHashStableThroughTagging(t *testing.T) {
	for _, ext := range []string{".png", ".jpg", ".gif"} {
		t.Run(ext, func(t *testing.T) {
			root := t.TempDir()
			orig := filepath.Join(root, "pic"+ext)
			writeImage(t, orig)

			before, err := ffmpeg.ImageHash(orig)
			if err != nil {
				t.Fatalf("hash: %v", err)
			}

			var out, errb bytes.Buffer
			opts := Options{
				Target:      root,
				Extensions:  []string{ext},
				MetadataKey: "mc.hash",
				NameLength:  6,
				AssumeYes:   true,
				Stdout:      &out,
				Stderr:      &errb,
				Logger:      quietLogger(),
			}
			if err := Run(context.Background(), opts); err != nil {
				t.Fatalf("run: %v\n%s", err, errb.String())
			}

			tagged := filepath.Join(root, "pic."+before[:6]+ext)
			after, err := ffmpeg.ImageHash(tagged)
			if err != nil {
				t.Fatalf("hash tagged: %v", err)
			}
			if before != after {
				t.Fatalf("tagging changed the pixel hash: %s != %s", before, after)
			}
			if v, err := imgmeta.Read(tagged, "mc.hash"); err != nil || v != before {
				t.Fatalf("tag readback: %q %v", v, err)
			}

			out.Reset()
			if err := Run(context.Background(), opts); err != nil {
				t.Fatalf("second run: %v", err)
			}
			if !strings.Contains(out.String(), "Nothing to do") {
				t.Fatalf("not idempotent: %s", out.String())
			}
		})
	}
}

func onlyMatch(t *testing.T, dir, glob string) string {
	t.Helper()
	m, _ := filepath.Glob(filepath.Join(dir, glob))
	if len(m) != 1 {
		t.Fatalf("want exactly 1 match for %s, got %v", glob, m)
	}
	return m[0]
}

// TestSkipAndStaleWarning covers the two tag-state paths: an up-to-date file is
// left untouched on a re-run, and a file whose stored mc.hash no longer matches
// its content is warned about and re-tagged.
func TestSkipAndStaleWarning(t *testing.T) {
	root := t.TempDir()
	writeImage(t, filepath.Join(root, "keep.png"))
	writeImage(t, filepath.Join(root, "stale.png"))

	run := func() (stdout, stderr string) {
		var o, e bytes.Buffer
		if err := Run(context.Background(), Options{
			Target: root, Extensions: []string{".png"}, MetadataKey: "mc.hash",
			NameLength: 6, AssumeYes: true, Stdout: &o, Stderr: &e, Logger: quietLogger(),
		}); err != nil {
			t.Fatalf("run: %v (stderr %s)", err, e.String())
		}
		return o.String(), e.String()
	}

	run() // first pass: tag + rename both

	// Overwrite stale's stored tag so it no longer matches the pixels.
	staleTagged := onlyMatch(t, root, "stale.*.png")
	tmp := filepath.Join(root, "tmp.png")
	if err := imgmeta.Write(staleTagged, tmp, "mc.hash", "dead00000000000000000000000000ff"); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(tmp, staleTagged); err != nil {
		t.Fatal(err)
	}

	stdout, stderr := run()
	if !strings.Contains(stderr, "content hash changed") {
		t.Fatalf("expected stale-tag warning on stderr, got: %q", stderr)
	}
	if !strings.Contains(stdout, "1 file(s) already up to date, skipped") {
		t.Fatalf("expected keep.png reported as skipped, got: %q", stdout)
	}
	// stale.png keeps its name (prefix already right) but is re-tagged.
	if v, err := imgmeta.Read(staleTagged, "mc.hash"); err != nil || strings.HasPrefix(v, "dead") {
		t.Fatalf("stale tag not refreshed: %q %v", v, err)
	}
}

// TestImageHashIgnoresMetadata proves two files with identical pixels but
// different embedded metadata hash the same.
func TestImageHashIgnoresMetadata(t *testing.T) {
	for _, ext := range []string{".png", ".jpg", ".gif"} {
		t.Run(ext, func(t *testing.T) {
			dir := t.TempDir()
			a := filepath.Join(dir, "a"+ext)
			b := filepath.Join(dir, "b"+ext)
			writeImage(t, a)

			// b = a with a foreign metadata tag injected.
			if err := imgmeta.Write(a, b, "author", "someone else"); err != nil {
				t.Fatal(err)
			}

			ha, err := ffmpeg.ImageHash(a)
			if err != nil {
				t.Fatal(err)
			}
			hb, err := ffmpeg.ImageHash(b)
			if err != nil {
				t.Fatal(err)
			}
			if ha != hb {
				t.Fatalf("metadata affected image hash: %s != %s", ha, hb)
			}
		})
	}
}
