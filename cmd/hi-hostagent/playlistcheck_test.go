package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"halocommunity/vote"
)

// playlistDoc builds a playlist of n custom entries with the given IDs' and
// names' lengths (name "" = no name field).
func playlistDoc(n int, id func(i int) string, name func(i int) string) string {
	var es []string
	for i := range n {
		nameField := ""
		if s := name(i); s != "" {
			nameField = fmt.Sprintf(`"name": %q, `, s)
		}
		es = append(es, fmt.Sprintf(`{"id": %q, %s"map": {"asset_id": "aaaaaaaa-0000-0000-0000-%012d", "version_id": "bbbbbbbb-0000-0000-0000-%012d"},
 "mode": {"asset_id": "cccccccc-0000-0000-0000-%012d", "version_id": "dddddddd-0000-0000-0000-%012d"}}`, id(i), nameField, i+1, i+1, i+1, i+1))
	}
	return `{"schema_version": 1, "entries": [` + strings.Join(es, ",") + `]}`
}

func writeTemp(t *testing.T, doc string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "playlist.json")
	if err := os.WriteFile(p, []byte(doc), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestCheckPlaylistRequired(t *testing.T) {
	if _, _, err := checkPlaylist(defaults(), "", false); !errors.Is(err, errNoPlaylist) {
		t.Fatalf("no playlist: %v", err)
	}
}

func TestCheckPlaylistVotingRules(t *testing.T) {
	c := defaults()
	c.Vote = &voteConfig{}
	short := func(i int) string { return fmt.Sprintf("e%d", i) }
	none := func(int) string { return "" }

	if _, _, err := checkPlaylist(c, writeTemp(t, playlistDoc(1, short, none)), true); err == nil {
		t.Error("voting with one entry accepted")
	}
	// The rotation-only check accepts an engine-only playlist; voting does not.
	if _, _, err := checkPlaylist(c, writeTemp(t, rotationDocEngineOnly), false); err != nil {
		t.Errorf("rotation: %v", err)
	}
	if _, _, err := checkPlaylist(c, writeTemp(t, rotationDocEngineOnly), true); err == nil {
		t.Error("voting without a custom mode accepted")
	}
}

const rotationDocEngineOnly = `{"schema_version": 1, "entries": [
 {"id": "a", "mode_kind": "engine", "map": {"asset_id": "aaaaaaaa-0000-0000-0000-000000000001", "version_id": "aaaaaaaa-0000-0000-0000-000000000002"},
  "mode": {"asset_id": "aaaaaaaa-0000-0000-0000-000000000003", "version_id": "aaaaaaaa-0000-0000-0000-000000000004"}},
 {"id": "b", "mode_kind": "engine", "map": {"asset_id": "bbbbbbbb-0000-0000-0000-000000000001", "version_id": "bbbbbbbb-0000-0000-0000-000000000002"},
  "mode": {"asset_id": "bbbbbbbb-0000-0000-0000-000000000003", "version_id": "bbbbbbbb-0000-0000-0000-000000000004"}}]}`

func TestCheckPlaylistBallotSize(t *testing.T) {
	c := defaults()
	c.Vote = &voteConfig{}
	longID := func(i int) string { return fmt.Sprintf("%079d", i) + "x" } // 80 bytes
	longName := func(i int) string { return strings.Repeat("n", 80) }

	// The largest IDs and names the loader allows still fit, with thumbnails.
	_, rep, err := checkPlaylist(c, writeTemp(t, playlistDoc(6, longID, longName)), true)
	if err != nil || rep.ballot > vote.MaxDatagram || rep.ballot < 1100 {
		t.Fatalf("80/80 playlist: %v (ballot %d)", err, rep.ballot)
	}

	// Escaped characters (& is 6 bytes on the wire) push a ballot over.
	amp := func(i int) string { return strings.Repeat("&", 40) }
	if _, rep, err := checkPlaylist(c, writeTemp(t, playlistDoc(5, longID, amp)), true); err == nil {
		t.Fatalf("oversized ballot accepted (%d bytes)", rep.ballot)
	}
	// With fewer options per vote the same playlist fits.
	c.Vote = &voteConfig{Options: 1}
	if _, _, err := checkPlaylist(c, writeTemp(t, playlistDoc(5, longID, amp)), true); err != nil {
		t.Fatalf("one-option ballots: %v", err)
	}
}

func TestWorstBallotCoversRealBallots(t *testing.T) {
	short := func(i int) string { return fmt.Sprintf("e%d", i) }
	mixed := func(i int) string { return strings.Repeat("m", 10*i) }
	f, _, err := checkPlaylist(defaults(), writeTemp(t, playlistDoc(6, short, mixed)), false)
	if err != nil {
		t.Fatal(err)
	}
	vc := voteConfig{}
	worst := vote.BallotSize(worstBallot(f.Entries, vc, 32), true)
	// Any real ballot (any 4 entries, closed or open, small numbers) is no larger.
	for start := range 3 {
		opts := f.Entries[start : start+4]
		b := vote.Ballot{Round: 12, RemainingMS: 1234, Counts: []int{1, 2, 3, 4}, Mine: 2, Winner: -1, StartMS: -1}
		for _, e := range opts {
			b.Options = append(b.Options, ballotOption(e))
		}
		if got := vote.BallotSize(b, true); got > worst {
			t.Fatalf("real ballot %d bytes > worst case %d", got, worst)
		}
	}
}
