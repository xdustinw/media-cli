package splitcmd

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xdustinw/media-cli/internal/ffmpeg"
)

func sample(t *testing.T, dir string) string {
	t.Helper()
	m, _ := filepath.Glob(filepath.Join("..", "..", "tmp", "video", "sample-r*.mp4"))
	if len(m) == 0 {
		t.Skip("no sample mp4 under tmp/video")
	}
	dst := filepath.Join(dir, "movie.mp4")
	b, err := os.ReadFile(m[0])
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dst, b, 0o644); err != nil {
		t.Fatal(err)
	}
	return dst
}

func run(t *testing.T, o Options) (string, string, error) {
	t.Helper()
	var out, errb bytes.Buffer
	o.Stdout, o.Stderr = &out, &errb
	if o.Confirm == nil {
		o.Confirm = func(string) (bool, error) { return true, nil }
	}
	err := Run(context.Background(), o)
	return out.String(), errb.String(), err
}

func TestParseTimecode(t *testing.T) {
	cases := map[string]float64{
		"90":      90,
		"1:20":    80,
		"1:00:00": 3600,
		"0:0:5.5": 5.5,
		" 2:30 ":  150,
	}
	for in, want := range cases {
		got, err := parseTimecode(in)
		if err != nil || got != want {
			t.Errorf("parseTimecode(%q) = %v, %v; want %v", in, got, err, want)
		}
	}
	for _, bad := range []string{"", "a:b", "1:2:3:4", "-5"} {
		if _, err := parseTimecode(bad); err == nil {
			t.Errorf("parseTimecode(%q) should error", bad)
		}
	}
}

func TestParseCutsSortsAndDedupes(t *testing.T) {
	got, err := parseCuts("2:30, 90 , 90, 1:00")
	if err != nil {
		t.Fatal(err)
	}
	want := []float64{60, 90, 150}
	if len(got) != len(want) {
		t.Fatalf("got %v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v want %v", got, want)
		}
	}
}

func TestSplitProducesParts(t *testing.T) {
	dir := t.TempDir()
	src := sample(t, dir)

	out, _, err := run(t, Options{File: src, Cuts: "3,7"})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(out, "parts[3]") {
		t.Fatalf("expected 3 parts in the preview:\n%s", out)
	}
	for i := 1; i <= 3; i++ {
		p := filepath.Join(dir, "movie-Part"+string(rune('0'+i))+".mp4")
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("part %d missing: %v", i, err)
		}
		pr, err := ffmpeg.Inspect(p, false)
		if err != nil {
			t.Fatalf("part %d not a valid media file: %v", i, err)
		}
		if len(pr.Streams) == 0 {
			t.Fatalf("part %d has no streams", i)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "movie-Part4.mp4")); err == nil {
		t.Fatal("only 3 parts expected")
	}
}

func TestSplitOutputFolder(t *testing.T) {
	dir := t.TempDir()
	src := sample(t, dir)
	outDir := filepath.Join(dir, "clips")

	if _, _, err := run(t, Options{File: src, Cuts: "4", OutputDir: outDir}); err != nil {
		t.Fatal(err)
	}
	for _, n := range []string{"movie-Part1.mp4", "movie-Part2.mp4"} {
		if _, err := os.Stat(filepath.Join(outDir, n)); err != nil {
			t.Fatalf("%s not written to the output folder: %v", n, err)
		}
	}
}

func TestSplitRejectsCutPastEnd(t *testing.T) {
	dir := t.TempDir()
	src := sample(t, dir)
	if _, _, err := run(t, Options{File: src, Cuts: "9999"}); err == nil {
		t.Fatal("expected an error for a cut past the file's duration")
	}
}

func TestSplitAbort(t *testing.T) {
	dir := t.TempDir()
	src := sample(t, dir)
	out, _, err := run(t, Options{
		File: src, Cuts: "3",
		Confirm: func(string) (bool, error) { return false, nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Aborted") {
		t.Fatalf("got: %s", out)
	}
	if _, err := os.Stat(filepath.Join(dir, "movie-Part1.mp4")); err == nil {
		t.Fatal("abort must not write parts")
	}
}

var _ io.Writer = (*bytes.Buffer)(nil)
