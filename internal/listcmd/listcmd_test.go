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
	got := run(t, Options{Root: root, Format: render.TOON, Recursive: true})
	base := filepath.Base(root)
	for _, want := range []string{
		"3 file(s)",
		`"` + base + `/":`,     // root folder key
		`"sub/":`,              // nested folder key
		"small.jpg",            // file listed under sub/ by basename
		"big.png", "notes.txt", // root-level files
		"{filename,size,artist,comment}:", // flat tabular rows
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in:\n%s", want, got)
		}
	}
	if strings.Contains(got, "sub/small.jpg") {
		t.Fatalf("nested file should be listed by basename under its folder:\n%s", got)
	}
	if strings.Contains(got, "- filename:") {
		t.Fatalf("rows should be flat, not one attribute per line:\n%s", got)
	}
}

func TestListNonRecursiveByDefault(t *testing.T) {
	root := fixtures(t)
	got := run(t, Options{Root: root, Format: render.TOON})
	if !strings.Contains(got, "2 file(s)") { // big.png, notes.txt — sub/ skipped
		t.Fatalf("expected only the 2 top-level files:\n%s", got)
	}
	if strings.Contains(got, "small.jpg") || strings.Contains(got, `"sub/":`) {
		t.Fatalf("non-recursive listing must not descend into sub/:\n%s", got)
	}
}

func TestListSummaryOnStderr(t *testing.T) {
	root := fixtures(t)
	var out, errb bytes.Buffer
	if err := Run(context.Background(), Options{
		Root: root, Format: render.TOON, Stdout: &out, Stderr: &errb,
	}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(errb.String(), "processed 2 file(s) in ") {
		t.Fatalf("missing summary line on stderr: %q", errb.String())
	}
}

// mc.hash is not a default column, but --meta=mc.hash brings it back, read from
// the imgmeta record for images.
func TestListMetaHashColumn(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "pic.png")
	writePNG(t, src, 40, 40)
	tagged := filepath.Join(root, "pic.tagged.png")
	if err := imgmeta.Write(src, tagged, "mc.hash", "deadbeefdeadbeefdeadbeefdeadbeef"); err != nil {
		t.Fatal(err)
	}
	_ = os.Remove(src)

	// Default output omits it.
	if got := run(t, Options{Root: root, Format: render.CSV}); strings.Contains(got, "deadbeef") {
		t.Fatalf("mc.hash should not be a default column:\n%s", got)
	}
	// --meta=mc.hash adds it back.
	got := run(t, Options{Root: root, Format: render.CSV, Meta: []string{"mc.hash"}})
	if !strings.Contains(got, "mc.hash") || !strings.Contains(got, "deadbeefdeadbeefdeadbeefdeadbeef") {
		t.Fatalf("--meta=mc.hash not shown for image:\n%s", got)
	}
}

func TestListSelectAndSort(t *testing.T) {
	root := fixtures(t)
	got := run(t, Options{
		Root:      root,
		Select:    "kind=image and size>2k",
		SortBy:    "size desc",
		Format:    render.CSV,
		Recursive: true,
	})
	lines := strings.Split(strings.TrimSpace(got), "\n")
	if len(lines) != 2 { // header + big.png only (small.jpg is tiny, notes.txt not image)
		t.Fatalf("expected header + 1 row, got:\n%s", got)
	}
	if !strings.HasPrefix(lines[1], filepath.Join(root, "big.png")) {
		t.Fatalf("csv row should be absolute path to big.png: %s", lines[1])
	}
}

func TestListJSONHierarchy(t *testing.T) {
	root := fixtures(t)
	got := run(t, Options{Root: root, Format: render.JSON, Meta: []string{"format"}, Recursive: true})

	var doc map[string]any
	if err := json.Unmarshal([]byte(got), &doc); err != nil {
		t.Fatalf("invalid json: %v\n%s", err, got)
	}
	rootObj, ok := doc[filepath.Base(root)+"/"].(map[string]any)
	if !ok {
		t.Fatalf("missing root folder key: %v", doc)
	}
	sub, ok := rootObj["sub/"].(map[string]any)
	if !ok {
		t.Fatalf("missing sub/ folder: %v", rootObj)
	}
	subFiles, ok := sub["files"].([]any)
	if !ok || len(subFiles) != 1 {
		t.Fatalf("sub/ should hold 1 file: %v", sub["files"])
	}
	f0 := subFiles[0].(map[string]any)
	if f0["filename"] != "small.jpg" {
		t.Fatalf("nested file name should be a basename: %v", f0)
	}
	if _, ok := f0["format"]; !ok {
		t.Fatalf("requested --meta column 'format' missing: %v", f0)
	}

	rootFiles, ok := rootObj["files"].([]any)
	if !ok || len(rootFiles) != 2 { // big.png, notes.txt
		t.Fatalf("root should hold 2 direct files: %v", rootObj["files"])
	}
}
