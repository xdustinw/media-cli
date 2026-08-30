package cmd

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestVersionCommand(t *testing.T) {
	var out, errb bytes.Buffer
	rootCmd.SetOut(&out)
	rootCmd.SetErr(&errb)
	rootCmd.SetArgs([]string{"version"})
	t.Cleanup(func() { rootCmd.SetArgs(nil) })

	if err := rootCmd.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("version: %v", err)
	}
	if !strings.HasPrefix(out.String(), "mc "+Version()) {
		t.Fatalf("unexpected output: %q", out.String())
	}
}

func TestHashHelp(t *testing.T) {
	var out bytes.Buffer
	rootCmd.SetOut(&out)
	rootCmd.SetArgs([]string{"hash", "--help"})
	t.Cleanup(func() { rootCmd.SetArgs(nil) })

	if err := rootCmd.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("hash --help: %v", err)
	}
	if !strings.Contains(out.String(), "mc.hash") {
		t.Fatalf("help missing tag mention: %q", out.String())
	}
}

func TestListInfoHelp(t *testing.T) {
	for _, tc := range []struct{ cmd, want string }{
		{"list", "--select"},
		{"info", "EXIF"},
		{"set", "key=value"},
		{"copy", "duplicate"},
		{"move", "duplicate"},
	} {
		var out bytes.Buffer
		rootCmd.SetOut(&out)
		rootCmd.SetArgs([]string{tc.cmd, "--help"})
		if err := rootCmd.ExecuteContext(context.Background()); err != nil {
			t.Fatalf("%s --help: %v", tc.cmd, err)
		}
		if !strings.Contains(out.String(), tc.want) {
			t.Fatalf("%s help missing %q: %s", tc.cmd, tc.want, out.String())
		}
	}
	rootCmd.SetArgs(nil)
}

func TestUpdateHelp(t *testing.T) {
	var out bytes.Buffer
	rootCmd.SetOut(&out)
	rootCmd.SetArgs([]string{"update", "--help"})
	t.Cleanup(func() { rootCmd.SetArgs(nil) })

	if err := rootCmd.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("update --help: %v", err)
	}
	if !strings.Contains(out.String(), "releases") || !strings.Contains(out.String(), "--preview") {
		t.Fatalf("update help unexpected: %q", out.String())
	}
}
