package toon

import "testing"

func TestDocumentRender(t *testing.T) {
	d := &Document{}
	d.AddField("metadata_key", "mc.hash")
	d.AddTable(Table{
		Name:    "changes",
		Columns: []string{"file", "rename_to"},
		Rows: [][]string{
			{"a.mp4", "a.1a2b3c.mp4"},
			{"nested/b, weird.mkv", "nested/b, weird.4d5e6f.mkv"},
		},
	})

	got := d.String()
	want := "metadata_key: mc.hash\n" +
		"changes[2]{file,rename_to}:\n" +
		"  a.mp4,a.1a2b3c.mp4\n" +
		"  \"nested/b, weird.mkv\",\"nested/b, weird.4d5e6f.mkv\"\n"
	if got != want {
		t.Fatalf("render mismatch:\n got: %q\nwant: %q", got, want)
	}
}

func TestQuote(t *testing.T) {
	cases := map[string]string{
		"plain":    "plain",
		"":         `""`,
		"a,b":      `"a,b"`,
		" leading": `" leading"`,
		`he"llo`:   `"he""llo"`,
		"key: val": `"key: val"`,
	}
	for in, want := range cases {
		if got := quote(in); got != want {
			t.Errorf("quote(%q) = %q, want %q", in, got, want)
		}
	}
}
