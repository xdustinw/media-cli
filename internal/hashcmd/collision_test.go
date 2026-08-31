package hashcmd

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// collisionFixture hashes one image, then drops a fresh un-hashed copy of the
// same content back in, so the next run's rename target already exists.
func collisionFixture(t *testing.T) (root, hashed, incoming string) {
	t.Helper()
	root = t.TempDir()
	writeImage(t, filepath.Join(root, "pic.png"))
	runMethod(t, root, MethodMD5)
	hashed = onlyMatch(t, root, "pic.*.png")

	b, err := os.ReadFile(hashed)
	if err != nil {
		t.Fatal(err)
	}
	incoming = filepath.Join(root, "pic.png")
	if err := os.WriteFile(incoming, b, 0o644); err != nil {
		t.Fatal(err)
	}
	return
}

func runColl(t *testing.T, root string, yes bool, on func(string, string) (CollisionAction, error)) string {
	t.Helper()
	var o, e bytes.Buffer
	err := Run(context.Background(), Options{
		Targets: []string{root}, Extensions: []string{".png"}, Method: MethodMD5,
		NameLength: 6, AssumeYes: yes, OnCollision: on,
		Confirm: func(string) (bool, error) { return true, nil },
		Stdout:  &o, Stderr: &e, Logger: quietLogger(),
	})
	if err != nil {
		t.Fatalf("run: %v (stderr %s)", err, e.String())
	}
	return o.String()
}

func exists2(p string) bool { _, err := os.Stat(p); return err == nil }

// -y => always delete the un-hashed file.
func TestCollisionAssumeYesDeletes(t *testing.T) {
	root, hashed, incoming := collisionFixture(t)
	out := runColl(t, root, true, nil)
	if !strings.Contains(out, "deleted") {
		t.Fatalf("expected a delete line:\n%s", out)
	}
	if exists2(incoming) {
		t.Fatal("the un-hashed pic.png should have been deleted")
	}
	if !exists2(hashed) {
		t.Fatal("the already-hashed copy must be kept")
	}
}

func TestCollisionDefaultChoiceIsDelete(t *testing.T) {
	root, hashed, incoming := collisionFixture(t)
	// callback returns the zero value (CollisionDelete) for an empty answer path
	runColl(t, root, false, func(string, string) (CollisionAction, error) {
		return CollisionDelete, nil
	})
	if exists2(incoming) || !exists2(hashed) {
		t.Fatal("default collision handling should delete the un-hashed file")
	}
}

func TestCollisionSkipKeepsBoth(t *testing.T) {
	root, hashed, incoming := collisionFixture(t)
	out := runColl(t, root, false, func(string, string) (CollisionAction, error) {
		return CollisionSkip, nil
	})
	if !strings.Contains(out, "kept") {
		t.Fatalf("expected a 'kept' line:\n%s", out)
	}
	if !exists2(incoming) || !exists2(hashed) {
		t.Fatal("skip must leave both files in place")
	}
}

func TestCollisionOverwriteConsumesIncoming(t *testing.T) {
	root, hashed, incoming := collisionFixture(t)
	out := runColl(t, root, false, func(string, string) (CollisionAction, error) {
		return CollisionOverwrite, nil
	})
	if strings.Contains(out, "deleted") || strings.Contains(out, "kept") {
		t.Fatalf("overwrite should take the normal rename path:\n%s", out)
	}
	if exists2(incoming) {
		t.Fatal("overwrite consumes the incoming file")
	}
	if !exists2(hashed) {
		t.Fatal("the hash-named file must still be present")
	}
}
