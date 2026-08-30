package query

import (
	"fmt"
	"strings"
)

// SortKey is one clause of a --sort-by expression.
type SortKey struct {
	Field string
	Desc  bool
}

// ParseSort parses "name, rating desc, size desc, modifiedAt" into ordered keys.
func ParseSort(expr string) ([]SortKey, error) {
	expr = strings.TrimSpace(expr)
	if expr == "" {
		return nil, nil
	}
	var keys []SortKey
	for _, part := range strings.Split(expr, ",") {
		toks := strings.Fields(part)
		if len(toks) == 0 {
			continue
		}
		k := SortKey{Field: strings.ToLower(toks[0])}
		if len(toks) >= 2 {
			switch strings.ToLower(toks[1]) {
			case "desc":
				k.Desc = true
			case "asc":
			default:
				return nil, fmt.Errorf("sort direction must be asc or desc, got %q", toks[1])
			}
		}
		keys = append(keys, k)
	}
	return keys, nil
}

// Less returns a comparison function for stable multi-key sorting. Records with
// an absent value for a key always sort after those that have one, regardless
// of asc/desc.
func Less(keys []SortKey) func(a, b Fields) bool {
	return func(a, b Fields) bool {
		for _, k := range keys {
			va, vb := a.QueryField(k.Field), b.QueryField(k.Field)

			if (va.Kind == KindAbsent) != (vb.Kind == KindAbsent) {
				return vb.Kind == KindAbsent // a present, b absent -> a first
			}
			c := va.compare(vb)
			if c == 0 {
				continue
			}
			if k.Desc {
				return c > 0
			}
			return c < 0
		}
		return false
	}
}
