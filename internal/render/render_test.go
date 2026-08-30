package render

import (
	"bytes"
	"strings"
	"testing"
)

func table() Table {
	return Table{
		Columns: []string{"filename", "size", "rating", "tags"},
		Rows: [][]any{
			{"a.mp4", "12MB", 5, []string{"x", "y"}},
			{"b/c.jpg", "1.4GB", nil, []string{}},
		},
	}
}

func TestTableCSV(t *testing.T) {
	var b bytes.Buffer
	if err := table().Encode(&b, CSV); err != nil {
		t.Fatal(err)
	}
	got := b.String()
	want := "filename,size,rating,tags\n" +
		"a.mp4,12MB,5,x; y\n" +
		"b/c.jpg,1.4GB,,\n"
	if got != want {
		t.Fatalf("csv mismatch:\n got %q\nwant %q", got, want)
	}
}

func TestTableTOON(t *testing.T) {
	var b bytes.Buffer
	if err := table().Encode(&b, TOON); err != nil {
		t.Fatal(err)
	}
	got := b.String()
	if !strings.Contains(got, "filename: a.mp4") || !strings.Contains(got, "rating: 5") {
		t.Fatalf("toon missing rows: %s", got)
	}
	if !strings.Contains(got, `rating: ""`) {
		t.Fatalf("nil rating should render as empty string: %s", got)
	}
}

func TestTableJSON(t *testing.T) {
	var b bytes.Buffer
	if err := table().Encode(&b, JSON); err != nil {
		t.Fatal(err)
	}
	got := b.String()
	// key order preserved
	if strings.Index(got, `"filename"`) > strings.Index(got, `"size"`) {
		t.Fatalf("json key order not preserved: %s", got)
	}
}

func TestOMOrderedJSON(t *testing.T) {
	o := NewOM("z", 1, "a", 2).Set("m", NewOM("k", "v"))
	got, err := o.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	want := `{"z":1,"a":2,"m":{"k":"v"}}`
	if string(got) != want {
		t.Fatalf("got %s want %s", got, want)
	}
}

func TestParseFormat(t *testing.T) {
	if f, _ := ParseFormat("", TOON, JSON); f != TOON {
		t.Error("empty should default to toon")
	}
	if _, err := ParseFormat("xml", TOON, JSON); err == nil {
		t.Error("xml should be rejected")
	}
}
