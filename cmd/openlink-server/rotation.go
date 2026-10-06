package main

import (
	"context"
	"fmt"
	"time"

	"halocommunity/internal/hostctl"
	"halocommunity/internal/playlist"
)

// rotationInfo is the playlist state shown by the admin API.
type rotationInfo struct {
	Playlist   string `json:"playlist"`
	Current    string `json:"current,omitempty"` // entry selected for the coming or current match
	Pending    string `json:"pending,omitempty"` // entry waiting for the next lobby
	Matches    uint32 `json:"matches"`           // matches the server has started
	Selections int    `json:"selections"`
	Error      string `json:"error,omitempty"`
}

const (
	rotationPoll     = time.Second
	rotationAttempts = 3 // failed selections of one entry before skipping it
)

func (a *agent) setRotation(update func(*rotationInfo)) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.rotation == nil {
		a.rotation = &rotationInfo{Playlist: a.cfg.Playlist}
	}
	update(a.rotation)
}

func entrySelection(e playlist.Entry, initialize bool) controlSelection {
	return controlSelection{
		Map:        contentID{AssetID: e.Map.AssetID, VersionID: e.Map.VersionID},
		Mode:       contentID{AssetID: e.Mode.AssetID, VersionID: e.Mode.VersionID},
		Initialize: initialize,
	}
}

// runRotation drives map/mode selection for the connected server, from the
// playlist checked at agent start, until ctx ends or the transport closes.
func (a *agent) runRotation(ctx context.Context, controller hostController) {
	a.setRotation(func(*rotationInfo) {})
	f := a.playlist
	if f == nil {
		a.log.Error("no checked playlist; rotation off")
		a.setRotation(func(r *rotationInfo) { r.Error = "no checked playlist" })
		return
	}
	a.log.Info("playlist loaded", "entries", len(f.Entries), "selection", f.Selection)
	a.rotate(ctx, controller, playlist.NewBag(f, nil), rotationPoll)
}

// rotate selects the first match's entry as soon as the server's lobby is
// ready (the DLL then holds it against the lobby leader), and the next entry
// each time the server is back in its lobby after a match started. The first
// selection initializes the native provider.
func (a *agent) rotate(ctx context.Context, controller hostController, bag *playlist.Bag, poll time.Duration) {
	first := bag.Next()
	pending, initialize := &first, true
	var baseline uint32 // matches started when the current entry was selected
	failures := 0
	a.setRotation(func(r *rotationInfo) { r.Pending = first.ID })
	for ctx.Err() == nil && !controller.Closed() {
		request, cancel := context.WithTimeout(ctx, 4*time.Second)
		st, err := controller.Status(request)
		cancel()
		if err != nil {
			a.log.Warn("rotation status failed", "err", err)
			sleep(ctx, poll)
			continue
		}
		if st.Version < 2 {
			a.setRotation(func(r *rotationInfo) {
				r.Error = "openlink-control.dll does not report the match lifecycle; rotation needs the lock DLL"
			})
			a.log.Error("host control reply has no lifecycle state; rotation off")
			return
		}
		a.setRotation(func(r *rotationInfo) { r.Matches = st.Matches })
		if pending == nil && st.Matches > baseline {
			next := bag.Next()
			pending, failures = &next, 0
			a.setRotation(func(r *rotationInfo) { r.Pending = next.ID })
		}
		// The DLL sets the lobby gate only in HostSetup/HostPreGame.
		if pending != nil && st.Gates&hostctl.GateLobby != 0 {
			entry := *pending
			selection := entrySelection(entry, initialize)
			pair, err := parseSelection(selection)
			var reply hostctl.Reply
			if err == nil {
				request, cancel := context.WithTimeout(ctx, 4*time.Second)
				reply, err = dispatchSelection(request, controller, selection, pair)
				cancel()
			}
			if err == nil && reply.Code == hostctl.CodeSelected {
				a.log.Info("rotation selected", "entry", entry.ID, "match", st.Matches+1, "generation", reply.Generation)
				baseline, initialize, pending, failures = st.Matches, false, nil, 0
				a.setRotation(func(r *rotationInfo) {
					r.Current, r.Pending, r.Error = entry.ID, "", ""
					r.Selections++
				})
			} else {
				failures++
				if err == nil {
					err = fmt.Errorf("server replied %v", controlReply(reply)["status"])
				}
				a.log.Warn("rotation selection failed", "entry", entry.ID, "attempt", failures, "err", err)
				a.setRotation(func(r *rotationInfo) { r.Error = fmt.Sprintf("%s: %v", entry.ID, err) })
				if failures >= rotationAttempts {
					a.log.Warn("skipping playlist entry", "entry", entry.ID)
					next := bag.Next()
					pending, failures = &next, 0
					a.setRotation(func(r *rotationInfo) { r.Pending = next.ID })
				}
			}
		}
		sleep(ctx, poll)
	}
}
