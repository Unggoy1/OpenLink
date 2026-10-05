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
	// HICOMM_DIRECTORY or a flag/config says otherwise. The OpenLink app
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
)

// RegisterRequest is sent by a host agent to list a server.
type RegisterRequest struct {
	Name   string `json:"name"`
	Host   string `json:"host,omitempty"` // empty: the directory uses the request's source IP
	Port   int    `json:"port"`
	Build  string `json:"build"`
	Region string `json:"region,omitempty"`
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
	Status       string    `json:"status"`
	Players      int       `json:"players"`
	Joinable     bool      `json:"joinable"`
	LastSeen     time.Time `json:"last_seen"`
	BeaconAgeMS  int64     `json:"beacon_age_ms"`
	Proxy        bool      `json:"proxy"`
	Reachability string    `json:"reachability"` // ReachUnknown, ReachOK or ReachUnreachable
	CheckedAt    time.Time `json:"checked_at,omitempty"`
	Match        *Match    `json:"match,omitempty"` // from the latest heartbeat; nil when unknown
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

// Ports returns the game and discovery ports used by simulate mode and the
// connector. The retail game always uses 1343/7117; HICOMM_DEV_PORTS="g,d"
// moves only our tools, so they can be tested next to a running server.
func Ports() (game, discovery int) {
	game, discovery = GamePort, DiscoveryPort
	if v := os.Getenv("HICOMM_DEV_PORTS"); v != "" {
		var g, d int
		if _, err := fmt.Sscanf(v, "%d,%d", &g, &d); err == nil && g > 0 && g < 65536 && d > 0 && d < 65536 {
			game, discovery = g, d
		}
	}
	return game, discovery
}
