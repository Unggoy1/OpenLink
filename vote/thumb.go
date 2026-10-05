package vote

import (
	"regexp"
	"strings"
)

// Map thumbnails come only from Halo Infinite's public UGC blob host, and the
// URL is computed from the map's asset and version IDs. A ballot never carries
// a URL, only the reference "<map asset>/<map version>"; the app builds the
// URLs from it, so a server cannot make players' apps fetch anything else.
const thumbHost = "https://blobs-infiniteugc.svc.halowaypoint.com/ugcstorage/map/"

const uuidPattern = `[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}`

var thumbRef = regexp.MustCompile(`^(` + uuidPattern + `)/(` + uuidPattern + `)$`)

// MapThumbRef is the thumbnail reference for a map, "" for invalid IDs.
func MapThumbRef(asset, version string) string {
	ref := strings.ToLower(asset) + "/" + strings.ToLower(version)
	if !thumbRef.MatchString(ref) {
		return ""
	}
	return ref
}

// ValidThumbRef reports whether ref is empty or a well-formed reference.
func ValidThumbRef(ref string) bool { return ref == "" || thumbRef.MatchString(ref) }

// ThumbURLs returns the URLs to try for a reference, in order. Maps store
// their thumbnail as either .jpg (most) or .png, so both are tried.
// Nil for an invalid reference.
func ThumbURLs(ref string) []string {
	if !thumbRef.MatchString(ref) {
		return nil
	}
	base := thumbHost + ref + "/images/thumbnail"
	return []string{base + ".jpg", base + ".png"}
}
