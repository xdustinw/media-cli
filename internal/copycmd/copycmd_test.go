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
	if !o.Recursive {
		o.Recursive = true // default in tests unless a case overrides
	}
	err := Run(context.Background(), o)
	return out.String(), errb.String(), err
}

func TestParseMode(t *testing.T) {
	for in, want := range map[string]Mode{
		"":               ModeSkipDuplicate,
		"s":              ModeSkipDuplicate,
		"skip-duplicate": ModeSkipDuplicate,
		"o":              ModeOverwrite,
		"overwrite":      ModeOverwrite,
		"k":              ModeKeepBoth,
		"KEEP-BOTH":      ModeKeepBoth,
	} {
		if got, err := ParseMode(in); err != nil || got != want {
			t.Errorf("ParseMode(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	if _, err := ParseMode("rename"); err == nil {
		t.Fatal("rename is no longer a mode")
	}
}

func TestCopyPlainFilesFromMultipleSources(t *testing.T) {
	root := t.TempDir()
	s1 := filepath.Join(root, "s1")
	s2 := filepath.Join(root, "s2")
	dst := filepath.Join(root, "dst")
	write(t, filepath.Join(s1, "a.aaaaaa.jpg"), "A")
	write(t, filepath.Join(s2, "sub", "b.bbbbbb.jpg"), "B")

	if _, _, err := run(t, Options{Sources: []string{s1, s2}, Target: dst}); err != nil {
		t.Fatal(err)
	}
	if read(t, filepath.Join(dst, "a.aaaaaa.jpg")) != "A" {
		t.Fatal("source 1 file missing")
	}
	if read(t, filepath.Join(dst, "sub", "b.bbbbbb.jpg")) != "B" {
		t.Fatal("source 2 nested file missing")
	}
}

func TestSkipDuplicateIsDefaultAndKeepsSourceOnMove(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src")
	dst := filepath.Join(root, "dst")
	write(t, filepath.Join(src, "x.ffffff.png"), "SRC")
	write(t, filepath.Join(dst, "have", "y.ffffff.png"), "TGT")

	_, errOut, err := run(t, Options{Sources: []string{src}, Target: dst, Move: true}) // no Mode => skip
	if err != nil {
		t.Fatal(err)
	}
	if read(t, filepath.Join(dst, "have", "y.ffffff.png")) != "TGT" {
		t.Fatal("skip must not touch the target")
	}
	if !exists(filepath.Join(src, "x.ffffff.png")) {
		t.Fatal("move + skip-duplicate must leave the source in place")
	}
	if !strings.Contains(errOut, "1 duplicate(s) skipped") {
		t.Fatalf("summary should note the skip:\n%s", errOut)
	}
}

func TestOverwriteDuplicateOnMove(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src")
	dst := filepath.Join(root, "dst")
	write(t, filepath.Join(src, "new.abcabc.mp4"), "NEW")
	write(t, filepath.Join(dst, "d", "old.abcabc.mp4"), "OLD")

	if _, _, err := run(t, Options{Sources: []string{src}, Target: dst, Move: true, Mode: ModeOverwrite}); err != nil {
		t.Fatal(err)
	}
	if read(t, filepath.Join(dst, "d", "old.abcabc.mp4")) != "NEW" {
		t.Fatal("overwrite kept old bytes")
	}
	if exists(filepath.Join(src, "new.abcabc.mp4")) {
		t.Fatal("move + overwrite must remove the source")
	}
}

func TestKeepBothBringsDuplicateInToo(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src")
	dst := filepath.Join(root, "dst")
	write(t, filepath.Join(src, "mine.dddddd.jpg"), "MINE")
	write(t, filepath.Join(dst, "existing", "theirs.dddddd.jpg"), "THEIRS")

	if _, _, err := run(t, Options{Sources: []string{src}, Target: dst, Mode: ModeKeepBoth}); err != nil {
		t.Fatal(err)
	}
	if read(t, filepath.Join(dst, "existing", "theirs.dddddd.jpg")) != "THEIRS" {
		t.Fatal("keep-both must not touch the existing target file")
	}
	if read(t, filepath.Join(dst, "mine.dddddd.jpg")) != "MINE" {
		t.Fatal("keep-both must also bring the source in under its own path")
	}
}

func TestSelectFiltersAndConfirms(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src")
	dst := filepath.Join(root, "dst")
	write(t, filepath.Join(src, "3-keep.aaaaaa.jpg"), "K")
	write(t, filepath.Join(src, "9-drop.bbbbbb.jpg"), "D")

	asked := ""
	out, _, err := run(t, Options{
		Sources: []string{src}, Target: dst, Select: "name=3*",
		Confirm: func(p string) (bool, error) { asked += p; return true, nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(asked, "Copy these 1 file(s)?") {
		t.Fatalf("expected a pre-scan confirmation for --select, got prompts:\n%s", asked)
	}
	if !exists(filepath.Join(dst, "3-keep.aaaaaa.jpg")) {
		t.Fatal("selected file not copied")
	}
	if exists(filepath.Join(dst, "9-drop.bbbbbb.jpg")) {
		t.Fatalf("non-matching file was copied:\n%s", out)
	}
}

func TestNonRecursiveSource(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src")
	dst := filepath.Join(root, "dst")
	write(t, filepath.Join(src, "top.aaaaaa.jpg"), "TOP")
	write(t, filepath.Join(src, "sub", "deep.bbbbbb.jpg"), "DEEP")

	var out, errb bytes.Buffer
	err := Run(context.Background(), Options{
		Sources: []string{src}, Target: dst, Recursive: false,
		Stdout: &out, Stderr: &errb,
		Confirm: func(string) (bool, error) { return true, nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	if !exists(filepath.Join(dst, "top.aaaaaa.jpg")) {
		t.Fatal("top-level file should be copied")
	}
	if exists(filepath.Join(dst, "sub", "deep.bbbbbb.jpg")) {
		t.Fatal("--nr must not descend into subfolders")
	}
}

// When files have no hash and the user declines to hash them, the comparison
// falls back to relative path.
func TestUnhashedFallsBackToPathCompare(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src")
	dst := filepath.Join(root, "dst")
	write(t, filepath.Join(src, "a.txt"), "SRC-A") // path duplicate
	write(t, filepath.Join(src, "b.txt"), "SRC-B") // new
	write(t, filepath.Join(dst, "a.txt"), "TGT-A")

	// PreHash present, but the user says "no".
	_, errOut, err := run(t, Options{
		Sources: []string{src}, Target: dst, Move: true,
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
	if !strings.Contains(errOut, "comparing by relative path") {
		t.Fatalf("expected the path-compare notice:\n%s", errOut)
	}
	if read(t, filepath.Join(dst, "a.txt")) != "TGT-A" {
		t.Fatal("a.txt is a path duplicate; skip-duplicate must keep the target")
	}
	if !exists(filepath.Join(src, "a.txt")) {
		t.Fatal("move + skip-duplicate must leave the source path-duplicate in place")
	}
	if read(t, filepath.Join(dst, "b.txt")) != "SRC-B" {
		t.Fatal("b.txt is not a duplicate and should have moved in")
	}
}

// -y hashes the unhashed files via PreHash before comparing.
func TestUnhashedAutoHashedUnderY(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src")
	dst := filepath.Join(root, "dst")
	write(t, filepath.Join(src, "clip.mp4"), "DATA")

	called := false
	_, _, err := run(t, Options{
		Sources: []string{src}, Target: dst, AssumeYes: true,
		PreHash: func(_ context.Context, files []string) (int, error) {
			called = true
			// pretend we hashed: rename in place with a fake slot
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
	if !exists(filepath.Join(dst, "clip.abc123.mp4")) {
		t.Fatal("the hashed file should have been copied in under its new name")
	}
}

// move --delete-source removes a duplicate source even under skip-duplicate.
func TestMoveDeleteSourceRemovesSkippedDuplicate(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src")
	dst := filepath.Join(root, "dst")
	write(t, filepath.Join(src, "dup.abcabc.mp4"), "SRC")   // duplicate of the target
	write(t, filepath.Join(src, "fresh.bbbbbb.mp4"), "NEW") // not a duplicate
	write(t, filepath.Join(dst, "have", "old.abcabc.mp4"), "TGT")

	out, errOut, err := run(t, Options{
		Sources: []string{src}, Target: dst, Move: true, DeleteSource: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if read(t, filepath.Join(dst, "have", "old.abcabc.mp4")) != "TGT" {
		t.Fatal("skip-duplicate must still leave the target bytes alone")
	}
	if exists(filepath.Join(src, "dup.abcabc.mp4")) {
		t.Fatalf("--delete-source must remove the duplicate source:\n%s", out)
	}
	if !exists(filepath.Join(dst, "fresh.bbbbbb.mp4")) {
		t.Fatal("the non-duplicate source should still be moved in")
	}
	if exists(filepath.Join(src, "fresh.bbbbbb.mp4")) {
		t.Fatal("the non-duplicate source should be gone after a move")
	}
	if !strings.Contains(errOut, "duplicate source(s) deleted") {
		t.Fatalf("summary should note the deleted source:\n%s", errOut)
	}
}

// After a move drains a source subtree, the empty folders are cleaned up;
// folders that still hold a file are left alone.
func TestMoveRemovesEmptedSourceDirs(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src")
	dst := filepath.Join(root, "dst")
	write(t, filepath.Join(src, "a", "b", "3-clip.aaaaaa.mp4"), "X") // matches --select, moves
	write(t, filepath.Join(src, "keep", "9-clip.bbbbbb.mp4"), "Y")   // does not match, stays

	out, _, err := run(t, Options{Sources: []string{src}, Target: dst, Move: true, Select: "name=3*"})
	if err != nil {
		t.Fatal(err)
	}
	if exists(filepath.Join(src, "a")) {
		t.Fatalf("src/a/b emptied by the move should be removed:\n%s", out)
	}
	if !exists(filepath.Join(src, "keep", "9-clip.bbbbbb.mp4")) {
		t.Fatal("src/keep still has a file and must survive")
	}
	if !exists(src) {
		t.Fatal("src still holds keep/ and must survive")
	}
	if !strings.Contains(out, "removed empty folder") {
		t.Fatalf("expected a removed-folder notice:\n%s", out)
	}
}

func TestConfirmAbortChangesNothing(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src")
	dst := filepath.Join(root, "dst")
	write(t, filepath.Join(src, "a.aaaaaa.jpg"), "SRC")

	out, _, err := run(t, Options{
		Sources: []string{src}, Target: dst,
		Confirm: func(string) (bool, error) { return false, nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Aborted") {
		t.Fatalf("expected an abort message:\n%s", out)
	}
	if exists(filepath.Join(dst, "a.aaaaaa.jpg")) {
		t.Fatal("abort must not copy anything")
	}
}
