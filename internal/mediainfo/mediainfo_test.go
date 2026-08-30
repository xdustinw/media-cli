package mediainfo

import "testing"

func TestHumanSize(t *testing.T) {
	cases := map[int64]string{
		0:                      "0B",
		512:                    "512B",
		1024:                   "1.0KB",
		48 * 1024:              "48KB",
		1536 * 1024:            "1.5MB",
		12 * 1024 * 1024:       "12MB",
		3 * 1024 * 1024 * 1024: "3.0GB",
	}
	for in, want := range cases {
		if got := HumanSize(in); got != want {
			t.Errorf("HumanSize(%d) = %q, want %q", in, got, want)
		}
	}
}

func TestDecodeXP(t *testing.T) {
	// "68, 0, 80, 0, 0, 0" is UTF-16LE "DP" + NUL terminator.
	if got := decodeXP("XPKeywords", "68, 0, 80, 0, 0, 0"); got != "DP" {
		t.Errorf("decodeXP = %q, want DP", got)
	}
	// non-XP keys pass through untouched
	if got := decodeXP("Artist", "68, 0"); got != "68, 0" {
		t.Errorf("decodeXP mangled a non-XP value: %q", got)
	}
}

func TestPercentToStars(t *testing.T) {
	for p, want := range map[int]int{0: 0, 1: 1, 25: 2, 50: 3, 75: 4, 99: 5} {
		if got := percentToStars(p); got != want {
			t.Errorf("percentToStars(%d) = %d, want %d", p, got, want)
		}
	}
}

func TestIsNoiseTag(t *testing.T) {
	long := ""
	for i := 0; i < 40; i++ {
		long += "0, "
	}
	if !IsNoiseTag("0xEA1C", long) {
		t.Error("long hex byte array should be noise")
	}
	if IsNoiseTag("Rating", "4") {
		t.Error("named short tag is not noise")
	}
	if IsNoiseTag("0x4746", "4") {
		t.Error("short hex value is not noise")
	}
}

func TestCleanValue(t *testing.T) {
	if got := CleanValue("   4  \n  "); got != "4" {
		t.Errorf("CleanValue = %q", got)
	}
	if got := CleanValue("a\n  b\tc"); got != "a b c" {
		t.Errorf("CleanValue = %q", got)
	}
}
