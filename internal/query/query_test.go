package query

import (
	"testing"
	"time"
)

type rec map[string]Value

func (r rec) QueryField(name string) Value {
	if v, ok := r[name]; ok {
		return v
	}
	return Absent
}

func TestParseNumber(t *testing.T) {
	cases := map[string]float64{
		"1024": 1024, "1k": 1024, "1K": 1024,
		"1m": 1 << 20, "1.5g": 1.5 * (1 << 30), "2t": 2 << 40,
		"500mb": 500 * (1 << 20),
	}
	for in, want := range cases {
		got, ok := parseNumber(in)
		if !ok || got != want {
			t.Errorf("parseNumber(%q) = %v,%v want %v", in, got, ok, want)
		}
	}
	if _, ok := parseNumber("abc"); ok {
		t.Error("expected failure on abc")
	}
}

func TestSelect(t *testing.T) {
	base := time.Date(2026, 8, 15, 0, 0, 0, 0, time.Local)
	r := rec{
		"name":       String("sample-01.mp4"),
		"size":       Number(2 * (1 << 30)),
		"rating":     Number(5),
		"modifiedat": Time(base),
		"tags":       List([]string{"landscape", "sunset"}),
	}

	must := func(expr string, want bool) {
		t.Helper()
		s, err := ParseSelect(expr)
		if err != nil {
			t.Fatalf("%s: parse: %v", expr, err)
		}
		if got := s.Match(r); got != want {
			t.Errorf("%s => %v, want %v", expr, got, want)
		}
	}

	must("name=sample*", true)
	must("name=*.mkv", false)
	must("rating=5 and size>1g", true)
	must("rating=5 and size>5g", false)
	must("rating>=4 and modifiedAt>2026-08-01", true)
	must("modifiedAt<2026-08-01", false)
	must("tags=sunset", true)
	must("tags=beach", false)
	must("rating=1 or name=sample*", true)
	must("missing=x", false)
	must("missing!=x", true)
}

func TestGlobMatch(t *testing.T) {
	cases := []struct {
		pat, s string
		want   bool
	}{
		{"*somestring*", "vid-somestring-1.mp4", true},
		{"*somestring*", "SomeString.jpg", false}, // caller lowercases; globMatch is literal
		{"3*adam*", "3-adam-trip.mp4", true},
		{"3*adam*", "adam-3.mp4", false},
		{"?oo.mp4", "foo.mp4", true},
		{"?oo.mp4", "fooo.mp4", false},
		{"*", "anything at all/with sep\\chars", true}, // '*' spans separators
		{"a*b*c", "axxbxxc", true},
		{"a*b*c", "axxbxx", false},
		{"exact.mp4", "exact.mp4", true},
		{"*.mp4", "a.mkv", false},
	}
	for _, c := range cases {
		if got := globMatch(c.pat, c.s); got != c.want {
			t.Errorf("globMatch(%q, %q) = %v, want %v", c.pat, c.s, got, c.want)
		}
	}
}

func TestSelectGlobCaseInsensitive(t *testing.T) {
	r := rec{"name": String("Trip-SomeString-2026.MP4")}
	s, err := ParseSelect("name=*somestring*")
	if err != nil {
		t.Fatal(err)
	}
	if !s.Match(r) {
		t.Fatal("name=*somestring* should match Trip-SomeString-2026.MP4 (case-insensitive)")
	}
}

func TestSort(t *testing.T) {
	keys, err := ParseSort("rating desc, size desc, name")
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != 3 || !keys[0].Desc || keys[0].Field != "rating" || keys[2].Desc {
		t.Fatalf("bad keys: %+v", keys)
	}

	a := rec{"rating": Number(5), "size": Number(10), "name": String("a")}
	b := rec{"rating": Number(3), "size": Number(99), "name": String("b")}
	c := rec{"rating": Number(5), "size": Number(10), "name": String("c")}
	less := Less(keys)
	if !less(a, b) {
		t.Error("a (rating 5) should sort before b (rating 3) descending")
	}
	if !less(a, c) {
		t.Error("a should sort before c on name tiebreak")
	}
	if _, err := ParseSort("name sideways"); err == nil {
		t.Error("expected error on bad direction")
	}
}

func TestSortAbsentLast(t *testing.T) {
	keys, _ := ParseSort("rating desc")
	less := Less(keys)
	rated := rec{"rating": Number(3)}
	unrated := rec{}
	if !less(rated, unrated) {
		t.Error("rated file must sort before unrated even under 'desc'")
	}
	if less(unrated, rated) {
		t.Error("unrated file must not sort before rated")
	}
}
