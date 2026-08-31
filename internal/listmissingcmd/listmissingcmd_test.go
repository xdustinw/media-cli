package listmissingcmd

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

func TestMissingByHash(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src")
	tgt := filepath.Join(root, "tgt")
	write(t, filepath.Join(src, "a.aaaaaa.jpg"), "A")        // present in target
	write(t, filepath.Join(src, "b.bbbbbb.jpg"), "B")        // missing
	write(t, filepath.Join(src, "sub", "c.cccccc.jpg"), "C") // missing, nested
	write(t, filepath.Join(tgt, "renamed-a.aaaaaa.jpg"), "A")

	out, errOut, err := run(t, Options{Source: src, Targets: []string{tgt}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "b.bbbbbb.jpg") || !strings.Contains(out, filepath.Join("sub", "c.cccccc.jpg")) {
		t.Fatalf("both missing files should be listed:\n%s", out)
	}
	if strings.Contains(out, "a.aaaaaa.jpg") {
		t.Fatalf("the present file should not be listed:\n%s", out)
	}
	if !strings.Contains(errOut, "2 of 3 source file(s) missing") {
		t.Fatalf("summary wrong:\n%s", errOut)
	}
}

func TestMultipleTargetsCoverEverything(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src")
	t1 := filepath.Join(root, "t1")
	t2 := filepath.Join(root, "t2")
	write(t, filepath.Join(src, "a.aaaaaa.jpg"), "A")
	write(t, filepath.Join(src, "b.bbbbbb.jpg"), "B")
	write(t, filepath.Join(t1, "a.aaaaaa.jpg"), "A")
	write(t, filepath.Join(t2, "keep", "b.bbbbbb.jpg"), "B")

	out, _, err := run(t, Options{Source: src, Targets: []string{t1, t2}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "missing: 0") {
		t.Fatalf("nothing should be missing:\n%s", out)
	}
}

func TestSelectFiltersSource(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src")
	tgt := filepath.Join(root, "tgt")
	write(t, filepath.Join(src, "keep-me.aaaaaa.jpg"), "A")
	write(t, filepath.Join(src, "ignore-me.bbbbbb.jpg"), "B")

	out, _, err := run(t, Options{Source: src, Targets: []string{tgt}, Select: "name=keep*"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "keep-me.aaaaaa.jpg") {
		t.Fatalf("selected file should be checked:\n%s", out)
	}
	if strings.Contains(out, "ignore-me") {
		t.Fatalf("--select should have excluded ignore-me:\n%s", out)
	}
}

// Declining the prehash offer falls back to comparing base file names.
func TestUnhashedFallsBackToName(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src")
	tgt := filepath.Join(root, "tgt")
	write(t, filepath.Join(src, "photo.jpg"), "X") // same name in target
	write(t, filepath.Join(src, "other.jpg"), "Y") // not in target
	write(t, filepath.Join(tgt, "archive", "photo.jpg"), "X")

	out, errOut, err := run(t, Options{
		Source: src, Targets: []string{tgt},
		PreHash: func(context.Context, []string) (int, error) { t.Fatal("PreHash must not run"); return 0, nil },
		Confirm: func(p string) (bool, error) {
			if strings.Contains(p, "Hash them first") {
				return false, nil
			}
			return true, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(errOut, "comparing by file name") {
		t.Fatalf("expected the by-name notice:\n%s", errOut)
	}
	if !strings.Contains(out, "other.jpg") || strings.Contains(out, "photo.jpg") {
		t.Fatalf("only other.jpg should be missing:\n%s", out)
	}
}

// -y hashes the unhashed files via PreHash before comparing.
func TestUnhashedAutoHashedUnderY(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src")
	tgt := filepath.Join(root, "tgt")
	write(t, filepath.Join(src, "clip.mp4"), "DATA")

	called := false
	_, errOut, err := run(t, Options{
		Source: src, Targets: []string{tgt}, AssumeYes: true,
		PreHash: func(_ context.Context, files []string) (int, error) {
			called = true
			for _, f := range files {
				_ = os.Rename(f, f[:len(f)-len(".mp4")]+".abc123.mp4")
			}
			return len(files), nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("-y should have invoked PreHash")
	}
	if !strings.Contains(errOut, "1 of 1 source file(s) missing") {
		t.Fatalf("the hashed source file should be reported missing:\n%s", errOut)
	}
}
