package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"slices"

	"halocommunity/internal/playlist"
	"halocommunity/vote"
)

// errNoPlaylist: every dedicated server runs from a playlist. Hosts who want
// to pick maps by hand can host an ordinary custom game without this tool.
var errNoPlaylist = errors.New(`a playlist is required: set "playlist" in openlink-server.json ` +
	`(see openlink-server.example.json). To host without a playlist, host an ordinary custom game instead`)

// checkControlFiles makes sure the control DLL and its loader are in place.
func checkControlFiles(dll string) error {
	loader := filepath.Join(filepath.Dir(dll), "openlink-loader.exe")
	for _, p := range []string{dll, loader} {
		if !fileExists(p) {
			return fmt.Errorf("%s not found: keep all files from the OpenLink Server zip in one folder", p)
		}
	}
	return nil
}

type playlistReport struct {
	ballot int // largest possible ballot in bytes, with thumbnails; 0 when not voting
}

// checkPlaylist loads the playlist at path and checks everything the agent
// will need from it, so a server never starts with a playlist that rotation
// or voting would refuse later. playlist.Parse checks the file itself (IDs,
// UUIDs, 80-byte IDs and names); with voting it also checks that every
// possible ballot fits in one datagram with its thumbnails.
func checkPlaylist(c config, path string, voting bool) (*playlist.File, playlistReport, error) {
	var rep playlistReport
	if path == "" {
		return nil, rep, errNoPlaylist
	}
	f, err := playlist.Load(path)
	if err != nil {
		return nil, rep, err
	}
	if !voting {
		return f, rep, nil
	}
	if !hasCustom(f.Entries) {
		return nil, rep, errors.New("playlist: voting needs at least one custom (UGC) mode entry for the first selection")
	}
	if len(f.Entries) < 2 {
		return nil, rep, errors.New("playlist: voting needs at least 2 enabled entries")
	}
	vc := voteConfig{}
	if c.Vote != nil {
		vc = *c.Vote
	}
	b := worstBallot(f.Entries, vc, c.MaxPlayers)
	rep.ballot = vote.BallotSize(b, true)
	if rep.ballot > vote.MaxDatagram {
		names := make([]string, len(b.Options))
		for i, o := range b.Options {
			names[i] = o.ID
		}
		return nil, rep, fmt.Errorf("playlist: a ballot of %v could be %d bytes; the limit is %d. "+
			"Shorten those IDs and names (& < > count as 6 bytes each)", names, rep.ballot, vote.MaxDatagram)
	}
	return f, rep, nil
}

// worstBallot is the largest ballot the voter could send: the entries with
// the longest encoded options, and every number at its longest.
func worstBallot(entries []playlist.Entry, vc voteConfig, maxPlayers int) vote.Ballot {
	opts := make([]vote.Option, len(entries))
	for i, e := range entries {
		opts[i] = ballotOption(e)
	}
	size := func(o vote.Option) int {
		j, _ := json.Marshal(o)
		return len(j)
	}
	slices.SortStableFunc(opts, func(x, y vote.Option) int { return size(y) - size(x) })
	opts = opts[:min(vc.options(), len(opts))]
	count := maxPlayers
	if count <= 0 || count > 999 {
		count = 999
	}
	counts := make([]int, len(opts))
	for i := range counts {
		counts[i] = count
	}
	return vote.Ballot{
		Round: 9_999_999, Options: opts, Counts: counts, Mine: -1, Winner: -1,
		RemainingMS: vc.window().Milliseconds(), StartMS: vc.startDelay().Milliseconds(),
	}
}

// checkPlaylistCommand: openlink-server check-playlist [file]. Always applies the
// voting checks (with the config's vote settings, or the defaults), so a
// playlist that passes works on any server.
func checkPlaylistCommand(c config, args []string) error {
	path := c.resolve(c.Playlist)
	switch len(args) {
	case 1:
	case 2:
		path = args[1]
	default:
		return errors.New("usage: check-playlist [playlist.json]")
	}
	f, rep, err := checkPlaylist(c, path, true)
	if err != nil {
		return err
	}
	fmt.Printf("%s: OK. %d enabled entries, selection %s. Largest possible ballot: %d of %d bytes.\n",
		path, len(f.Entries), f.Selection, rep.ballot, vote.MaxDatagram)
	return nil
}
