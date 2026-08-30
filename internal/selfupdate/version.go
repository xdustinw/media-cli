package selfupdate

import (
	"strconv"
	"strings"
)

// NormalizeVersion strips a leading "v" and surrounding whitespace.
func NormalizeVersion(v string) string {
	return strings.TrimPrefix(strings.TrimSpace(v), "v")
}

// IsNewer reports whether remote is a strictly newer version than local.
func IsNewer(remote, local string) bool {
	return Compare(remote, local) > 0
}

// Compare orders two version strings, returning -1, 0 or 1. It parses a leading
// dotted-numeric core (e.g. "1.4.0") and treats any trailing "-suffix" as a
// pre-release that sorts before the same core without one (semver-style).
// Build metadata after "+" is ignored. Non-numeric leading tokens (e.g.
// "preview", "dev") sort as a pre-release of core 0.
func Compare(a, b string) int {
	ca, pa := splitVersion(NormalizeVersion(a))
	cb, pb := splitVersion(NormalizeVersion(b))

	for i := 0; i < len(ca) || i < len(cb); i++ {
		var x, y int
		if i < len(ca) {
			x = ca[i]
		}
		if i < len(cb) {
			y = cb[i]
		}
		if x != y {
			if x < y {
				return -1
			}
			return 1
		}
	}

	switch {
	case pa == "" && pb != "":
		return 1
	case pa != "" && pb == "":
		return -1
	default:
		return strings.Compare(pa, pb)
	}
}

// splitVersion returns the numeric core fields and the pre-release remainder.
func splitVersion(v string) (core []int, pre string) {
	if plus := strings.IndexByte(v, '+'); plus >= 0 {
		v = v[:plus] // drop build metadata
	}
	main := v
	if dash := strings.IndexByte(v, '-'); dash >= 0 {
		main, pre = v[:dash], v[dash+1:]
	}

	for _, f := range strings.Split(main, ".") {
		n, err := strconv.Atoi(f)
		if err != nil {
			// Non-numeric token (e.g. "preview"): treat as a pre-release.
			if pre == "" {
				pre = f
			}
			break
		}
		core = append(core, n)
	}
	if len(core) == 0 {
		core = []int{0}
		if pre == "" {
			pre = main
		}
	}
	return core, pre
}
