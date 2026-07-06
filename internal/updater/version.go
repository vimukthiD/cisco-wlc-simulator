package updater

import (
	"strconv"
	"strings"
)

// isNewer reports whether the latest release tag is a newer version than the
// currently running build. Comparison is lenient semver: a leading "v" and any
// pre-release/build suffix (e.g. the "-2-gabc123-dirty" that `git describe`
// appends to dev builds) are ignored, so only the numeric X.Y.Z core is
// compared. An unparseable current version (e.g. the default "dev") sorts as
// 0.0.0, so a non-release build always sees an update as available.
func isNewer(latest, current string) bool {
	return compareVersions(latest, current) > 0
}

// compareVersions returns -1, 0, or 1 as a is older than, equal to, or newer
// than b, comparing only the dotted numeric core of each version.
func compareVersions(a, b string) int {
	av := parseVersion(a)
	bv := parseVersion(b)
	for i := 0; i < 3; i++ {
		if av[i] != bv[i] {
			if av[i] < bv[i] {
				return -1
			}
			return 1
		}
	}
	return 0
}

// parseVersion extracts up to three numeric components (major, minor, patch)
// from a version string, tolerating a leading "v" and any suffix after the
// first "-", "+", or non-numeric character. Missing/unparseable components are 0.
func parseVersion(s string) [3]int {
	var out [3]int
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "v")
	s = strings.TrimPrefix(s, "V")
	// Drop any pre-release / build / git-describe suffix.
	if i := strings.IndexAny(s, "-+ "); i >= 0 {
		s = s[:i]
	}
	for i, part := range strings.SplitN(s, ".", 3) {
		// Keep only the leading digits of the component.
		digits := part
		for j := 0; j < len(part); j++ {
			if part[j] < '0' || part[j] > '9' {
				digits = part[:j]
				break
			}
		}
		if n, err := strconv.Atoi(digits); err == nil {
			out[i] = n
		}
	}
	return out
}
