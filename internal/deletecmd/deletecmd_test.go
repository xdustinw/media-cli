package deletecmd

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func exists(p string) bool { _, err := os.Stat(p); return err == nil }

func run(t *testing.T, o Options) (string, string, error) {
	t.Helper()
	var out, errb bytes.Buffer
	o.Stdout, o.Stderr = &out, &errb
	if o.Confirm == nil {
		o.Confirm = func(string) (bool, error) { return true, nil }
	}
	if !o.Recursive {
		o.Recursive = true
	}
	err := Run(context.Background(), o)
	return out.String(), errb.String(), err
}

func TestSelectRequired(t *testing.T) {
	_, _, err := run(t, Options{Folders: []string{t.TempDir()}})
	if err == nil || !strings.Contains(err.Error(), "--select is required") {
		t.Fatalf("expected a --select-required error, got %v", err)
	}
}

func TestDeletesOnlyMatches(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "IMG_2001.jpg"), "a")
	write(t, filepath.Join(root, "IMG_3002.jpg"), "b")
	write(t, filepath.Join(root, "sub", "IMG_2003.jpg"), "c")
	write(t, filepath.Join(root, "keep.jpg"), "d")

	out, _, err := run(t, Options{Folders: []string{root}, Select: "name=IMG_2* or name=IMG_3*"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "delete[3]") {
		t.Fatalf("expected 3 files in the preview:\n%s", out)
	}
	for _, gone := range []string{"IMG_2001.jpg", "IMG_3002.jpg", filepath.Join("sub", "IMG_2003.jpg")} {
		if exists(filepath.Join(root, gone)) {
			t.Fatalf("%s should have been deleted", gone)
		}
	}
	if !exists(filepath.Join(root, "keep.jpg")) {
		t.Fatal("keep.jpg does not match and must survive")
	}
}

func TestNonRecursive(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "a.tmp"), "x")
	write(t, filepath.Join(root, "sub", "b.tmp"), "y")

	var out, errb bytes.Buffer
	err := Run(context.Background(), Options{
		Folders: []string{root}, Select: "ext=tmp", Recursive: false,
		Stdout: &out, Stderr: &errb,
		Confirm: func(string) (bool, error) { return true, nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	if exists(filepath.Join(root, "a.tmp")) {
		t.Fatal("top-level a.tmp should be gone")
	}
	if !exists(filepath.Join(root, "sub", "b.tmp")) {
		t.Fatal("--nr must not descend into sub/")
	}
}

func TestConfirmAbort(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "x.jpg"), "x")
	out, _, err := run(t, Options{
		Folders: []string{root}, Select: "name=x*",
		Confirm: func(string) (bool, error) { return false, nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Aborted") {
		t.Fatalf("expected abort: %s", out)
	}
	if !exists(filepath.Join(root, "x.jpg")) {
		t.Fatal("abort must not delete")
	}
}

func TestNoMatches(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "a.jpg"), "a")
	out, _, err := run(t, Options{Folders: []string{root}, Select: "name=zzz*"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "No files match") {
		t.Fatalf("got: %s", out)
	}
}
