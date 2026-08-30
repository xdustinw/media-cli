package infocmd

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xdustinw/media-cli/internal/render"
)

func makePNG(t *testing.T, path string) {
	t.Helper()
	m := image.NewRGBA(image.Rect(0, 0, 20, 12))
	for y := 0; y < 12; y++ {
		for x := 0; x < 20; x++ {
			m.Set(x, y, color.RGBA{uint8(x * 10), uint8(y * 12), 40, 255})
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

func TestInfoTOON(t *testing.T) {
	p := filepath.Join(t.TempDir(), "pic.png")
	makePNG(t, p)

	var out bytes.Buffer
	if err := Run(context.Background(), Options{Path: p, Format: render.TOON, Stdout: &out}); err != nil {
		t.Fatal(err)
	}
	s := out.String()
	for _, want := range []string{"file:", "path:", "kind: image", "format:", "name: png_pipe", "streams", ",png,", "20,12"} {
		if !strings.Contains(s, want) {
			t.Fatalf("missing %q in:\n%s", want, s)
		}
	}
	// images must not carry the video-ish 1-frame artefacts
	if strings.Contains(s, "fps:") || strings.Contains(s, "duration:") {
		t.Fatalf("image info leaked fps/duration:\n%s", s)
	}
}

func TestInfoJSON(t *testing.T) {
	p := filepath.Join(t.TempDir(), "pic.png")
	makePNG(t, p)

	var out bytes.Buffer
	if err := Run(context.Background(), Options{Path: p, Format: render.JSON, Stdout: &out}); err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(out.Bytes(), &doc); err != nil {
		t.Fatalf("invalid json: %v\n%s", err, out.String())
	}
	file, ok := doc["file"].(map[string]any)
	if !ok || file["name"] != "pic.png" {
		t.Fatalf("bad file object: %v", doc["file"])
	}
	if _, ok := doc["streams"].([]any); !ok {
		t.Fatalf("missing streams array: %v", doc)
	}
}

func TestInfoNonMedia(t *testing.T) {
	p := filepath.Join(t.TempDir(), "x.bin")
	if err := os.WriteFile(p, []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := Run(context.Background(), Options{Path: p, Format: render.TOON, Stdout: &out}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "kind: unknown") {
		t.Fatalf("expected unknown kind:\n%s", out.String())
	}
}
