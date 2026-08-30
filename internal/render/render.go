// Package render turns simple column/tree data into TOON, JSON or CSV output.
package render

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	toon "github.com/toon-format/toon-go"
)

// Format is an output format name.
type Format string

const (
	TOON Format = "toon"
	JSON Format = "json"
	CSV  Format = "csv"
)

// ParseFormat validates a --format value.
func ParseFormat(s string, allowed ...Format) (Format, error) {
	f := Format(strings.ToLower(strings.TrimSpace(s)))
	if f == "" {
		f = TOON
	}
	for _, a := range allowed {
		if f == a {
			return f, nil
		}
	}
	return "", fmt.Errorf("unsupported format %q (want %s)", s, joinFormats(allowed))
}

func joinFormats(fs []Format) string {
	s := make([]string, len(fs))
	for i, f := range fs {
		s[i] = string(f)
	}
	return strings.Join(s, "/")
}

// OM is an insertion-ordered map that marshals to a JSON object preserving key
// order and to a TOON object.
type OM struct {
	keys []string
	vals map[string]any
}

// NewOM builds an ordered map. Pairs are key, value, key, value, ...
func NewOM(pairs ...any) *OM {
	o := &OM{vals: map[string]any{}}
	for i := 0; i+1 < len(pairs); i += 2 {
		o.Set(fmt.Sprint(pairs[i]), pairs[i+1])
	}
	return o
}

// Set appends or replaces a key.
func (o *OM) Set(k string, v any) *OM {
	if _, ok := o.vals[k]; !ok {
		o.keys = append(o.keys, k)
	}
	o.vals[k] = v
	return o
}

// SetIf sets k only when cond is true.
func (o *OM) SetIf(cond bool, k string, v any) *OM {
	if cond {
		o.Set(k, v)
	}
	return o
}

// Len reports the number of keys.
func (o *OM) Len() int { return len(o.keys) }

// MarshalJSON renders an ordered JSON object.
func (o *OM) MarshalJSON() ([]byte, error) {
	var b bytes.Buffer
	b.WriteByte('{')
	for i, k := range o.keys {
		if i > 0 {
			b.WriteByte(',')
		}
		kb, _ := json.Marshal(k)
		b.Write(kb)
		b.WriteByte(':')
		vb, err := json.Marshal(o.vals[k])
		if err != nil {
			return nil, err
		}
		b.Write(vb)
	}
	b.WriteByte('}')
	return b.Bytes(), nil
}

// TOON renders o (recursively converting nested *OM to toon.Object).
func (o *OM) TOON() (string, error) {
	return toon.MarshalString(o.toonValue(), toon.WithIndent(2))
}

func (o *OM) toonValue() toon.Object {
	fields := make([]toon.Field, 0, len(o.keys))
	for _, k := range o.keys {
		fields = append(fields, toon.Field{Key: k, Value: toonize(o.vals[k])})
	}
	return toon.NewObject(fields...)
}

func toonize(v any) any {
	switch t := v.(type) {
	case *OM:
		return t.toonValue()
	case []*OM:
		out := make([]toon.Object, len(t))
		for i, e := range t {
			out[i] = e.toonValue()
		}
		return out
	default:
		return v
	}
}

// Table is columnar data: one header, many rows.
type Table struct {
	Columns []string
	Rows    [][]any
}

// Encode writes the table in the given format to w.
func (t Table) Encode(w io.Writer, f Format) error {
	switch f {
	case JSON:
		objs := make([]*OM, len(t.Rows))
		for i, row := range t.Rows {
			o := NewOM()
			for c, col := range t.Columns {
				if c < len(row) {
					o.Set(col, row[c])
				}
			}
			objs[i] = o
		}
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(objs)

	case CSV:
		cw := csv.NewWriter(w)
		if err := cw.Write(t.Columns); err != nil {
			return err
		}
		for _, row := range t.Rows {
			rec := make([]string, len(t.Columns))
			for c := range t.Columns {
				if c < len(row) {
					rec[c] = Stringify(row[c])
				}
			}
			if err := cw.Write(rec); err != nil {
				return err
			}
		}
		cw.Flush()
		return cw.Error()

	default: // TOON
		objs := make([]toon.Object, len(t.Rows))
		for i, row := range t.Rows {
			fields := make([]toon.Field, 0, len(t.Columns))
			for c, col := range t.Columns {
				var v any
				if c < len(row) {
					v = row[c]
				}
				fields = append(fields, toon.Field{Key: col, Value: toonEmpty(v)})
			}
			objs[i] = toon.NewObject(fields...)
		}
		s, err := toon.MarshalString(objs, toon.WithIndent(2))
		if err != nil {
			return err
		}
		_, err = io.WriteString(w, s+"\n")
		return err
	}
}

// toonEmpty renders nil and empty slices as "" so TOON output stays clean.
func toonEmpty(v any) any {
	switch t := v.(type) {
	case nil:
		return ""
	case []string:
		if len(t) == 0 {
			return ""
		}
	case []any:
		if len(t) == 0 {
			return ""
		}
	}
	return v
}

// Stringify flattens a cell value for CSV.
func Stringify(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case []string:
		return strings.Join(t, "; ")
	case fmt.Stringer:
		return t.String()
	default:
		return fmt.Sprint(t)
	}
}
