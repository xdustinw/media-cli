package concatcmd

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xdustinw/media-cli/internal/ffmpeg"
	"github.com/xdustinw/media-cli/internal/splitcmd"
)

// twoParts splits a sample clip into two parts and returns their paths.
func twoParts(t *testing.T, dir string) (string, string) {
	t.Helper()
	m, _ := filepath.Glob(filepath.Join("..", "..", "tmp", "video", "sample-r*.mp4"))
	if len(m) == 0 {
		t.Skip("no sample mp4 under tmp/video")
	}
	src := filepath.Join(dir, "movie.mp4")
	b, err := os.ReadFile(m[0])
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(src, b, 0o644); err != nil {
		t.Fatal(err)
	}
	// Two cuts keep both used parts short so the re-encode test stays quick.
	var out, errb bytes.Buffer
	if err := splitcmd.Run(context.Background(), splitcmd.Options{
		File: src, Cuts: "2,4", AssumeYes: true, Stdout: &out, Stderr: &errb,
	}); err != nil {
		t.Fatalf("split: %v", err)
	}
	return filepath.Join(dir, "movie-Part1.mp4"), filepath.Join(dir, "movie-Part2.mp4")
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

func dur(t *testing.T, path string) float64 {
	t.Helper()
	p, err := ffmpeg.Inspect(path, false)
	if err != nil {
		t.Fatalf("inspect %s: %v", path, err)
	}
	return p.Format.Duration.Seconds()
}

func TestDerivedName(t *testing.T) {
	got := derivedName([]string{"/a/one.mp4", "/a/two.mp4"}, false)
	if got != filepath.FromSlash("/a/one-two-combined.mp4") {
		t.Fatalf("got %q", got)
	}
	// Re-encode into a container that can't hold mpeg4/aac -> switches to mkv.
	got = derivedName([]string{"/a/one.webm", "/a/two.webm"}, true)
	if got != filepath.FromSlash("/a/one-two-combined.mkv") {
		t.Fatalf("got %q", got)
	}
}

func TestConcatStreamCopy(t *testing.T) {
	dir := t.TempDir()
	p1, p2 := twoParts(t, dir)
	total := dur(t, p1) + dur(t, p2)

	out, _, err := run(t, Options{Inputs: []string{p1, p2}})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(out, "stream copy") {
		t.Fatalf("expected a stream-copy join:\n%s", out)
	}
	joined := filepath.Join(dir, "movie-Part1-movie-Part2-combined.mp4")
	if got := dur(t, joined); got < total-0.5 || got > total+0.5 {
		t.Fatalf("joined duration %.2f, want ~%.2f", got, total)
	}
}

func TestConcatReencodeOnMismatch(t *testing.T) {
	dir := t.TempDir()
	p1, p2 := twoParts(t, dir)

	// Re-encode p1 to a different codec + size so it no longer matches p2.
	diff := filepath.Join(dir, "diff.mp4")
	if err := ffmpeg.Transcode(p1, diff, 320, 180, 25, 1, 44100, 2); err != nil {
		t.Fatalf("prep transcode: %v", err)
	}

	out, errOut, err := run(t, Options{
		Inputs: []string{diff, p2}, OutputFile: filepath.Join(dir, "j.mp4"),
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(out, "re-encode") || !strings.Contains(errOut, "inputs differ") {
		t.Fatalf("expected a re-encode warning:\nstdout %s\nstderr %s", out, errOut)
	}
	j, err := ffmpeg.Inspect(filepath.Join(dir, "j.mp4"), false)
	if err != nil {
		t.Fatalf("output not valid: %v", err)
	}
	var haveV, haveA bool
	for _, s := range j.Streams {
		switch s.Type {
		case "video":
			haveV = true
			if s.Codec != "mpeg4" {
				t.Fatalf("video codec = %s, want mpeg4", s.Codec)
			}
		case "audio":
			haveA = true
		}
	}
	if !haveV || !haveA {
		t.Fatal("re-encoded output is missing a stream")
	}
	want := dur(t, diff) + dur(t, p2)
	if got := j.Format.Duration.Seconds(); got < want-1 || got > want+1 {
		t.Fatalf("joined duration %.2f, want ~%.2f", got, want)
	}
}

func TestConcatNeedsTwoInputs(t *testing.T) {
	if _, _, err := run(t, Options{Inputs: []string{"only.mp4"}}); err == nil {
		t.Fatal("expected an error for a single input")
	}
}

func TestConcatAbort(t *testing.T) {
	dir := t.TempDir()
	p1, p2 := twoParts(t, dir)
	out, _, err := run(t, Options{
		Inputs:  []string{p1, p2},
		Confirm: func(string) (bool, error) { return false, nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Aborted") {
		t.Fatalf("got: %s", out)
	}
	if _, err := os.Stat(filepath.Join(dir, "movie-Part1-movie-Part2-combined.mp4")); err == nil {
		t.Fatal("abort must not write output")
	}
}
