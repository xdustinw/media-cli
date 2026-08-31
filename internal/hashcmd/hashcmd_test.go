package hashcmd

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xdustinw/media-cli/internal/ffmpeg"
)

func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// samples returns two MP4s with identical A/V but different metadata, populated
// by the repo owner under ../../tmp/video; tests skip when absent.
func samples(t *testing.T) (string, string) {
	t.Helper()
	m, _ := filepath.Glob(filepath.Join("..", "..", "tmp", "video", "sample-r*.mp4"))
	if len(m) < 2 {
		t.Skipf("need >=2 samples in tmp/video, found %d", len(m))
	}
	return m[0], m[1]
}

func cp(t *testing.T, src, dst string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		t.Fatal(err)
	}
	in, err := os.Open(src)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		t.Fatal(err)
	}
	defer out.Close()
	if _, err := io.Copy(out, in); err != nil {
		t.Fatal(err)
	}
}

func TestRunTagsRenamesAndIsIdempotent(t *testing.T) {
	a, b := samples(t)
	root := t.TempDir()
	cp(t, a, filepath.Join(root, "movie.mp4"))
	cp(t, b, filepath.Join(root, "sub", "clip.mp4"))

	var out, errb bytes.Buffer
	opts := Options{
		Targets:     []string{root},
		Extensions:  []string{".mp4", ".mkv"},
		Method:      MethodFFmpeg,
		MetadataKey: "mc.hash",
		NameLength:  6,
		AssumeYes:   true,
		Recursive:   true,
		Stdout:      &out,
		Stderr:      &errb,
		Logger:      quietLogger(),
	}
	if err := Run(context.Background(), opts); err != nil {
		t.Fatalf("run: %v\nstderr: %s", err, errb.String())
	}

	// The preview streams one line per file (with its hash and target name)
	// before the summary — not a batch list afterwards.
	so := out.String()
	preIdx := strings.Index(so, "Preview")
	movieIdx := strings.Index(so, "movie.mp4  ")
	summaryIdx := strings.Index(so, "file(s) —")
	if preIdx < 0 || movieIdx < preIdx || summaryIdx < movieIdx {
		t.Fatalf("expected streamed per-file preview before the summary:\n%s", so)
	}
	if !strings.Contains(so, "movie.mp4  8f9b6e8b376998734a08110d2d75e137  ->  movie.8f9b6e.mp4") {
		t.Fatalf("preview line missing hash/target name:\n%s", so)
	}

	var renamed []string
	filepath.WalkDir(root, func(p string, d os.DirEntry, _ error) error {
		if d != nil && !d.IsDir() {
			renamed = append(renamed, filepath.Base(p))
		}
		return nil
	})
	for _, name := range renamed {
		if !strings.Contains(name, ".") || strings.Count(name, ".") < 2 {
			t.Fatalf("file not renamed with hash prefix: %s", name)
		}
	}

	// The freeform tag is embedded and matches the hash in the new filename.
	var tagged string
	filepath.WalkDir(root, func(p string, d os.DirEntry, _ error) error {
		if d == nil || d.IsDir() {
			return nil
		}
		v, err := ffmpeg.ReadTag(p, "mc.hash")
		if err != nil {
			t.Fatalf("ReadTag(%s): %v", p, err)
		}
		if !strings.Contains(filepath.Base(p), v[:6]) {
			t.Fatalf("filename %s does not carry hash prefix %s", filepath.Base(p), v[:6])
		}
		tagged = v
		return nil
	})
	if len(tagged) != 32 {
		t.Fatalf("unexpected tag value: %q", tagged)
	}

	// Second run: nothing to do.
	out.Reset()
	if err := Run(context.Background(), opts); err != nil {
		t.Fatalf("second run: %v", err)
	}
	if !strings.Contains(out.String(), "Nothing to do") {
		t.Fatalf("expected idempotent no-op, got: %s", out.String())
	}
}

// TestMethodFFmpeg10MVideoRenameOnly: ffmpeg-10m renames the clip by a bounded
// stream hash and writes no mc.hash tag.
func TestMethodFFmpeg10MVideoRenameOnly(t *testing.T) {
	a, _ := samples(t)
	root := t.TempDir()
	orig := filepath.Join(root, "movie.mp4")
	cp(t, a, orig)
	tagBefore, _ := ffmpeg.ReadTag(orig, "mc.hash") // samples may already carry one

	var out, errb bytes.Buffer
	if err := Run(context.Background(), Options{
		Targets: []string{root}, Extensions: []string{".mp4"}, Method: MethodFFmpeg10M,
		NameLength: 6, AssumeYes: true,
		Stdout: &out, Stderr: &errb, Logger: quietLogger(),
	}); err != nil {
		t.Fatalf("run: %v\n%s", err, errb.String())
	}
	if !strings.Contains(out.String(), "rename only") {
		t.Fatalf("expected a rename-only preview header:\n%s", out.String())
	}

	got := onlyMatch(t, root, "movie.*.mp4")
	if tagAfter, _ := ffmpeg.ReadTag(got, "mc.hash"); tagAfter != tagBefore {
		t.Fatalf("ffmpeg-10m must not touch mc.hash (%q -> %q)", tagBefore, tagAfter)
	}
	// The bounded hash differs from the full-stream hash on a >10 MiB sample.
	full, err := ffmpeg.StreamHash(got)
	if err != nil {
		t.Fatal(err)
	}
	capped, err := ffmpeg.StreamHashLimit(got, 10<<20)
	if err != nil {
		t.Fatal(err)
	}
	if full == capped {
		t.Fatal("expected the 10 MiB-bounded stream hash to differ from the full hash")
	}
}

func TestRunAbortsWithoutConfirmation(t *testing.T) {
	a, _ := samples(t)
	root := t.TempDir()
	orig := filepath.Join(root, "movie.mp4")
	cp(t, a, orig)

	var out, errb bytes.Buffer
	opts := Options{
		Targets:     []string{root},
		Extensions:  []string{".mp4"},
		Method:      MethodFFmpeg,
		MetadataKey: "mc.hash",
		NameLength:  6,
		Stdout:      &out,
		Stderr:      &errb,
		Logger:      quietLogger(),
		Confirm:     func(string) (bool, error) { return false, nil },
	}
	if err := Run(context.Background(), opts); err != nil {
		t.Fatalf("run: %v", err)
	}
	if _, err := os.Stat(orig); err != nil {
		t.Fatalf("original file should be untouched: %v", err)
	}
	if !strings.Contains(out.String(), "Aborted") {
		t.Fatalf("expected abort message, got: %s", out.String())
	}
}
