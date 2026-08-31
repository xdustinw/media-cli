package hashcmd

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xdustinw/media-cli/internal/imgmeta"
	"github.com/xdustinw/media-cli/internal/media"
)

func TestParseMethod(t *testing.T) {
	if m, err := ParseMethod(""); err != nil || m != MethodAuto {
		t.Fatalf("empty => %q, %v (want auto)", m, err)
	}
	for _, ok := range []string{"ffmpeg", "FFMPEG", " md5 ", "sha-10m"} {
		if _, err := ParseMethod(ok); err != nil {
			t.Errorf("ParseMethod(%q): %v", ok, err)
		}
	}
	if _, err := ParseMethod("crc32"); err == nil {
		t.Fatal("expected an error for an unknown method")
	}
}

func runMethod(t *testing.T, root string, method Method) (stdout string) {
	t.Helper()
	var o, e bytes.Buffer
	err := Run(context.Background(), Options{
		Targets: []string{root}, Extensions: []string{".png"}, Method: method,
		NameLength: 6, AssumeYes: true,
		Stdout: &o, Stderr: &e, Logger: quietLogger(),
	})
	if err != nil {
		t.Fatalf("run %s: %v (stderr %s)", method, err, e.String())
	}
	return o.String()
}

// TestMethodRenameOnly: md5 renames by content hash but writes no mc.hash tag.
func TestMethodRenameOnly(t *testing.T) {
	root := t.TempDir()
	writeImage(t, filepath.Join(root, "pic.png"))

	runMethod(t, root, MethodMD5)

	got := onlyMatch(t, root, "pic.*.png")
	slot := strings.TrimSuffix(strings.TrimPrefix(filepath.Base(got), "pic."), ".png")
	if len(slot) != 6 {
		t.Fatalf("expected a 6-char hash slot, got %q", filepath.Base(got))
	}
	// The md5 of the raw bytes should start with that slot.
	raw, _ := os.ReadFile(got)
	if sum := fmt.Sprintf("%x", md5.Sum(raw)); !strings.HasPrefix(sum, slot) {
		t.Fatalf("name slot %q is not the md5 prefix (%s)", slot, sum)
	}
	// No metadata was written.
	if _, err := imgmeta.Read(got, "mc.hash"); err == nil {
		t.Fatalf("md5 method must not write mc.hash into %s", got)
	}

	// Second run is a no-op.
	if out := runMethod(t, root, MethodMD5); !strings.Contains(out, "Nothing to do") {
		t.Fatalf("second run not idempotent:\n%s", out)
	}
}

// TestMethod10MHashesPrefixOnly: md5-10m only reads the first 10 MiB.
func TestMethod10MHashesPrefixOnly(t *testing.T) {
	root := t.TempDir()
	big := make([]byte, byteCap10M+4<<20) // 14 MiB
	for i := range big {
		big[i] = byte(i * 7)
	}
	path := filepath.Join(root, "clip.bin")
	if err := os.WriteFile(path, big, 0o644); err != nil {
		t.Fatal(err)
	}

	full, err := MethodMD5.digest(path, media.KindUnknown)
	if err != nil {
		t.Fatal(err)
	}
	capped, err := MethodMD510M.digest(path, media.KindUnknown)
	if err != nil {
		t.Fatal(err)
	}
	if full == capped {
		t.Fatal("md5 and md5-10m should differ for a >10 MiB file")
	}
	want := md5.Sum(big[:byteCap10M])
	if capped != hex.EncodeToString(want[:]) {
		t.Fatalf("md5-10m = %s, want md5(first 10 MiB) = %x", capped, want)
	}
}

// TestAutoFallsBackToMD5: the default (auto) method hashes a non-media file
// with md5-10m after the ffmpeg attempt fails.
func TestAutoFallsBackToMD5(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "notes.bin"), []byte("plain data, not media"), 0o644); err != nil {
		t.Fatal(err)
	}
	var o, e bytes.Buffer
	err := Run(context.Background(), Options{
		Targets: []string{root}, Extensions: []string{".bin"}, // Method zero value = MethodAuto
		NameLength: 6, AssumeYes: true, Recursive: true,
		Stdout: &o, Stderr: &e, Logger: quietLogger(),
	})
	if err != nil {
		t.Fatalf("run: %v (stderr %s)", err, e.String())
	}
	if !strings.Contains(o.String(), "[md5-10m]") {
		t.Fatalf("expected an md5-10m fallback marker:\n%s", o.String())
	}
	got := onlyMatch(t, root, "notes.*.bin")
	raw, _ := os.ReadFile(got)
	slot := strings.TrimSuffix(strings.TrimPrefix(filepath.Base(got), "notes."), ".bin")
	if sum := fmt.Sprintf("%x", md5.Sum(raw)); !strings.HasPrefix(sum, slot) {
		t.Fatalf("slot %q not the md5 prefix of %s", slot, sum)
	}
}

// TestMultipleTargetsAndSelect: several targets are merged and --select filters.
func TestMultipleTargetsAndSelect(t *testing.T) {
	root := t.TempDir()
	a := filepath.Join(root, "a")
	b := filepath.Join(root, "b")
	writeImage(t, mkdirFile(t, a, "keep-me.png"))
	writeImage(t, mkdirFile(t, a, "skip.png"))
	writeImage(t, mkdirFile(t, b, "keep-too.png"))

	var o, e bytes.Buffer
	err := Run(context.Background(), Options{
		Targets: []string{a, b}, Extensions: []string{".png"}, Method: MethodMD5,
		Select: "name=keep*", NameLength: 6, AssumeYes: true, Recursive: true,
		Stdout: &o, Stderr: &e, Logger: quietLogger(),
	})
	if err != nil {
		t.Fatalf("run: %v (%s)", err, e.String())
	}
	if _, err := filepathGlob1(a, "keep-me.*.png"); err != nil {
		t.Fatalf("keep-me should have been hashed: %v", err)
	}
	if _, err := filepathGlob1(b, "keep-too.*.png"); err != nil {
		t.Fatalf("keep-too (2nd target) should have been hashed: %v", err)
	}
	if m, _ := filepath.Glob(filepath.Join(a, "skip.*.png")); len(m) != 0 {
		t.Fatalf("skip.png should have been filtered out by --select")
	}
}

func mkdirFile(t *testing.T, dir, name string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	return filepath.Join(dir, name)
}

func filepathGlob1(dir, pat string) (string, error) {
	m, _ := filepath.Glob(filepath.Join(dir, pat))
	if len(m) != 1 {
		return "", fmt.Errorf("want 1 match for %s, got %v", pat, m)
	}
	return m[0], nil
}

// TestMethodReplacesStaleNameHash: a different short hash already in the name is
// replaced, keeping the stem.
func TestMethodReplacesStaleNameHash(t *testing.T) {
	root := t.TempDir()
	writeImage(t, filepath.Join(root, "pic.aaaaaa.png"))

	out := runMethod(t, root, MethodMD5)
	if !strings.Contains(out, "replaces .aaaaaa") {
		t.Fatalf("expected a 'replaces .aaaaaa' note:\n%s", out)
	}
	got := onlyMatch(t, root, "pic.*.png")
	if strings.Contains(filepath.Base(got), "aaaaaa") {
		t.Fatalf("stale hash slot not replaced: %s", filepath.Base(got))
	}
}
