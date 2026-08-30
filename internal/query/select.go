package query

import (
	"fmt"
	"regexp"
	"strings"
)

// Selector is a parsed --select expression: an OR of AND-groups of conditions.
type Selector struct {
	groups []andGroup
}

type andGroup struct {
	conds []condition
}

type condition struct {
	field string
	op    string
	value string
}

var (
	reAnd = regexp.MustCompile(`(?i)\s+and\s+`)
	reOr  = regexp.MustCompile(`(?i)\s+or\s+`)
	// operators, longest first so ">=" is not read as ">"
	ops = []string{">=", "<=", "!=", "=", ">", "<"}
)

// ParseSelect parses expr. An empty expression yields a nil Selector, which
// matches everything.
func ParseSelect(expr string) (*Selector, error) {
	expr = strings.TrimSpace(expr)
	if expr == "" {
		return nil, nil
	}
	sel := &Selector{}
	for _, orPart := range reOr.Split(expr, -1) {
		g := andGroup{}
		for _, andPart := range reAnd.Split(orPart, -1) {
			c, err := parseCondition(strings.TrimSpace(andPart))
			if err != nil {
				return nil, err
			}
			g.conds = append(g.conds, c)
		}
		sel.groups = append(sel.groups, g)
	}
	return sel, nil
}

func parseCondition(s string) (condition, error) {
	for _, op := range ops {
		if i := strings.Index(s, op); i > 0 {
			field := strings.TrimSpace(s[:i])
			value := strings.TrimSpace(s[i+len(op):])
			if field == "" || value == "" {
				break
			}
			return condition{
				field: strings.ToLower(field),
				op:    op,
				value: strings.Trim(value, `"'`),
			}, nil
		}
	}
	return condition{}, fmt.Errorf("cannot parse condition %q (expected field<op>value)", s)
}

// Match reports whether f satisfies the selector. A nil Selector matches.
func (s *Selector) Match(f Fields) bool {
	if s == nil {
		return true
	}
	for _, g := range s.groups {
		all := true
		for _, c := range g.conds {
			if !f.QueryField(c.field).matchOp(c.op, c.value) {
				all = false
				break
			}
		}
		if all {
			return true
		}
	}
	return false
}

// Fields lists every field name referenced by the selector (for validation /
// deciding whether a deep probe is needed).
func (s *Selector) Fields() []string {
	if s == nil {
		return nil
	}
	seen := map[string]struct{}{}
	var out []string
	for _, g := range s.groups {
		for _, c := range g.conds {
			if _, ok := seen[c.field]; !ok {
				seen[c.field] = struct{}{}
				out = append(out, c.field)
			}
		}
	}
	return out
}
