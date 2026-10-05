// Package vote is the playlist-voting protocol between a host agent in proxy
// mode and the players' OpenLink apps. Messages travel as small UDP datagrams
// on the game port, beside the game's own traffic, like reachability probes:
//
//   - the agent sends each connected player a Ballot (BallotPrefix + JSON)
//     about once a second while a vote is open and while its result is shown;
//   - the app answers with a Cast (CastPrefix + JSON) through the same socket
//     it forwards the player's game traffic with, so the agent can count one
//     vote per connected game session without any account or sign-in data.
//
// Game datagrams begin with a random 12-byte nonce, so these ASCII prefixes do
// not collide with them in practice; the forwarders still check the prefix and
// parse strictly before treating a datagram as a vote message.
package vote

import (
	"bytes"
	"encoding/json"
	"errors"
	"unicode/utf8"
)

const (
	BallotPrefix = "HICOMM-BALLOT "
	CastPrefix   = "HICOMM-VOTE "
	// MaxOptions is the most choices on one ballot.
	MaxOptions = 4
	// MaxNameBytes bounds an option's display name.
	MaxNameBytes = 80
	// MaxDatagram bounds an encoded message, well under a safe UDP payload.
	MaxDatagram = 1200
)

// Option is one map/mode pair on the ballot. ID is the playlist entry ID.
type Option struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Thumb string `json:"thumb,omitempty"` // map thumbnail reference (ThumbURLs), optional
}

// Ballot is the agent's view of the current vote, addressed to one player.
type Ballot struct {
	Round       uint64   `json:"round"`        // increases with every vote
	Options     []Option `json:"options"`      // 1..MaxOptions
	RemainingMS int64    `json:"remaining_ms"` // until the vote closes; 0 once closed
	Counts      []int    `json:"counts"`       // votes per option, same length as Options
	Mine        int      `json:"mine"`         // this player's choice, -1 if none
	Closed      bool     `json:"closed"`
	Winner      int      `json:"winner"`   // index into Options once closed, else -1
	StartMS     int64    `json:"start_ms"` // once closed: time until the match starts, -1 unknown
}

// Cast is a player's vote for one option of a round.
type Cast struct {
	Round  uint64 `json:"round"`
	Choice int    `json:"choice"`
}

// Validate checks a ballot's internal consistency.
func (b *Ballot) Validate() error {
	n := len(b.Options)
	if b.Round == 0 || n == 0 || n > MaxOptions || len(b.Counts) != n {
		return errors.New("vote: malformed ballot")
	}
	for _, o := range b.Options {
		if o.ID == "" || len(o.ID) > MaxNameBytes || len(o.Name) > MaxNameBytes || !utf8.ValidString(o.Name) || !ValidThumbRef(o.Thumb) {
			return errors.New("vote: malformed ballot option")
		}
	}
	for _, c := range b.Counts {
		if c < 0 {
			return errors.New("vote: malformed ballot count")
		}
	}
	if b.Mine < -1 || b.Mine >= n || b.Winner < -1 || b.Winner >= n || (b.Closed && b.Winner < 0) || b.RemainingMS < 0 {
		return errors.New("vote: malformed ballot state")
	}
	return nil
}

// EncodeBallot returns the datagram for b. If thumbnails make it too large,
// it is sent without them rather than not at all.
func EncodeBallot(b Ballot) ([]byte, error) {
	if err := b.Validate(); err != nil {
		return nil, err
	}
	d, err := encode(BallotPrefix, b)
	if err != nil && len(b.Options) > 0 {
		bare := b
		bare.Options = make([]Option, len(b.Options))
		for i, o := range b.Options {
			bare.Options[i] = Option{ID: o.ID, Name: o.Name}
		}
		d, err = encode(BallotPrefix, bare)
	}
	return d, err
}

// DecodeBallot parses a ballot datagram; ok is false if d is not one.
func DecodeBallot(d []byte) (b Ballot, ok bool) {
	if !decode(d, BallotPrefix, &b) || b.Validate() != nil {
		return Ballot{}, false
	}
	return b, true
}

// EncodeCast returns the datagram for c.
func EncodeCast(c Cast) ([]byte, error) {
	if c.Round == 0 || c.Choice < 0 || c.Choice >= MaxOptions {
		return nil, errors.New("vote: malformed cast")
	}
	return encode(CastPrefix, c)
}

// DecodeCast parses a vote datagram; ok is false if d is not one.
func DecodeCast(d []byte) (c Cast, ok bool) {
	if !decode(d, CastPrefix, &c) || c.Round == 0 || c.Choice < 0 || c.Choice >= MaxOptions {
		return Cast{}, false
	}
	return c, true
}

// Is reports whether d carries either vote prefix.
func Is(d []byte) bool {
	return bytes.HasPrefix(d, []byte(BallotPrefix)) || bytes.HasPrefix(d, []byte(CastPrefix))
}

func encode(prefix string, v any) ([]byte, error) {
	j, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	d := append([]byte(prefix), j...)
	if len(d) > MaxDatagram {
		return nil, errors.New("vote: message too large")
	}
	return d, nil
}

func decode(d []byte, prefix string, v any) bool {
	if len(d) > MaxDatagram || !bytes.HasPrefix(d, []byte(prefix)) {
		return false
	}
	dec := json.NewDecoder(bytes.NewReader(d[len(prefix):]))
	dec.DisallowUnknownFields()
	return dec.Decode(v) == nil && !dec.More()
}

// Name shortens a display name to MaxNameBytes on a rune boundary.
func Name(s string) string {
	if len(s) <= MaxNameBytes {
		return s
	}
	cut := MaxNameBytes
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut]
}
