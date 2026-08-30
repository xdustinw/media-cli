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

	"github.com/xdustinw/media-cli/internal/imgmeta"
	"github.com/xdustinw/media-cli/internal/tag"
)

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
