// Package query parses and evaluates the --select filter and --sort-by ordering
// used by `mc list`.
package query

import (
	"strconv"
	"strings"
	"time"
)

// Kind is the dynamic type of a Value.
type Kind int

const (
	KindAbsent Kind = iota
	KindString
	KindNumber
	KindTime
	KindStringList
)

// Value is a single field value read from a record.
type Value struct {
	Kind Kind
	S    string
	N    float64
	T    time.Time
	L    []string
}

// Absent is the zero Value: a field that is not present.
var Absent = Value{Kind: KindAbsent}

func String(s string) Value  { return Value{Kind: KindString, S: s} }
func Number(n float64) Value { return Value{Kind: KindNumber, N: n} }
func Time(t time.Time) Value { return Value{Kind: KindTime, T: t} }
func List(l []string) Value  { return Value{Kind: KindStringList, L: l} }

// Fields is the value source a Selector or sort comparator reads from.
type Fields interface {
	QueryField(name string) Value
}

// matchOp evaluates "value <op> literal" for a filter condition.
func (v Value) matchOp(op string, lit string) bool {
	switch v.Kind {
	case KindAbsent:
		return op == "!=" // an absent field only satisfies "!= anything"

	case KindNumber:
		want, ok := parseNumber(lit)
		if !ok {
			return false
		}
		return cmpNum(v.N, want, op)

	case KindTime:
		want, ok := parseTime(lit)
		if !ok {
			return false
		}
		return cmpNum(float64(v.T.Unix()), float64(want.Unix()), op)

	case KindStringList:
		for _, item := range v.L {
			if cmpStr(item, lit, "=") {
				return op == "=" || op == ">=" || op == "<="
			}
		}
		return op == "!="

	default: // KindString
		return cmpStr(v.S, lit, op)
	}
}

// compare orders two values of (ideally) the same kind: -1, 0, 1. Absent sorts
// after any present value.
func (v Value) compare(o Value) int {
	if v.Kind == KindAbsent || o.Kind == KindAbsent {
		switch {
		case v.Kind == o.Kind:
			return 0
		case v.Kind == KindAbsent:
			return 1
		default:
			return -1
		}
	}
	switch v.Kind {
	case KindNumber:
		return sign(v.N - o.N)
	case KindTime:
		return v.T.Compare(o.T)
	case KindStringList:
		return strings.Compare(strings.Join(v.L, ","), strings.Join(o.L, ","))
	default:
		return strings.Compare(strings.ToLower(v.S), strings.ToLower(o.S))
	}
}

func cmpNum(a, b float64, op string) bool {
	switch op {
	case "=":
		return a == b
	case "!=":
		return a != b
	case ">":
		return a > b
	case "<":
		return a < b
	case ">=":
		return a >= b
	case "<=":
		return a <= b
	}
	return false
}

func cmpStr(s, lit, op string) bool {
	s, lit = strings.ToLower(s), strings.ToLower(lit)
	switch op {
	case "=":
		if strings.ContainsAny(lit, "*?") {
			return globMatch(lit, s)
		}
		return s == lit
	case "!=":
		return !cmpStr(s, lit, "=")
	case ">":
		return s > lit
	case "<":
		return s < lit
	case ">=":
		return s >= lit
	case "<=":
		return s <= lit
	}
	return false
}

// globMatch reports whether s matches a shell-style pattern using '*' (any run
// of runes, separators included) and '?' (any single rune). Unlike
// filepath.Match it is separator-agnostic and behaves identically on every OS,
// so `name=*foo*` works on Windows too.
func globMatch(pattern, s string) bool {
	p, t := []rune(pattern), []rune(s)
	var pi, ti int
	star, starT := -1, 0
	for ti < len(t) {
		switch {
		case pi < len(p) && (p[pi] == '?' || p[pi] == t[ti]):
			pi++
			ti++
		case pi < len(p) && p[pi] == '*':
			star, starT = pi, ti
			pi++
		case star >= 0:
			pi = star + 1
			starT++
			ti = starT
		default:
			return false
		}
	}
	for pi < len(p) && p[pi] == '*' {
		pi++
	}
	return pi == len(p)
}

// parseNumber accepts a plain number or one with a size suffix (k/m/g/t, 1024-based).
func parseNumber(s string) (float64, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, false
	}
	// Drop an optional trailing "b"/"B" so "500mb" == "500m".
	if n := len(s); n > 0 && (s[n-1] == 'b' || s[n-1] == 'B') {
		s = s[:n-1]
	}
	mult := 1.0
	if n := len(s); n > 0 {
		switch s[n-1] | 0x20 {
		case 'k':
			mult, s = 1<<10, s[:n-1]
		case 'm':
			mult, s = 1<<20, s[:n-1]
		case 'g':
			mult, s = 1<<30, s[:n-1]
		case 't':
			mult, s = 1<<40, s[:n-1]
		}
	}
	n, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil {
		return 0, false
	}
	return n * mult, true
}

func parseTime(s string) (time.Time, bool) {
	s = strings.TrimSpace(s)
	for _, layout := range []string{
		"2006-01-02T15:04:05Z07:00",
		"2006-01-02T15:04:05",
		"2006-01-02 15:04:05",
		"2006-01-02 15:04",
		"2006-01-02",
		"2006/01/02",
	} {
		if t, err := time.ParseInLocation(layout, s, time.Local); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

func sign(f float64) int {
	switch {
	case f < 0:
		return -1
	case f > 0:
		return 1
	default:
		return 0
	}
}
