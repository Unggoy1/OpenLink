package main

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"halocommunity/vote"
)

// ballotFresh is how long a ballot stays on screen without a new copy from
// the host (it repeats them about once a second).
const ballotFresh = 4 * time.Second

// VoteOption is one map/mode choice.
type VoteOption struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Votes int    `json:"votes"`
	// Thumbs are the map thumbnail URLs to try in order (Halo Infinite's UGC
	// image host only; built here, never taken from the server). Empty = none.
	Thumbs []string `json:"thumbs"`
}

// BallotView is the host's current playlist vote, as shown in the app.
type BallotView struct {
	Round     uint64       `json:"round"`
	Options   []VoteOption `json:"options"`
	Mine      int          `json:"mine"`      // the player's choice, -1 if none
	Closed    bool         `json:"closed"`    // the vote is over; Winner is set
	Winner    int          `json:"winner"`    // -1 until closed
	Remaining float64      `json:"remaining"` // seconds until the vote closes
	StartsIn  float64      `json:"startsIn"`  // once closed: seconds until the match starts, -1 unknown
}

// Ballot returns the host's current vote, or nil when there is none.
func (a *App) Ballot() *BallotView {
	if demo != nil {
		return demo.ballot()
	}
	a.mu.Lock()
	sess := a.sess
	a.mu.Unlock()
	if sess == nil {
		return nil
	}
	b, at, ok := sess.Ballot()
	age := time.Since(at)
	if !ok || age > ballotFresh {
		return nil
	}
	v := &BallotView{Round: b.Round, Mine: b.Mine, Closed: b.Closed, Winner: b.Winner, StartsIn: -1,
		Remaining: max(0, (time.Duration(b.RemainingMS)*time.Millisecond - age).Seconds())}
	if b.Closed && b.StartMS >= 0 {
		v.StartsIn = max(0, (time.Duration(b.StartMS)*time.Millisecond - age).Seconds())
	}
	for i, o := range b.Options {
		v.Options = append(v.Options, VoteOption{ID: o.ID, Name: o.Name, Votes: b.Counts[i], Thumbs: vote.ThumbURLs(o.Thumb)})
	}
	return v
}

// Vote sends the player's choice (an index into Options) for a round.
func (a *App) Vote(round uint64, choice int) error {
	if demo != nil {
		return demo.vote(choice)
	}
	a.mu.Lock()
	sess := a.sess
	a.mu.Unlock()
	if sess == nil {
		return errors.New("not joined to a server")
	}
	if err := sess.Vote(round, choice); err != nil {
		a.events.Add("vote in round %d failed: %v", round, err)
		return err
	}
	a.events.Add("voted option %d in round %d", choice, round)
	return nil
}

// OverlaySupported reports whether this build has the in-game vote overlay.
func (a *App) OverlaySupported() bool { return a.overlay != nil }

// OverlayKeyConflicts returns the hotkeys (as written) that another app has
// already taken. Keys the overlay itself holds right now are not reported.
func (a *App) OverlayKeyConflicts(keys []string) []string { return a.overlay.conflicts(keys) }

// watchVotes alerts the player when a new vote opens: a Windows notification
// and a flashing taskbar button, since they are usually in the game. It also
// feeds the overlay, which decides for itself whether to show.
func (a *App) watchVotes(ctx context.Context) {
	notifications := runtime.InitializeNotifications(ctx) == nil
	t := time.NewTicker(500 * time.Millisecond)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		b := a.Ballot()
		a.overlay.update(b, a.GetSettings())
		if b == nil || b.Closed || b.Round == a.notified {
			continue
		}
		a.notified = b.Round
		flashWindow("OpenLink")
		if !a.GetSettings().MuteVoteSound {
			playChime()
		}
		if notifications {
			runtime.SendNotification(ctx, runtime.NotificationOptions{
				ID:    fmt.Sprintf("openlink-vote-%d", b.Round),
				Title: "Vote for the next match",
				Body:  fmt.Sprintf("Pick the next map and mode in OpenLink (%.0f s left).", b.Remaining),
			})
		}
	}
}
