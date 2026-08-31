package setcmd

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xdustinw/media-cli/internal/ffmpeg"
	"github.com/xdustinw/media-cli/internal/imgmeta"
	"github.com/xdustinw/media-cli/internal/tag"
)

// sampleMP4 copies one of the repo owner's sample clips (../../tmp/video) into
// dir; the test is skipped when none are present.
func sampleMP4(t *testing.T, dir string) string {
	t.Helper()
	m, _ := filepath.Glob(filepath.Join("..", "..", "tmp", "video", "sample-r*.mp4"))
	if len(m) == 0 {
		t.Skip("no sample mp4 in tmp/video")
	}
	dst := filepath.Join(dir, "clip.mp4")
	b, err := os.ReadFile(m[0])
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dst, b, 0o644); err != nil {
		t.Fatal(err)
	}
	return dst
}

func writePNG(t *testing.T, path string) {
	t.Helper()
	m := image.NewRGBA(image.Rect(0, 0, 24, 16))
	for y := 0; y < 16; y++ {
		for x := 0; x < 24; x++ {
			m.Set(x, y, color.RGBA{uint8(x * 9), uint8(y * 13), 0x20, 255})
		}
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, m); err != nil {
		t.Fatal(err)
	}
}

func run(t *testing.T, o Options) (string, string) {
	t.Helper()
	var out, errb bytes.Buffer
	o.Stdout, o.Stderr = &out, &errb
	if err := Run(context.Background(), o); err != nil {
		t.Fatalf("run: %v (stderr %s)", err, errb.String())
	}
	return out.String(), errb.String()
}

func TestSetSelectedFiles(t *testing.T) {
	root := t.TempDir()
	writePNG(t, filepath.Join(root, "3-Adam-a.png"))
	if err := os.MkdirAll(filepath.Join(root, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	writePNG(t, filepath.Join(root, "sub", "3-Adam-b.png"))
	writePNG(t, filepath.Join(root, "other.png"))

	tags, _ := tag.Parse("rating=3,author=Adam")
	out, _ := run(t, Options{
		Target: root, Tags: tags, Select: "name=3*Adam*",
		Extensions: []string{".png"}, AssumeYes: true, Recursive: true,
	})
	if !strings.Contains(out, "on 2 file(s)") {
		t.Fatalf("expected 2 targets:\n%s", out)
	}

	for _, name := range []string{"3-Adam-a.png", filepath.Join("sub", "3-Adam-b.png")} {
		all, err := imgmeta.ReadAll(filepath.Join(root, name))
		if err != nil {
			t.Fatal(err)
		}
		if all["rating"] != "3" || all["author"] != "Adam" {
			t.Fatalf("%s not tagged: %v", name, all)
		}
	}
	// non-matching file untouched
	if all, _ := imgmeta.ReadAll(filepath.Join(root, "other.png")); len(all) != 0 {
		t.Fatalf("other.png should be untouched: %v", all)
	}
}

func TestSetVideoWritesStandardAtoms(t *testing.T) {
	root := t.TempDir()
	clip := sampleMP4(t, root)

	tags, _ := tag.Parse("artist=Adam Yu,comment=hello world")
	_, errOut := run(t, Options{
		Target: clip, Tags: tags, Extensions: []string{".mp4"}, AssumeYes: true,
	})
	if strings.Contains(errOut, "freeform") {
		t.Fatalf("artist/comment are standard fields, should stay as ilst atoms:\n%s", errOut)
	}

	// Written as iTunes ilst atoms (©ART/©cmt) — no mdta freeform box needed —
	// which is what the mov demuxer maps back and what Windows/QuickTime read.
	if v, err := ffmpeg.ReadTag(clip, "artist"); err != nil || v != "Adam Yu" {
		t.Fatalf("artist read back as %q, %v", v, err)
	}
	if v, err := ffmpeg.ReadTag(clip, "comment"); err != nil || v != "hello world" {
		t.Fatalf("comment read back as %q, %v", v, err)
	}
}

// A non-standard key on MP4 is written via the freeform mdta box and does
// persist; the command notes the fallback rather than dropping it.
func TestSetVideoNonStandardKeyStoredAsFreeform(t *testing.T) {
	root := t.TempDir()
	clip := sampleMP4(t, root)

	tags, _ := tag.Parse("rating=3,tags=holiday")
	_, errOut := run(t, Options{
		Target: clip, Tags: tags, Extensions: []string{".mp4"}, AssumeYes: true,
	})
	if !strings.Contains(errOut, "freeform") {
		t.Fatalf("expected a freeform-fallback note for non-standard keys:\n%s", errOut)
	}
	if v, err := ffmpeg.ReadTag(clip, "rating"); err != nil || v != "3" {
		t.Fatalf("rating read back as %q, %v (should persist via mdta)", v, err)
	}
	if v, err := ffmpeg.ReadTag(clip, "tags"); err != nil || v != "holiday" {
		t.Fatalf("tags read back as %q, %v", v, err)
	}
}

func TestSetNoMatch(t *testing.T) {
	root := t.TempDir()
	writePNG(t, filepath.Join(root, "x.png"))
	tags, _ := tag.Parse("rating=3")
	out, _ := run(t, Options{
		Target: root, Tags: tags, Select: "name=nope*",
		Extensions: []string{".png"}, AssumeYes: true,
	})
	if !strings.Contains(out, "No files match") {
		t.Fatalf("got: %s", out)
	}
}

func TestSetAbort(t *testing.T) {
	root := t.TempDir()
	writePNG(t, filepath.Join(root, "a.png"))
	tags, _ := tag.Parse("rating=3")
	out, _ := run(t, Options{
		Target: root, Tags: tags, Extensions: []string{".png"},
		Confirm: func(string) (bool, error) { return false, nil },
	})
	if !strings.Contains(out, "Aborted") {
		t.Fatalf("got: %s", out)
	}
	if all, _ := imgmeta.ReadAll(filepath.Join(root, "a.png")); len(all) != 0 {
		t.Fatalf("abort must not write: %v", all)
	}
}
