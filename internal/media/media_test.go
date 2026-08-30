package media

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSummary(t *testing.T) {
	// Rate is reported when both bytes and a positive duration are known.
	got := Summary(3, 6*1024*1024, 2*time.Second)
	if !strings.Contains(got, "processed 3 file(s) in 2s") || !strings.Contains(got, "3.0 MB/s") {
		t.Fatalf("unexpected summary: %q", got)
	}
	// No rate when byte total is unknown.
	if got := Summary(1, 0, time.Second); strings.Contains(got, "MB/s") {
		t.Fatalf("rate should be omitted without a byte total: %q", got)
	}
	// No rate on a zero duration.
	if got := Summary(1, 1024, 0); strings.Contains(got, "MB/s") {
		t.Fatalf("rate should be omitted on a zero duration: %q", got)
	}
	// Runs over a minute read as "Xm Ys".
	if got := Summary(9, 0, 125*time.Second); !strings.Contains(got, "in 2m 5s") {
		t.Fatalf("expected minutes/seconds form: %q", got)
	}
	// Just under a minute keeps the sub-minute form.
	if got := Summary(9, 0, 59500*time.Millisecond); !strings.Contains(got, "in 59.5s") {
		t.Fatalf("expected sub-minute form: %q", got)
	}
}

func TestHashedName(t *testing.T) {
	cases := map[string]string{
		"/v/movie.mp4":        "/v/movie.1a2b3c.mp4",
		"/v/movie.9f8e7d.mp4": "/v/movie.1a2b3c.mp4", // replace an existing hash slot
		"/v/movie.9F8E7D.mp4": "/v/movie.1a2b3c.mp4", // uppercase hex too
		"/v/a.b.c.mkv":        "/v/a.b.c.1a2b3c.mkv", // ".c" is not 6 hex -> keep
		"/v/clip.123456.mov":  "/v/clip.1a2b3c.mov",  // digits are hex
	}
	for in, want := range cases {
		got := HashedName(filepath.FromSlash(in), "1a2b3c")
		if got != filepath.FromSlash(want) {
			t.Errorf("HashedName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestAlreadyTagged(t *testing.T) {
	if !AlreadyTagged(filepath.FromSlash("/v/movie.1a2b3c.mp4"), "1a2b3c") {
		t.Error("expected tagged")
	}
	if AlreadyTagged(filepath.FromSlash("/v/movie.mp4"), "1a2b3c") {
		t.Error("expected not tagged")
	}
}

func TestDiscover(t *testing.T) {
	root := t.TempDir()
	mk := func(rel string) {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	mk("a.mp4")
	mk("z.mkv")
	mk("sub/b.MKV")
	mk("sub/c.txt")
	mk("sub/deep/d.mkv")

	// Recursive: every matching file below root.
	got, err := Discover(context.Background(), root, []string{".mp4", ".mkv"}, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 4 {
		t.Fatalf("expected 4 media files, got %v", got)
	}

	// Non-recursive: only root's own files, subdirectories ignored.
	top, err := Discover(context.Background(), root, []string{".mp4", ".mkv"}, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(top) != 2 {
		t.Fatalf("expected 2 top-level media files, got %v", top)
	}

	// A single explicitly named file is returned regardless of extension.
	one, err := Discover(context.Background(), filepath.Join(root, "sub", "c.txt"), []string{".mp4"}, false)
	if err != nil || len(one) != 1 {
		t.Fatalf("explicit file: got %v err %v", one, err)
	}

	// Empty directory -> ErrNoMediaFiles.
	if _, err := Discover(context.Background(), t.TempDir(), []string{".mp4"}, true); err != ErrNoMediaFiles {
		t.Fatalf("expected ErrNoMediaFiles, got %v", err)
	}
}

func TestSwapInPlace(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	dst := filepath.Join(dir, "dst")
	if err := os.WriteFile(src, []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dst, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := SwapInPlace(src, dst, false); err == nil {
		t.Fatal("expected refusal to overwrite")
	}
	if err := SwapInPlace(src, dst, true); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(dst); string(b) != "new" {
		t.Fatalf("dst = %q, want new", b)
	}
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Fatal("src should be gone")
	}
}
