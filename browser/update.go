package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// version is set at build time (-ldflags "-X main.version=v0.1.0").
var version = "dev"

// releasesURL lists the project's GitHub releases.
const releasesURL = "https://api.github.com/repos/Unggoy1/OpenLink/releases?per_page=20"

// releasePages prefixes every release link the app will open.
const releasePages = "https://github.com/Unggoy1/OpenLink/releases/"

// UpdateInfo describes a newer release, if there is one.
type UpdateInfo struct {
	Current   string `json:"current"`
	Latest    string `json:"latest"`
	URL       string `json:"url"`
	Available bool   `json:"available"`
}

// Version returns the app version.
func (a *App) Version() string { return version }

// CheckUpdate looks for a newer release on GitHub. Pre-releases count only when
// the running version is itself a pre-release (testers stay on the test track).
// Errors are ignored: no network simply means no notice.
func (a *App) CheckUpdate() UpdateInfo {
	info := UpdateInfo{Current: version}
	cur, ok := parseSemver(version)
	if !ok {
		return info // dev builds never nag
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, releasesURL, nil)
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return info
	}
	defer resp.Body.Close()
	var releases []release
	if resp.StatusCode != http.StatusOK || json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&releases) != nil {
		return info
	}
	return pickUpdate(info, cur, releases)
}

type release struct {
	Tag        string `json:"tag_name"`
	URL        string `json:"html_url"`
	Draft      bool   `json:"draft"`
	Prerelease bool   `json:"prerelease"`
}

// pickUpdate fills info with the newest release newer than cur.
func pickUpdate(info UpdateInfo, cur semver, releases []release) UpdateInfo {
	best := cur
	for _, r := range releases {
		v, ok := parseSemver(r.Tag)
		// The link is opened in the browser: only this project's release pages.
		if !ok || r.Draft || (r.Prerelease && cur.pre == "") || !strings.HasPrefix(r.URL, releasePages) {
			continue
		}
		if v.newer(best) {
			best, info.Latest, info.URL, info.Available = v, r.Tag, r.URL, true
		}
	}
	return info
}

type semver struct {
	major, minor, patch int
	pre                 string // "" for a release
}

// parseSemver accepts vMAJOR.MINOR.PATCH[-PRE]; build metadata is ignored.
func parseSemver(s string) (semver, bool) {
	s = strings.TrimPrefix(strings.SplitN(s, "+", 2)[0], "v")
	core, pre, _ := strings.Cut(s, "-")
	parts := strings.Split(core, ".")
	if len(parts) != 3 {
		return semver{}, false
	}
	var n [3]int
	for i, p := range parts {
		v, err := strconv.Atoi(p)
		if err != nil || v < 0 {
			return semver{}, false
		}
		n[i] = v
	}
	return semver{n[0], n[1], n[2], pre}, true
}

// newer reports whether v > o by semver precedence.
func (v semver) newer(o semver) bool {
	if v.major != o.major {
		return v.major > o.major
	}
	if v.minor != o.minor {
		return v.minor > o.minor
	}
	if v.patch != o.patch {
		return v.patch > o.patch
	}
	switch {
	case v.pre == o.pre:
		return false
	case v.pre == "":
		return true // a release is newer than its pre-releases
	case o.pre == "":
		return false
	}
	return prereleaseNewer(v.pre, o.pre)
}

// prereleaseNewer compares dot-separated identifiers: numeric ones numerically,
// others lexically, and a longer list wins when all shared ones are equal.
func prereleaseNewer(a, b string) bool {
	as, bs := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < len(as) && i < len(bs); i++ {
		ai, aerr := strconv.Atoi(as[i])
		bi, berr := strconv.Atoi(bs[i])
		switch {
		case aerr == nil && berr == nil:
			if ai != bi {
				return ai > bi
			}
		case aerr == nil:
			return false // numeric identifiers sort before alphanumeric ones
		case berr == nil:
			return true
		case as[i] != bs[i]:
			return as[i] > bs[i]
		}
	}
	return len(as) > len(bs)
}
