// Package playlist reads the host agent's map/mode rotation file and picks
// entries from it. An entry is a whole map/mode pair identified by the
// asset and version IDs shown in the game's content browser, so a random
// pick never combines a map and mode that were not meant to go together.
package playlist

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"os"
	"strings"

	"halocommunity/vote"
)

// Content is one piece of published content: its asset ID and a pinned
// version ID.
type Content struct {
	AssetID   string `json:"asset_id"`
	VersionID string `json:"version_id"`
}

// Entry is one playable map/mode pair: a map and a published game variant,
// as in a custom game.
type Entry struct {
	ID      string  `json:"id"`
	Name    string  `json:"name,omitempty"`
	Map     Content `json:"map"`
	Mode    Content `json:"mode"`
	Enabled *bool   `json:"enabled,omitempty"` // default true
}

// ThumbRef is the entry's map thumbnail reference for vote ballots
// (vote.ThumbURLs builds the URLs from the map's asset and version IDs).
func (e Entry) ThumbRef() string { return vote.MapThumbRef(e.Map.AssetID, e.Map.VersionID) }

func (e Entry) enabled() bool { return e.Enabled == nil || *e.Enabled }

// File is the playlist document.
type File struct {
	SchemaVersion int     `json:"schema_version"`
	Selection     string  `json:"selection"` // "shuffle_bag" (default) or "sequential"
	Entries       []Entry `json:"entries"`
}

// Load reads and validates a playlist file. Only enabled entries are kept.
func Load(path string) (*File, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return Parse(b)
}

// Parse validates a playlist document. Only enabled entries are kept.
func Parse(b []byte) (*File, error) {
	var f File
	if err := json.Unmarshal(b, &f); err != nil {
		return nil, fmt.Errorf("playlist: %w", err)
	}
	if f.SchemaVersion != 1 {
		return nil, fmt.Errorf("playlist: schema_version %d is not supported (want 1)", f.SchemaVersion)
	}
	if f.Selection == "" {
		f.Selection = "shuffle_bag"
	}
	if f.Selection != "shuffle_bag" && f.Selection != "sequential" {
		return nil, fmt.Errorf("playlist: selection %q must be shuffle_bag or sequential", f.Selection)
	}
	seen := map[string]bool{}
	var kept []Entry
	for i, e := range f.Entries {
		if e.ID == "" {
			return nil, fmt.Errorf("playlist: entry %d has no id", i)
		}
		if seen[e.ID] {
			return nil, fmt.Errorf("playlist: duplicate entry id %q", e.ID)
		}
		seen[e.ID] = true
		// IDs and names go on vote ballots, which cap them at vote.MaxNameBytes.
		for _, f := range []struct{ field, value string }{{"id", e.ID}, {"name", e.Name}} {
			if n := len(f.value); n > vote.MaxNameBytes {
				return nil, fmt.Errorf("playlist: entry %q %s is %d bytes; the limit is %d UTF-8 bytes (not characters)", e.ID, f.field, n, vote.MaxNameBytes)
			}
			if strings.IndexFunc(f.value, func(r rune) bool { return r < 0x20 || r == 0x7f }) >= 0 {
				return nil, fmt.Errorf("playlist: entry %q %s contains a control character", e.ID, f.field)
			}
		}
		for _, id := range []struct{ name, value string }{
			{"map.asset_id", e.Map.AssetID}, {"map.version_id", e.Map.VersionID},
			{"mode.asset_id", e.Mode.AssetID}, {"mode.version_id", e.Mode.VersionID},
		} {
			if !canonicalUUID(id.value) {
				return nil, fmt.Errorf("playlist: entry %q %s %q is not a canonical nonzero UUID", e.ID, id.name, id.value)
			}
		}
		if e.enabled() {
			kept = append(kept, e)
		}
	}
	if len(kept) == 0 {
		return nil, errors.New("playlist: no enabled entries")
	}
	f.Entries = kept
	return &f, nil
}

func canonicalUUID(s string) bool {
	if len(s) != 36 || s[8] != '-' || s[13] != '-' || s[18] != '-' || s[23] != '-' {
		return false
	}
	b, err := hex.DecodeString(strings.ReplaceAll(s, "-", ""))
	if err != nil || len(b) != 16 {
		return false
	}
	for _, v := range b {
		if v != 0 {
			return true
		}
	}
	return false
}

// Bag hands out entries. With shuffle_bag every entry is played once per
// cycle in random order, and a new cycle never starts with the entry that
// ended the previous one (when there are at least two entries). With
// sequential the file order repeats.
type Bag struct {
	entries    []Entry
	sequential bool
	rng        *rand.Rand
	order      []int
	pos        int
	last       int
}

// NewBag starts a rotation. rng may be nil for a randomly seeded source.
func NewBag(f *File, rng *rand.Rand) *Bag {
	if rng == nil {
		rng = rand.New(rand.NewPCG(rand.Uint64(), rand.Uint64()))
	}
	return &Bag{entries: f.Entries, sequential: f.Selection == "sequential", rng: rng, last: -1}
}

// Next returns the next entry to play.
func (b *Bag) Next() Entry {
	if b.pos >= len(b.order) {
		b.refill()
	}
	i := b.order[b.pos]
	b.pos++
	b.last = i
	return b.entries[i]
}

func (b *Bag) refill() {
	n := len(b.entries)
	b.order, b.pos = make([]int, n), 0
	for i := range b.order {
		b.order[i] = i
	}
	if b.sequential {
		return
	}
	b.rng.Shuffle(n, func(i, j int) { b.order[i], b.order[j] = b.order[j], b.order[i] })
	if n > 1 && b.order[0] == b.last {
		k := 1 + b.rng.IntN(n-1)
		b.order[0], b.order[k] = b.order[k], b.order[0]
	}
}
