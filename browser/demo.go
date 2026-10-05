//go:build demo

package main

import (
	"sync"
	"time"

	"halocommunity/vote"
)

// Preview build: `wails dev -tags demo` shows a joined server and a vote that
// repeats every 45 s (30 s open, then the result), with real map thumbnails.
var demo = &demoState{start: time.Now(), mine: -1}

type demoState struct {
	mu    sync.Mutex
	start time.Time
	mine  int
	round uint64
}

func (*demoState) status() *StatusView {
	return &StatusView{ServerID: "demo", ServerName: "Vote Test (demo)", Mode: "loopback", BeaconAge: 1, Connected: true, UpKB: 812, DownKB: 2048}
}

func (d *demoState) ballot() *BallotView {
	d.mu.Lock()
	defer d.mu.Unlock()
	elapsed := time.Since(d.start) % (45 * time.Second)
	round := uint64(time.Since(d.start)/(45*time.Second)) + 1
	if round != d.round {
		d.round, d.mine = round, -1
	}
	opts := []struct{ id, name, asset, version string }{
		{"interference-fiesta", "Fiesta Slayer on Interference", "70f884d7-6869-469d-b4d2-4219627e2d83", "cc791b4b-054a-4653-9034-5dc13c809c54"},
		{"kusini-ctf", "CTF: Arena on Kusini Bay", "4eb7a3ac-81f7-4faa-acd8-ce6bbba667af", "98a5391c-4a3a-4f04-bdc7-6db58cc27433"},
		{"kusini-fiesta", "Fiesta Slayer on Kusini Bay", "4eb7a3ac-81f7-4faa-acd8-ce6bbba667af", "98a5391c-4a3a-4f04-bdc7-6db58cc27433"},
		{"no-thumbnail", "A map without a thumbnail", "00000000-0000-0000-0000-000000000001", "00000000-0000-0000-0000-000000000001"},
	}
	v := &BallotView{Round: round, Mine: d.mine, Winner: -1, StartsIn: -1}
	for i, o := range opts {
		votes := []int{2, 1, 0, 0}[i]
		if d.mine == i {
			votes++
		}
		v.Options = append(v.Options, VoteOption{ID: o.id, Name: o.name, Votes: votes, Thumbs: vote.ThumbURLs(vote.MapThumbRef(o.asset, o.version))})
	}
	if elapsed < 30*time.Second {
		v.Remaining = (30*time.Second - elapsed).Seconds()
	} else {
		v.Closed, v.Winner, v.StartsIn = true, 0, (35*time.Second - elapsed).Seconds()
		if v.StartsIn < 0 {
			v.StartsIn = 0
		}
	}
	return v
}

func (d *demoState) vote(choice int) error {
	d.mu.Lock()
	d.mine = choice
	d.mu.Unlock()
	return nil
}
