package release

import "testing"

func TestSemverOrder(t *testing.T) {
	ordered := []string{"v0.1.0-alpha.1", "v0.1.0-alpha.2", "v0.1.0-alpha.10", "v0.1.0-beta.1", "v0.1.0-rc.1", "v0.1.0", "v0.1.1", "v0.2.0", "v1.0.0"}
	for i := 0; i+1 < len(ordered); i++ {
		a, okA := ParseSemver(ordered[i])
		b, okB := ParseSemver(ordered[i+1])
		if !okA || !okB {
			t.Fatalf("parse %s / %s", ordered[i], ordered[i+1])
		}
		if !b.Newer(a) || a.Newer(b) {
			t.Fatalf("%s should be newer than %s", ordered[i+1], ordered[i])
		}
	}
	for _, bad := range []string{"dev", "dev-abc123", "v1.2", "1.2.x"} {
		if _, ok := ParseSemver(bad); ok {
			t.Fatalf("%q parsed as semver", bad)
		}
	}
}

func page(tag string) string { return Pages + "tag/" + tag }

func TestPickOnlyOpensReleasePages(t *testing.T) {
	app := AppAsset("windows", "amd64")
	files := []Asset{{Name: app}}
	cur, _ := ParseSemver("v0.7.1")
	got := Pick(Update{Current: "v0.7.1"}, cur, app, []Release{
		{Tag: "v0.9.0", URL: "search-ms:query=evil", Assets: files},
		{Tag: "v0.8.5", URL: "https://evil.example/OpenLink/releases/tag/v0.8.5", Assets: files},
		{Tag: "v0.8.0", URL: page("v0.8.0"), Assets: files},
	})
	if !got.Available || got.Latest != "v0.8.0" || got.URL != page("v0.8.0") {
		t.Fatalf("picked %+v", got)
	}
}

// A server-only release must not tell players to update the app, and the
// server looks only at releases that carry the server package.
func TestPickSkipsReleasesWithoutTheProgram(t *testing.T) {
	app := AppAsset("windows", "amd64")
	releases := []Release{
		{Tag: "v0.8.2", URL: page("v0.8.2"), Assets: []Asset{{Name: ServerAsset}, {Name: "SHA256SUMS"}}},
		{Tag: "v0.8.1", URL: page("v0.8.1"), Assets: []Asset{{Name: ServerAsset}}},
		{Tag: "v0.8.0", URL: page("v0.8.0"), Assets: []Asset{{Name: app}, {Name: ServerAsset}}},
	}
	cur, _ := ParseSemver("v0.8.0")
	if got := Pick(Update{Current: "v0.8.0"}, cur, app, releases); got.Available {
		t.Fatalf("app offered a server-only release: %+v", got)
	}
	if got := Pick(Update{Current: "v0.8.0"}, cur, ServerAsset, releases); !got.Available || got.Latest != "v0.8.2" {
		t.Fatalf("server picked %+v", got)
	}
}

func TestPickPrereleaseTrack(t *testing.T) {
	releases := []Release{
		{Tag: "v0.9.0-alpha.1", URL: page("v0.9.0-alpha.1"), Prerelease: true, Assets: []Asset{{Name: ServerAsset}}},
	}
	stable, _ := ParseSemver("v0.8.0")
	if got := Pick(Update{}, stable, ServerAsset, releases); got.Available {
		t.Fatalf("stable build offered a pre-release: %+v", got)
	}
	tester, _ := ParseSemver("v0.8.0-alpha.3")
	if got := Pick(Update{}, tester, ServerAsset, releases); !got.Available {
		t.Fatal("pre-release build was not offered the newer pre-release")
	}
}

func TestAppAsset(t *testing.T) {
	if got := AppAsset("windows", "amd64"); got != "OpenLink-windows-amd64.exe" {
		t.Fatal(got)
	}
	if got := AppAsset("linux", "amd64"); got != "OpenLink-linux-amd64" {
		t.Fatal(got)
	}
}
