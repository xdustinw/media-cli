// Package toon renders a tiny subset of TOON (Token-Oriented Object Notation)
// used for previewing file mutations before they are applied.
//
// Only what the CLI needs is implemented: scalar "key: value" lines and uniform
// arrays of records rendered as a tabular block:
//
//	renames[2]{from,to}:
//	  a.mp4,a.1a2b3c.mp4
//	  b.mkv,b.4d5e6f.mkv
package toon

import (
	"sort"
	"strings"
)

// Field is a single scalar entry rendered as "key: value".
type Field struct {
	Key   string
	Value string
}

// Table is a uniform array of records rendered as a TOON tabular block.
type Table struct {
	Name    string
	Columns []string
	Rows    [][]string
}

// Document is an ordered collection of scalar fields followed by tables.
type Document struct {
	Fields []Field
	Tables []Table
}

// AddField appends a scalar field.
func (d *Document) AddField(key, value string) {
	d.Fields = append(d.Fields, Field{Key: key, Value: value})
}

// AddTable appends a table.
func (d *Document) AddTable(t Table) {
	d.Tables = append(d.Tables, t)
}

// String renders the document as TOON text.
func (d *Document) String() string {
	var b strings.Builder
	for _, f := range d.Fields {
		b.WriteString(f.Key)
		b.WriteString(": ")
		b.WriteString(quote(f.Value))
		b.WriteByte('\n')
	}
	for _, t := range d.Tables {
		b.WriteString(t.render())
	}
	return b.String()
}

func (t Table) render() string {
	var b strings.Builder
	b.WriteString(t.Name)
	b.WriteByte('[')
	b.WriteString(itoa(len(t.Rows)))
	b.WriteString("]{")
	b.WriteString(strings.Join(t.Columns, ","))
	b.WriteString("}:\n")
	for _, row := range t.Rows {
		b.WriteString("  ")
		cells := make([]string, len(row))
		for i, c := range row {
			cells[i] = quote(c)
		}
		b.WriteString(strings.Join(cells, ","))
		b.WriteByte('\n')
	}
	return b.String()
}

// SortRows sorts a table's rows by the given column index, in place.
func (t Table) SortRows(col int) {
	sort.SliceStable(t.Rows, func(i, j int) bool {
		return t.Rows[i][col] < t.Rows[j][col]
	})
}

// quote wraps a value in double quotes when it contains a delimiter, leading or
// trailing whitespace, or is empty; embedded quotes are doubled.
func quote(s string) string {
	if s == "" {
		return `""`
	}
	needs := strings.ContainsAny(s, ",\n\"{}[]:") ||
		s != strings.TrimSpace(s)
	if !needs {
		return s
	}
	return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
