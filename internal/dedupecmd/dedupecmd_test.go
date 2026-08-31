package dedupecmd

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func write(t *testing.T, path, content string, mod time.Time) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	if !mod.IsZero() {
		if err := os.Chtimes(path, mod, mod); err != nil {
			t.Fatal(err)
		}
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

func TestParseMethod(t *testing.T) {
	for in, want := range map[string]Method{
		"": MethodInteractive, "i": MethodInteractive,
		"l": MethodLongerName, "longer-name": MethodLongerName,
		"n": MethodNewer, "o": MethodOlder, "OLDER": MethodOlder,
	} {
		if got, err := ParseMethod(in); err != nil || got != want {
			t.Errorf("ParseMethod(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	if _, err := ParseMethod("random"); err == nil {
		t.Fatal("expected an error for an unknown method")
	}
}

func TestNoDuplicates(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "a.aaaaaa.jpg"), "A", time.Time{})
	write(t, filepath.Join(root, "b.bbbbbb.jpg"), "B", time.Time{})
	out, _, err := run(t, Options{Folders: []string{root}, Method: MethodNewer})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "No duplicate sets") {
		t.Fatalf("got: %s", out)
	}
}

func TestLongerNameWins(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "a", "x.abcabc.jpg"), "DUP", time.Time{})
	write(t, filepath.Join(root, "b", "a-much-longer-name.abcabc.jpg"), "DUP", time.Time{})

	if _, _, err := run(t, Options{Folders: []string{root}, Method: MethodLongerName}); err != nil {
		t.Fatal(err)
	}
	if exists(filepath.Join(root, "a", "x.abcabc.jpg")) {
		t.Fatal("short-named copy should have been deleted")
	}
	if !exists(filepath.Join(root, "b", "a-much-longer-name.abcabc.jpg")) {
		t.Fatal("longer-named copy should have been kept")
	}
}

func TestNewerAndOlderWins(t *testing.T) {
	old := time.Now().Add(-48 * time.Hour)
	newer := time.Now().Add(-1 * time.Hour)

	for _, tc := range []struct {
		method Method
		kept   string
	}{
		{MethodNewer, "new.dddddd.jpg"},
		{MethodOlder, "old.dddddd.jpg"},
	} {
		root := t.TempDir()
		write(t, filepath.Join(root, "old.dddddd.jpg"), "D", old)
		write(t, filepath.Join(root, "new.dddddd.jpg"), "D", newer)

		if _, _, err := run(t, Options{Folders: []string{root}, Method: tc.method}); err != nil {
			t.Fatal(err)
		}
		if !exists(filepath.Join(root, tc.kept)) {
			t.Fatalf("%s: %s should have been kept", tc.method, tc.kept)
		}
		others := 0
		for _, n := range []string{"old.dddddd.jpg", "new.dddddd.jpg"} {
			if exists(filepath.Join(root, n)) {
				others++
			}
		}
		if others != 1 {
			t.Fatalf("%s: expected exactly one survivor", tc.method)
		}
	}
}

func TestInteractiveKeepsChosenAndSkips(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "1.eeeeee.jpg"), "E", time.Time{})
	write(t, filepath.Join(root, "2.eeeeee.jpg"), "E", time.Time{})
	write(t, filepath.Join(root, "3.eeeeee.jpg"), "E", time.Time{})
	// second set — will be skipped
	write(t, filepath.Join(root, "p.ffffff.jpg"), "F", time.Time{})
	write(t, filepath.Join(root, "q.ffffff.jpg"), "F", time.Time{})

	answers := map[string]int{"eeeeee": 2, "ffffff": 0} // keep #2 of set e; skip set f
	_, _, err := run(t, Options{
		Folders: []string{root},
		Ask: func(prompt string, _ int) (int, error) {
			for h, a := range answers {
				if strings.Contains(prompt, h) {
					return a, nil
				}
			}
			return 0, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if exists(filepath.Join(root, "1.eeeeee.jpg")) || exists(filepath.Join(root, "3.eeeeee.jpg")) {
		t.Fatal("unchosen copies of set e should be gone")
	}
	if !exists(filepath.Join(root, "2.eeeeee.jpg")) {
		t.Fatal("chosen copy #2 of set e should remain")
	}
	if !exists(filepath.Join(root, "p.ffffff.jpg")) || !exists(filepath.Join(root, "q.ffffff.jpg")) {
		t.Fatal("skipped set f must be left untouched")
	}
}

func TestSelectNarrowsSets(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "keep-1.aaaaaa.jpg"), "A", time.Time{})
	write(t, filepath.Join(root, "keep-2.aaaaaa.jpg"), "A", time.Time{})
	write(t, filepath.Join(root, "other-1.bbbbbb.jpg"), "B", time.Time{})
	write(t, filepath.Join(root, "other-2.bbbbbb.jpg"), "B", time.Time{})

	if _, _, err := run(t, Options{
		Folders: []string{root}, Method: MethodLongerName, Select: "name=other*",
	}); err != nil {
		t.Fatal(err)
	}
	// The "aaaaaa" set was filtered out entirely — both copies survive.
	if !exists(filepath.Join(root, "keep-1.aaaaaa.jpg")) || !exists(filepath.Join(root, "keep-2.aaaaaa.jpg")) {
		t.Fatal("--select should have excluded the aaaaaa set")
	}
	// The "bbbbbb" set was deduped.
	surv := 0
	for _, n := range []string{"other-1.bbbbbb.jpg", "other-2.bbbbbb.jpg"} {
		if exists(filepath.Join(root, n)) {
			surv++
		}
	}
	if surv != 1 {
		t.Fatalf("expected 1 survivor in the bbbbbb set, got %d", surv)
	}
}

func TestConfirmAbortDeletesNothing(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "a.cccccc.jpg"), "C", time.Time{})
	write(t, filepath.Join(root, "b.cccccc.jpg"), "C", time.Time{})

	out, _, err := run(t, Options{
		Folders: []string{root}, Method: MethodNewer,
		Confirm: func(string) (bool, error) { return false, nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Aborted") {
		t.Fatalf("expected abort message: %s", out)
	}
	if !exists(filepath.Join(root, "a.cccccc.jpg")) || !exists(filepath.Join(root, "b.cccccc.jpg")) {
		t.Fatal("abort must not delete anything")
	}
}
