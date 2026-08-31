package media

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
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

func TestIsCrossDevice(t *testing.T) {
	if !isCrossDevice(&os.LinkError{Op: "rename", Err: syscall.EXDEV}) {
		t.Fatal("EXDEV should count as a cross-device error")
	}
	if isCrossDevice(&os.LinkError{Op: "rename", Err: syscall.ENOENT}) {
		t.Fatal("ENOENT is not a cross-device error")
	}
	if isCrossDevice(errors.New("some other failure")) {
		t.Fatal("a plain error is not a cross-device error")
	}
}

// A rename failure that is not cross-device must be returned as-is, with the
// source left untouched — never copied-then-deleted.
func TestMoveFileKeepsSourceOnRenameError(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src.bin")
	if err := os.WriteFile(src, []byte("keepme"), 0o644); err != nil {
		t.Fatal(err)
	}
	// dst is an existing, non-empty directory: os.Rename fails with a
	// non-EXDEV error and MoveFile must not fall back to copy+remove.
	dst := filepath.Join(dir, "dstdir")
	if err := os.MkdirAll(filepath.Join(dst, "child"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := MoveFile(src, dst); err == nil {
		t.Fatal("expected an error moving onto a non-empty directory")
	}
	if _, err := os.Stat(src); err != nil {
		t.Fatalf("source must survive a failed move: %v", err)
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
