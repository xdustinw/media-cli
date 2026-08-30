package copycmd

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

func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func exists(path string) bool { _, err := os.Stat(path); return err == nil }

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

func TestParseMode(t *testing.T) {
	for in, want := range map[string]Mode{
		"":               ModeAsk,
		"o":              ModeOverwrite,
		"overwrite":      ModeOverwrite,
		"s":              ModeSkipDuplicate,
		"skip-duplicate": ModeSkipDuplicate,
		"r":              ModeRename,
		"RENAME":         ModeRename,
	} {
		if got, err := ParseMode(in); err != nil || got != want {
			t.Errorf("ParseMode(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	if _, err := ParseMode("merge"); err == nil {
		t.Fatal("expected an error for an unknown mode")
	}
}

func TestCopyPlainAndRenameDuplicate(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src")
	dst := filepath.Join(root, "dst")
	write(t, filepath.Join(src, "a.aaaaaa.jpg"), "SRC-A")
	write(t, filepath.Join(src, "sub", "b.bbbbbb.jpg"), "SRC-B")
	write(t, filepath.Join(dst, "keep", "old.aaaaaa.jpg"), "TGT-A")

	out, _, err := run(t, Options{Source: src, Target: dst, Mode: ModeRename})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "rename target -> a.aaaaaa.jpg") {
		t.Fatalf("preview missing rename action:\n%s", out)
	}

	// Plain file copied under its source-relative path.
	if got := read(t, filepath.Join(dst, "sub", "b.bbbbbb.jpg")); got != "SRC-B" {
		t.Fatalf("plain copy = %q", got)
	}
	// Duplicate: target renamed to the source's name, same folder, bytes kept.
	if exists(filepath.Join(dst, "keep", "old.aaaaaa.jpg")) {
		t.Fatal("old target name should be gone")
	}
	if got := read(t, filepath.Join(dst, "keep", "a.aaaaaa.jpg")); got != "TGT-A" {
		t.Fatalf("rename kept wrong bytes: %q", got)
	}
	// Copy leaves the source in place.
	if !exists(filepath.Join(src, "a.aaaaaa.jpg")) {
		t.Fatal("copy must not remove the source")
	}
}

func TestMoveOverwriteDuplicate(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src")
	dst := filepath.Join(root, "dst")
	write(t, filepath.Join(src, "new.abcabc.mp4"), "NEW-BYTES")
	write(t, filepath.Join(dst, "d", "have.abcabc.mp4"), "OLD-BYTES")

	if _, _, err := run(t, Options{Source: src, Target: dst, Move: true, Mode: ModeOverwrite}); err != nil {
		t.Fatal(err)
	}
	if got := read(t, filepath.Join(dst, "d", "have.abcabc.mp4")); got != "NEW-BYTES" {
		t.Fatalf("overwrite kept old bytes: %q", got)
	}
	if exists(filepath.Join(src, "new.abcabc.mp4")) {
		t.Fatal("move must remove the source after overwrite")
	}
}

func TestSkipDuplicateLeavesEverything(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src")
	dst := filepath.Join(root, "dst")
	write(t, filepath.Join(src, "x.ffffff.png"), "SRC")
	write(t, filepath.Join(dst, "y.ffffff.png"), "TGT")

	_, errOut, err := run(t, Options{Source: src, Target: dst, Move: true, Mode: ModeSkipDuplicate})
	if err != nil {
		t.Fatal(err)
	}
	if read(t, filepath.Join(dst, "y.ffffff.png")) != "TGT" {
		t.Fatal("skip must not touch the target")
	}
	if !exists(filepath.Join(src, "x.ffffff.png")) {
		t.Fatal("skip on move must leave the source")
	}
	if !strings.Contains(errOut, "1 duplicate(s) skipped") {
		t.Fatalf("summary should note the skip:\n%s", errOut)
	}
}

func TestAskCallbackConsulted(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src")
	dst := filepath.Join(root, "dst")
	write(t, filepath.Join(src, "a.111111.jpg"), "A")
	write(t, filepath.Join(src, "b.222222.jpg"), "B")
	write(t, filepath.Join(dst, "a0.111111.jpg"), "A-OLD")
	write(t, filepath.Join(dst, "b0.222222.jpg"), "B-OLD")

	asked := 0
	_, _, err := run(t, Options{
		Source: src, Target: dst,
		Ask: func(string) (Mode, error) {
			asked++
			if asked == 1 {
				return ModeOverwrite, nil
			}
			return ModeSkipDuplicate, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if asked != 2 {
		t.Fatalf("Ask called %d times, want 2", asked)
	}
	if read(t, filepath.Join(dst, "a0.111111.jpg")) != "A" {
		t.Fatal("first duplicate should have been overwritten")
	}
	if read(t, filepath.Join(dst, "b0.222222.jpg")) != "B-OLD" {
		t.Fatal("second duplicate should have been skipped")
	}
}

func TestPlainPathCollisionSkipped(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src")
	dst := filepath.Join(root, "dst")
	write(t, filepath.Join(src, "note.txt"), "SRC") // no short hash in the name
	write(t, filepath.Join(dst, "note.txt"), "TGT")

	_, errOut, err := run(t, Options{Source: src, Target: dst, Move: true})
	if err == nil {
		t.Fatal("expected a non-nil error for the skipped conflict")
	}
	if !strings.Contains(errOut, "already exists at destination") {
		t.Fatalf("missing conflict note:\n%s", errOut)
	}
	if read(t, filepath.Join(dst, "note.txt")) != "TGT" {
		t.Fatal("a path collision must never overwrite")
	}
}

func TestConfirmAbortChangesNothing(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src")
	dst := filepath.Join(root, "dst")
	write(t, filepath.Join(src, "a.aaaaaa.jpg"), "SRC")
	write(t, filepath.Join(dst, "keep", "b.aaaaaa.jpg"), "TGT")

	out, _, err := run(t, Options{
		Source: src, Target: dst, Mode: ModeOverwrite,
		Confirm: func(string) (bool, error) { return false, nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Aborted") {
		t.Fatalf("expected an abort message:\n%s", out)
	}
	if read(t, filepath.Join(dst, "keep", "b.aaaaaa.jpg")) != "TGT" {
		t.Fatal("abort must not change the target")
	}
}
