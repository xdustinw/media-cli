package selfupdate

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestAssetName(t *testing.T) {
	got := AssetName()
	if !strings.HasPrefix(got, "mc-"+runtime.GOOS+"-"+runtime.GOARCH) {
		t.Fatalf("unexpected asset name %q", got)
	}
	if runtime.GOOS == "windows" && !strings.HasSuffix(got, ".exe") {
		t.Fatalf("windows asset should end in .exe: %q", got)
	}
}

func TestReleaseFindAsset(t *testing.T) {
	r := Release{Assets: []Asset{
		{Name: "mc-linux-amd64"},
		{Name: AssetName()},
		{Name: "checksums.txt"},
	}}
	if a := r.FindAsset(); a == nil || a.Name != AssetName() {
		t.Fatalf("FindAsset returned %v", a)
	}
	if (Release{}).FindAsset() != nil {
		t.Fatal("empty release should find nothing")
	}
}

func TestApplyReplacesBinary(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix replacement path")
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte("#!/bin/sh\necho new\n"))
	}))
	defer srv.Close()

	dir := t.TempDir()
	exe := filepath.Join(dir, "mc")
	if err := os.WriteFile(exe, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := Apply(context.Background(), srv.URL, exe, "0.1.0"); err != nil {
		t.Fatalf("apply: %v", err)
	}
	b, _ := os.ReadFile(exe)
	if !strings.Contains(string(b), "echo new") {
		t.Fatalf("binary not replaced: %q", b)
	}
	if fi, _ := os.Stat(exe); fi.Mode().Perm()&0o100 == 0 {
		t.Fatal("replacement is not executable")
	}
}

func TestLatestReleaseParsing(t *testing.T) {
	// LatestRelease targets the real GitHub host, so exercise the JSON shape
	// the handler would return via the exported types instead.
	payload := `{"tag_name":"v1.4.0","html_url":"h","assets":[
		{"name":"mc-linux-amd64","browser_download_url":"u","size":10}]}`
	var r Release
	if err := json.Unmarshal([]byte(payload), &r); err != nil {
		t.Fatal(err)
	}
	if r.TagName != "v1.4.0" || len(r.Assets) != 1 || r.Assets[0].Size != 10 {
		t.Fatalf("bad parse: %+v", r)
	}
}

func TestTargetPathResolves(t *testing.T) {
	p, err := TargetPath()
	if err != nil {
		t.Fatal(err)
	}
	if !filepath.IsAbs(p) {
		t.Fatalf("expected absolute path, got %q", p)
	}
}
