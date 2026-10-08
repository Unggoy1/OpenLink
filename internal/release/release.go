// Package release checks the project's GitHub releases for a newer version of
// one program. The OpenLink app and OpenLink Server share one tag series
// (vMAJOR.MINOR.PATCH), but a release carries only the programs that changed,
// so each program looks for the newest release that contains its own file.
package release

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// listURL lists the project's GitHub releases, newest first. A page of 50
// reaches past a long run of releases that skip a program.
const listURL = "https://api.github.com/repos/Unggoy1/OpenLink/releases?per_page=50"

// Pages prefixes every release link a program will show or open.
const Pages = "https://github.com/Unggoy1/OpenLink/releases/"

// Release file names, as the release workflow uploads them.
const (
	ServerAsset = "OpenLink-Server-windows-amd64.zip"
)

// AppAsset is the OpenLink app's file for an OS and architecture, e.g.
// OpenLink-windows-amd64.exe.
func AppAsset(goos, goarch string) string {
	name := "OpenLink-" + goos + "-" + goarch
	if goos == "windows" {
		name += ".exe"
	}
	return name
}

// Update describes a newer release, if there is one.
type Update struct {
	Current   string `json:"current"`
	Latest    string `json:"latest"`
	URL       string `json:"url"`
	Available bool   `json:"available"`
}

// Check looks for a release newer than current that contains asset.
// Pre-releases count only when current is itself a pre-release (testers stay
// on the test track). Errors are ignored: no network simply means no notice,
// and a version that is not semver (a dev build) never gets one.
func Check(ctx context.Context, current, asset string) Update {
	info := Update{Current: current}
	cur, ok := ParseSemver(current)
	if !ok {
		return info
	}
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, listURL, nil)
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return info
	}
	defer resp.Body.Close()
	var releases []Release
	if resp.StatusCode != http.StatusOK || json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&releases) != nil {
		return info
	}
	return Pick(info, cur, asset, releases)
}

// Release is a GitHub release as the API lists it.
type Release struct {
	Tag        string  `json:"tag_name"`
	URL        string  `json:"html_url"`
	Draft      bool    `json:"draft"`
	Prerelease bool    `json:"prerelease"`
	Assets     []Asset `json:"assets"`
}

// Asset is one file of a release.
type Asset struct {
	Name string `json:"name"`
}

func (r Release) has(asset string) bool {
	for _, a := range r.Assets {
		if a.Name == asset {
			return true
		}
	}
	return false
}

// Pick fills info with the newest release newer than cur that contains asset.
func Pick(info Update, cur Semver, asset string, releases []Release) Update {
	best := cur
	for _, r := range releases {
		v, ok := ParseSemver(r.Tag)
		// The link is opened in the browser: only this project's release pages.
		if !ok || r.Draft || (r.Prerelease && cur.pre == "") || !strings.HasPrefix(r.URL, Pages) || !r.has(asset) {
			continue
		}
		if v.Newer(best) {
			best, info.Latest, info.URL, info.Available = v, r.Tag, r.URL, true
		}
	}
	return info
}

// Semver is a parsed vMAJOR.MINOR.PATCH[-PRE] version.
type Semver struct {
	major, minor, patch int
	pre                 string // "" for a release
}

// ParseSemver accepts vMAJOR.MINOR.PATCH[-PRE]; build metadata is ignored.
func ParseSemver(s string) (Semver, bool) {
	s = strings.TrimPrefix(strings.SplitN(s, "+", 2)[0], "v")
	core, pre, _ := strings.Cut(s, "-")
	parts := strings.Split(core, ".")
	if len(parts) != 3 {
		return Semver{}, false
	}
	var n [3]int
	for i, p := range parts {
		v, err := strconv.Atoi(p)
		if err != nil || v < 0 {
			return Semver{}, false
		}
		n[i] = v
	}
	return Semver{n[0], n[1], n[2], pre}, true
}

// Newer reports whether v > o by semver precedence.
func (v Semver) Newer(o Semver) bool {
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
