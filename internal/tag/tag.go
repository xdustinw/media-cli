// Package tag parses and represents an ordered set of metadata key/value pairs
// (as used by `mc set`).
package tag

import (
	"fmt"
	"strings"
)

// Pair is one metadata assignment.
type Pair struct {
	Key   string
	Value string
}

// Set is an ordered list of assignments.
type Set []Pair

// Keys returns the keys in order.
func (s Set) Keys() []string {
	out := make([]string, len(s))
	for i, p := range s {
		out[i] = p.Key
	}
	return out
}

// Values returns the values in order.
func (s Set) Values() []string {
	out := make([]string, len(s))
	for i, p := range s {
		out[i] = p.Value
	}
	return out
}

// String renders the set as "k1=v1, k2=v2".
func (s Set) String() string {
	parts := make([]string, len(s))
	for i, p := range s {
		parts[i] = p.Key + "=" + p.Value
	}
	return strings.Join(parts, ", ")
}

// Parse reads "rating=3,author=Adam" into a Set. Whitespace around keys is
// trimmed. A comma-separated fragment with no '=' is treated as a continuation
// of the previous value, so `author=Doe, Jane,rating=3` yields
// {author: "Doe, Jane", rating: "3"}. A leading/trailing quote pair around a
// value is stripped.
func Parse(spec string) (Set, error) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return nil, fmt.Errorf("no key=value pairs given")
	}

	var set Set
	for _, frag := range strings.Split(spec, ",") {
		eq := strings.IndexByte(frag, '=')
		if eq < 0 {
			if len(set) == 0 {
				return nil, fmt.Errorf("bad assignment %q (expected key=value)", strings.TrimSpace(frag))
			}
			set[len(set)-1].Value += "," + strings.TrimRight(frag, " \t")
			continue
		}
		key := strings.TrimSpace(frag[:eq])
		if key == "" {
			return nil, fmt.Errorf("empty key in %q", strings.TrimSpace(frag))
		}
		set = append(set, Pair{Key: key, Value: strings.TrimSpace(frag[eq+1:])})
	}

	// Strip a wrapping quote pair now that any comma-continuations are joined.
	for i := range set {
		if v := set[i].Value; len(v) >= 2 && v[0] == '"' && v[len(v)-1] == '"' {
			set[i].Value = v[1 : len(v)-1]
		}
	}
	return set, nil
}
