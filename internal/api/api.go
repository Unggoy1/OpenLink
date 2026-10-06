// Package api holds the JSON types and constants shared by the directory,
// host agent and player app.
package api

import (
	"fmt"
	"os"
	"strings"
	"time"
	"unicode/utf8"

	"halocommunity/vote"
)

const (
	// DefaultDirectory is the community directory the tools use unless
	// OPENLINK_DIRECTORY or a flag/config says otherwise. The OpenLink app
	// has its own copy (browser/settings.go).
	DefaultDirectory = "https://openlink-dir.unggoy.xyz"
	// GamePort is the UDP port a LAN client always dials on the host.
	GamePort = 1343
	// DiscoveryPort is the UDP port of the LAN beacon broadcast.
	DiscoveryPort = 7117
	// BeaconInterval matches the retail server's beacon period.
	BeaconInterval = time.Second
	// BeaconFreshFor is how old a beacon may be before a server stops being listed as joinable.
	BeaconFreshFor = 15 * time.Second
	// MaxBeaconBytes bounds stored beacons; the retail beacon is 79 bytes.
	MaxBeaconBytes = 512
	// MaxNameRunes bounds server names in the directory.
	MaxNameRunes = 48
	// MaxGameNameLength is the longest name in Halo's in-game server list: the
	// LAN beacon holds 48 UTF-16 units including the terminator (O080).
	MaxGameNameLength = 47
)

// GameName is the name OpenLink Server puts in Halo's in-game server list for
// a server listed as name: printable ASCII only, surrounding spaces trimmed,
// cut to 47 characters. "" means nothing usable was left, and the game shows
// the host's PC name. The game displays it in capitals.
func GameName(name string) string {
	b := make([]byte, 0, len(name))
	for _, r := range name {
		if r >= 0x20 && r <= 0x7e {
			b = append(b, byte(r))
		}
	}
	out := strings.TrimSpace(string(b))
	if len(out) > MaxGameNameLength {
		out = strings.TrimRight(out[:MaxGameNameLength], " ")
	}
	return out
}

// ValidServerName reports whether the directory accepts name: 1-48
// characters after trimming spaces, no control characters.
func ValidServerName(name string) bool {
	name = strings.TrimSpace(name)
	if name == "" || utf8.RuneCountInString(name) > MaxNameRunes {
		return false
	}
	for _, r := range name {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}

// MaxDescriptionRunes bounds the optional listing description a host adds.
const MaxDescriptionRunes = 120

// ValidDescription reports whether the directory accepts a server
// description: up to 120 characters, no control characters ("" = none).
func ValidDescription(s string) bool {
	if utf8.RuneCountInString(s) > MaxDescriptionRunes {
		return false
	}
	for _, r := range s {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}

// RegisterRequest is sent by a host agent to list a server.
type RegisterRequest struct {
	Name        string `json:"name"`
	Host        string `json:"host,omitempty"` // empty: the directory uses the request's source IP
	Port        int    `json:"port"`
	Build       string `json:"build"`
	Region      string `json:"region,omitempty"`
	Description string `json:"description,omitempty"` // ValidDescription
}

// RegisterResponse carries the server ID and the secret used for later updates.
type RegisterResponse struct {
	ID    string `json:"id"`
	Token string `json:"token"`
	Host  string `json:"host"`
}

// Heartbeat refreshes a listing. Beacon is the latest captured beacon datagram.
type Heartbeat struct {
	Status      string `json:"status"`           // starting, ready, exited, simulated
	Players     int    `json:"players"`          // -1 when unknown
	Beacon      []byte `json:"beacon,omitempty"` // base64 in JSON
	BeaconAgeMS int64  `json:"beacon_age_ms"`    // age when the heartbeat was sent
	Proxy       bool   `json:"proxy"`            // the agent fronts the server and answers probes
	Match       *Match `json:"match,omitempty"`  // what the server is playing; nil when unknown
}

// Match phases, as reported by a host agent with host control.
const (
	PhaseLobby    = "lobby"     // in the lobby; Entry, if set, is the next match
	PhaseVoting   = "voting"    // players are voting for the next match
	PhaseStarting = "starting"  // Entry is about to start
	PhaseInGame   = "in_game"   // Entry is being played
	PhasePostGame = "post_game" // Entry just ended
)

// Match is what a server is playing, from its host agent's playlist. The
// directory passes it on only if Valid.
type Match struct {
	Phase string `json:"phase"`
	Entry string `json:"entry,omitempty"` // playlist entry ID
	Name  string `json:"name,omitempty"`  // display name (the entry's name, or its ID)
	Thumb string `json:"thumb,omitempty"` // map thumbnail reference (vote.ThumbURLs builds the URLs)
}

// Valid reports whether m is safe to list: a known phase, and an entry, name
// and thumbnail reference within the ballot limits.
func (m *Match) Valid() bool {
	switch m.Phase {
	case PhaseLobby, PhaseVoting, PhaseStarting, PhaseInGame, PhasePostGame:
	default:
		return false
	}
	for _, s := range []string{m.Entry, m.Name} {
		if len(s) > vote.MaxNameBytes || !utf8.ValidString(s) ||
			strings.IndexFunc(s, func(r rune) bool { return r < 0x20 || r == 0x7f }) >= 0 {
			return false
		}
	}
	return vote.ValidThumbRef(m.Thumb)
}

// Reachability values in ServerInfo.
const (
	ReachUnknown     = "unknown"     // not checked (only proxy-mode hosts can be probed)
	ReachOK          = "ok"          // the directory's probe was answered
	ReachUnreachable = "unreachable" // probes went unanswered: port forward or firewall
)

// ServerInfo is the public view of a listing.
type ServerInfo struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	Host         string    `json:"host"`
	Port         int       `json:"port"`
	Build        string    `json:"build"`
	Region       string    `json:"region,omitempty"`
	Description  string    `json:"description,omitempty"`
	Status       string    `json:"status"`
	Players      int       `json:"players"`
	Joinable     bool      `json:"joinable"`
	LastSeen     time.Time `json:"last_seen"`
	BeaconAgeMS  int64     `json:"beacon_age_ms"`
	Proxy        bool      `json:"proxy"`
	Reachability string    `json:"reachability"` // ReachUnknown, ReachOK or ReachUnreachable
	CheckedAt    time.Time `json:"checked_at,omitempty"`
	Match        *Match    `json:"match,omitempty"` // from the latest heartbeat; nil when unknown
	// Listed is set only in a host's own view of its listing (GET
	// /v1/servers/{id}): whether players see it yet. The directory shows a
	// server once its reachability probe has answered.
	Listed bool `json:"listed,omitempty"`
}

// ProbePrefix marks probe datagrams. A host agent in proxy mode (and the
// simulator) answers them by echoing; game datagrams never start with it
// (they begin with a random 12-byte nonce).
const ProbePrefix = "HICOMM-PROBE "

// BeaconResponse returns the latest beacon for a server.
type BeaconResponse struct {
	Beacon []byte `json:"beacon"`
	AgeMS  int64  `json:"age_ms"`
}

// Getenv reads the environment variable OPENLINK_<name>, falling back to the
// project's older HICOMM_<name> so existing deployments keep working.
func Getenv(name string) string {
	if v := os.Getenv("OPENLINK_" + name); v != "" {
		return v
	}
	return os.Getenv("HICOMM_" + name)
}

// Ports returns the game and discovery ports used by simulate mode and the
// player app. The retail game always uses 1343/7117; OPENLINK_DEV_PORTS="g,d"
// moves only our tools, so they can be tested next to a running server.
func Ports() (game, discovery int) {
	game, discovery = GamePort, DiscoveryPort
	if v := Getenv("DEV_PORTS"); v != "" {
		var g, d int
		if _, err := fmt.Sscanf(v, "%d,%d", &g, &d); err == nil && g > 0 && g < 65536 && d > 0 && d < 65536 {
			game, discovery = g, d
		}
	}
	return game, discovery
}
