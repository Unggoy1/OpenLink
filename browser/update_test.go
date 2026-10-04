package main

import "testing"

func TestSemverOrder(t *testing.T) {
	ordered := []string{"v0.1.0-alpha.1", "v0.1.0-alpha.2", "v0.1.0-alpha.10", "v0.1.0-beta.1", "v0.1.0-rc.1", "v0.1.0", "v0.1.1", "v0.2.0", "v1.0.0"}
	for i := 0; i+1 < len(ordered); i++ {
		a, okA := parseSemver(ordered[i])
		b, okB := parseSemver(ordered[i+1])
		if !okA || !okB {
			t.Fatalf("parse %s / %s", ordered[i], ordered[i+1])
		}
		if !b.newer(a) || a.newer(b) {
			t.Fatalf("%s should be newer than %s", ordered[i+1], ordered[i])
		}
	}
	for _, bad := range []string{"dev", "dev-abc123", "v1.2", "1.2.x"} {
		if _, ok := parseSemver(bad); ok {
			t.Fatalf("%q parsed as semver", bad)
		}
	}
}
