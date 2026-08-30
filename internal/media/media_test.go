package media

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

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
	mk("sub/b.MKV")
	mk("sub/c.txt")
	mk("sub/deep/d.mkv")

	got, err := Discover(context.Background(), root, []string{".mp4", ".mkv"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("expected 3 media files, got %v", got)
	}

	// A single explicitly named file is returned regardless of extension.
	one, err := Discover(context.Background(), filepath.Join(root, "sub", "c.txt"), []string{".mp4"})
	if err != nil || len(one) != 1 {
		t.Fatalf("explicit file: got %v err %v", one, err)
	}

	// Empty directory -> ErrNoMediaFiles.
	if _, err := Discover(context.Background(), t.TempDir(), []string{".mp4"}); err != ErrNoMediaFiles {
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
