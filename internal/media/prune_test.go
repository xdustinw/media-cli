package media

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPruneEmptyDirs(t *testing.T) {
	root := t.TempDir()
	// root/keep/file.txt      -> keep/ stays
	// root/empty/deep/deeper  -> all three pruned, plus root/empty
	// root/alsoempty          -> pruned
	mustMkdir(t, filepath.Join(root, "empty", "deep", "deeper"))
	mustMkdir(t, filepath.Join(root, "alsoempty"))
	mustWrite(t, filepath.Join(root, "keep", "file.txt"), "x")

	removed := PruneEmptyDirs(root)

	if _, err := os.Stat(filepath.Join(root, "empty")); !os.IsNotExist(err) {
		t.Fatal("root/empty (and its empty subtree) should be gone")
	}
	if _, err := os.Stat(filepath.Join(root, "alsoempty")); !os.IsNotExist(err) {
		t.Fatal("root/alsoempty should be gone")
	}
	if _, err := os.Stat(filepath.Join(root, "keep", "file.txt")); err != nil {
		t.Fatalf("root/keep/file.txt must survive: %v", err)
	}
	// root itself still has keep/, so it is not removed.
	if _, err := os.Stat(root); err != nil {
		t.Fatalf("non-empty root must survive: %v", err)
	}
	if len(removed) != 4 { // empty/deep/deeper, empty/deep, empty, alsoempty
		t.Fatalf("expected 4 removed dirs, got %d: %v", len(removed), removed)
	}
}

func TestPruneEmptyDirsRemovesEmptyRoot(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "gone")
	mustMkdir(t, filepath.Join(root, "a", "b"))

	PruneEmptyDirs(root)

	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatal("a fully-empty root should be removed too")
	}
}

func TestPruneEmptyDirsNeverRemovesCwd(t *testing.T) {
	dir := t.TempDir()
	restore, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(restore) })

	PruneEmptyDirs(".", dir)

	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("the current working directory must never be pruned: %v", err)
	}
}

func mustMkdir(t *testing.T, p string) {
	t.Helper()
	if err := os.MkdirAll(p, 0o755); err != nil {
		t.Fatal(err)
	}
}

func mustWrite(t *testing.T, p, content string) {
	t.Helper()
	mustMkdir(t, filepath.Dir(p))
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
