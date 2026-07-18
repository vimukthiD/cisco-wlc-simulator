package updater

import "testing"

func TestIsNewer(t *testing.T) {
	cases := []struct {
		latest, current string
		want            bool
	}{
		{"v0.0.10", "v0.0.9", true},
		{"v0.0.9", "v0.0.9", false},
		{"v0.0.9", "v0.0.10", false}, // never offer a downgrade
		{"v1.0.0", "v0.9.9", true},
		{"v0.1.0", "v0.0.9", true},
		{"v0.0.9", "dev", true},                     // unparseable current => update available
		{"v0.0.9", "v0.0.9-2-gabcdef", false},       // dev build of same release is not newer
		{"v0.0.10", "v0.0.9-2-gabcdef-dirty", true}, // dev build behind a newer release
		{"1.2.3", "1.2.2", true},                    // no leading v
		{"v2", "v1.9.9", true},                      // missing components on latest
		{"", "v0.0.1", false},                       // no latest => nothing to offer
	}
	for _, c := range cases {
		if got := isNewer(c.latest, c.current); got != c.want {
			t.Errorf("isNewer(%q, %q) = %v, want %v", c.latest, c.current, got, c.want)
		}
	}
}

func TestParseVersion(t *testing.T) {
	cases := []struct {
		in   string
		want [3]int
	}{
		{"v1.2.3", [3]int{1, 2, 3}},
		{"1.2.3", [3]int{1, 2, 3}},
		{"v0.0.9-2-gabcdef", [3]int{0, 0, 9}},
		{"v0.0.9-dirty", [3]int{0, 0, 9}},
		{"dev", [3]int{0, 0, 0}},
		{"v10.20.30", [3]int{10, 20, 30}},
		{"v1", [3]int{1, 0, 0}},
		{"v1.2", [3]int{1, 2, 0}},
		{"garbage", [3]int{0, 0, 0}},
	}
	for _, c := range cases {
		if got := parseVersion(c.in); got != c.want {
			t.Errorf("parseVersion(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}
