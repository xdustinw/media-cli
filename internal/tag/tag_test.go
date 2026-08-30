package tag

import "testing"

func TestParse(t *testing.T) {
	got, err := Parse(" rating=3 , author = Adam ")
	if err != nil {
		t.Fatal(err)
	}
	if got.String() != "rating=3, author=Adam" {
		t.Fatalf("got %q", got.String())
	}
	if len(got) != 2 || got[0].Key != "rating" || got[1].Value != "Adam" {
		t.Fatalf("bad parse: %+v", got)
	}
}

func TestParseCommaInValue(t *testing.T) {
	got, err := Parse(`author=Doe, Jane,rating=3`)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Value != "Doe, Jane" || got[1].Value != "3" {
		t.Fatalf("comma-in-value not handled: %+v", got)
	}
}

func TestParseQuotedValue(t *testing.T) {
	got, _ := Parse(`title="Hello, World"`)
	if len(got) != 1 || got[0].Value != "Hello, World" {
		t.Fatalf("quoted value: %+v", got)
	}
}

func TestParseErrors(t *testing.T) {
	for _, s := range []string{"", "novalue", "=v"} {
		if _, err := Parse(s); err == nil {
			t.Errorf("Parse(%q) should fail", s)
		}
	}
}
