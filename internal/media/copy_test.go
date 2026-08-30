package media

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCopyFileCreatesDirsAndReplaces(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src.bin")
	if err := os.WriteFile(src, []byte("new"), 0o600); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(dir, "a", "b", "dst.bin")

	if err := CopyFile(src, dst); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(dst); string(b) != "new" {
		t.Fatalf("dst = %q", b)
	}
	// Perm is carried from src.
	if fi, _ := os.Stat(dst); fi.Mode().Perm() != 0o600 {
		t.Fatalf("perm = %v", fi.Mode().Perm())
	}
	// Replacing an existing dst works and leaves no temp files behind.
	if err := os.WriteFile(src, []byte("newer"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := CopyFile(src, dst); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(dst); string(b) != "newer" {
		t.Fatalf("dst after replace = %q", b)
	}
	entries, _ := os.ReadDir(filepath.Dir(dst))
	for _, e := range entries {
		if len(e.Name()) > 3 && e.Name()[:3] == ".mc" {
			t.Fatalf("leftover temp file: %s", e.Name())
		}
	}
}

func TestMoveFileRemovesSource(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src.bin")
	if err := os.WriteFile(src, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(dir, "sub", "dst.bin")
	if err := MoveFile(src, dst); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Fatal("source should be gone after move")
	}
	if b, _ := os.ReadFile(dst); string(b) != "x" {
		t.Fatalf("dst = %q", b)
	}
}
