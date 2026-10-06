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

func TestPickUpdateOnlyOpensReleasePages(t *testing.T) {
	cur, _ := parseSemver("v0.7.1")
	got := pickUpdate(UpdateInfo{Current: "v0.7.1"}, cur, []release{
		{Tag: "v0.9.0", URL: "search-ms:query=evil"},
		{Tag: "v0.8.5", URL: "https://evil.example/OpenLink/releases/tag/v0.8.5"},
		{Tag: "v0.8.0", URL: "https://github.com/Unggoy1/OpenLink/releases/tag/v0.8.0"},
	})
	if !got.Available || got.Latest != "v0.8.0" || got.URL != "https://github.com/Unggoy1/OpenLink/releases/tag/v0.8.0" {
		t.Fatalf("picked %+v", got)
	}
}
