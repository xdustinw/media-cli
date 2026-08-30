package selfupdate

import "testing"

func TestCompare(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"v1.2.0", "v1.2.0", 0},
		{"1.2.0", "v1.2.0", 0},
		{"v1.3.0", "v1.2.9", 1},
		{"v1.2.0", "v1.10.0", -1},
		{"v2.0.0", "v1.9.9", 1},
		{"v1.2.1", "v1.2", 1},
		{"v1.2.0", "v1.2.0-rc1", 1}, // release beats its own pre-release
		{"v1.2.0-rc2", "v1.2.0-rc1", 1},
		{"v0.1.0", "0.1.0-dev", 1},
		{"v0.1.0", "preview+abc.2026-08-30", 1}, // real release beats a preview
		{"v0.2.0", "v0.1.0", 1},
	}
	for _, c := range cases {
		if got := Compare(c.a, c.b); got != c.want {
			t.Errorf("Compare(%q,%q) = %d, want %d", c.a, c.b, got, c.want)
		}
		if got := Compare(c.b, c.a); got != -c.want {
			t.Errorf("Compare(%q,%q) = %d, want %d (antisymmetry)", c.b, c.a, got, -c.want)
		}
	}
}

func TestIsNewer(t *testing.T) {
	if !IsNewer("v0.2.0", "0.1.0-dev") {
		t.Error("0.2.0 should be newer than 0.1.0-dev")
	}
	if IsNewer("v0.1.0", "v0.1.0") {
		t.Error("equal versions are not newer")
	}
	if IsNewer("v0.1.0", "v0.2.0") {
		t.Error("older is not newer")
	}
}
