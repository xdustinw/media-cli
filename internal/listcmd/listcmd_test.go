package listcmd

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xdustinw/media-cli/internal/imgmeta"
	"github.com/xdustinw/media-cli/internal/render"
)

func writePNG(t *testing.T, path string, w, h int) {
	t.Helper()
	m := image.NewRGBA(image.Rect(0, 0, w, h))
	seed := uint32(2166136261)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			seed = seed*16777619 ^ uint32(x*7+y*13) // noise so PNG does not shrink to nothing
			m.Set(x, y, color.RGBA{uint8(seed), uint8(seed >> 8), uint8(seed >> 16), 255})
		}
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	enc := png.Encode
	if strings.HasSuffix(path, ".jpg") {
		if err := jpeg.Encode(f, m, nil); err != nil {
			t.Fatal(err)
		}
		return
	}
	if err := enc(f, m); err != nil {
		t.Fatal(err)
	}
}

func fixtures(t *testing.T) string {
	root := t.TempDir()
	writePNG(t, filepath.Join(root, "big.png"), 200, 200)
	if err := os.MkdirAll(filepath.Join(root, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	writePNG(t, filepath.Join(root, "sub", "small.jpg"), 16, 16)
	if err := os.WriteFile(filepath.Join(root, "notes.txt"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

func run(t *testing.T, o Options) string {
	t.Helper()
	var out, errb bytes.Buffer
	o.Stdout, o.Stderr = &out, &errb
	if err := Run(context.Background(), o); err != nil {
		t.Fatalf("run: %v (stderr %s)", err, errb.String())
	}
	return out.String()
}

func TestListBasic(t *testing.T) {
	root := fixtures(t)
	got := run(t, Options{Root: root, Format: render.TOON})
	for _, want := range []string{"big.png", "sub/small.jpg", "notes.txt", "3 file(s)"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in:\n%s", want, got)
		}
	}
	// One flat tabular block with the mc.hash column, not an object per row.
	if !strings.Contains(got, "{filename,size,mc.hash,rating,authors,tags}:") {
		t.Fatalf("expected a flat TOON table header:\n%s", got)
	}
	if strings.Contains(got, "- filename:") {
		t.Fatalf("output should not be one attribute per row:\n%s", got)
	}
}

func TestListImageHashColumn(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "pic.png")
	writePNG(t, src, 40, 40)
	tagged := filepath.Join(root, "pic.tagged.png")
	if err := imgmeta.Write(src, tagged, "mc.hash", "deadbeefdeadbeefdeadbeefdeadbeef"); err != nil {
		t.Fatal(err)
	}
	_ = os.Remove(src)

	got := run(t, Options{Root: root, Format: render.CSV})
	if !strings.Contains(got, "deadbeefdeadbeefdeadbeefdeadbeef") {
		t.Fatalf("mc.hash from imgmeta not shown for image:\n%s", got)
	}
}

func TestListSelectAndSort(t *testing.T) {
	root := fixtures(t)
	got := run(t, Options{
		Root:   root,
		Select: "kind=image and size>2k",
		SortBy: "size desc",
		Format: render.CSV,
	})
	lines := strings.Split(strings.TrimSpace(got), "\n")
	if len(lines) != 2 { // header + big.png only (small.jpg is tiny, notes.txt not image)
		t.Fatalf("expected header + 1 row, got:\n%s", got)
	}
	if !strings.HasPrefix(lines[1], filepath.Join(root, "big.png")) {
		t.Fatalf("csv row should be absolute path to big.png: %s", lines[1])
	}
}

func TestListJSON(t *testing.T) {
	root := fixtures(t)
	got := run(t, Options{Root: root, Format: render.JSON, Meta: []string{"format"}})
	var rows []map[string]any
	if err := json.Unmarshal([]byte(got), &rows); err != nil {
		t.Fatalf("invalid json: %v\n%s", err, got)
	}
	if len(rows) != 3 {
		t.Fatalf("want 3 rows, got %d", len(rows))
	}
	for _, r := range rows {
		if _, ok := r["filename"]; !ok {
			t.Fatalf("row missing filename: %v", r)
		}
		if _, ok := r["format"]; !ok {
			t.Fatalf("row missing requested meta column 'format': %v", r)
		}
	}
}
