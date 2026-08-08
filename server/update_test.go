package server

import "testing"

// The comparison is the whole feature: get it wrong in one direction and every
// instance nags forever about the version it is already running, get it wrong
// in the other and nobody is ever told about anything.
func TestIsNewer(t *testing.T) {
	cases := []struct {
		latest, current string
		want            bool
		why             string
	}{
		{"v1.0.1", "v1.0.0", true, "a patch release is newer"},
		{"v1.1.0", "v1.0.9", true, "minor beats a higher patch"},
		{"v2.0.0", "v1.9.9", true, "major beats everything below it"},

		{"v1.0.0", "v1.0.0", false, "the same release is not an update"},
		{"v1.0.0", "1.0.0", false, "the v prefix is spelling, not a version — this is the local-build case"},
		{"1.0.0", "v1.0.0", false, "and the same the other way round"},
		{"v1.0.0", "v1.0.1", false, "an older tag is never offered"},
		{"v1.0.9", "v1.1.0", false, "patch does not beat minor"},

		{"v1.0.1", "dev", false, "a build nobody released has nothing to compare against"},
		{"v1.0.1", "", false, "and neither has an empty version"},
		{"desktop-v0.1.0", "v1.0.0", false, "the desktop app has its own tag line and is not this server"},
		{"v1.0.1-rc1", "v1.0.0", false, "a pre-release is not what anybody is waiting for"},
		{"latest", "v1.0.0", false, "not a version"},
		{"v1.0", "v1.0.0", false, "two parts is not a release tag here"},
	}
	for _, c := range cases {
		if got := isNewer(c.latest, c.current); got != c.want {
			t.Errorf("isNewer(%q, %q) = %v, want %v — %s", c.latest, c.current, got, c.want, c.why)
		}
	}
}

func TestReleaseVersionRejectsJunk(t *testing.T) {
	for _, s := range []string{"", "dev", "v", "v1", "v1.2", "v1.2.3.4", "va.b.c", "v1.-2.3", "desktop-v1.0.0", "v1.0.0-beta", "v1.0.0+build"} {
		if _, ok := releaseVersion(s); ok {
			t.Errorf("releaseVersion(%q) accepted it, want rejected", s)
		}
	}
	for _, s := range []string{"1.0.0", "v1.0.0", "v0.0.1", "v10.20.30", " v1.2.3 "} {
		if _, ok := releaseVersion(s); !ok {
			t.Errorf("releaseVersion(%q) rejected it, want accepted", s)
		}
	}
}
