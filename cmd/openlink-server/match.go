package main

import "halocommunity/internal/api"

// Server lifecycle states reported by the DLL (docs/HOST-CONTROL.md);
// statePreGame (8, the lobby) is in voting.go.
const (
	stateInGame   int32 = 9
	stateEndGame  int32 = 10
	stateStarting int32 = 11
)

// match is what the server is playing, for the directory listing: the phase
// from the DLL's lifecycle state (and the vote, when voting), and the
// playlist entry it concerns. Nil while the server or host control is not
// ready, so players simply see nothing.
func (a *agent) match() *api.Match {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.status != "ready" || a.lobby == nil || a.playlist == nil {
		return nil
	}
	m := &api.Match{}
	switch a.lobby.State {
	case statePreGame:
		m.Phase = api.PhaseLobby
	case stateStarting:
		m.Phase = api.PhaseStarting
	case stateInGame:
		m.Phase = api.PhaseInGame
	case stateEndGame:
		m.Phase = api.PhasePostGame
	default:
		return nil
	}
	var entry string
	switch {
	case a.cfg.Vote != nil && a.vote != nil:
		switch a.vote.Phase {
		case "voting":
			m.Phase = api.PhaseVoting // the next match is not chosen yet
		case "starting":
			m.Phase, entry = api.PhaseStarting, a.vote.Winner
		case "playing":
			entry = a.vote.Current
			if m.Phase == api.PhaseLobby {
				m.Phase = api.PhaseStarting // start requested; the match has not begun yet
			}
		default: // waiting: in the lobby nothing is chosen yet; otherwise the last match
			if m.Phase != api.PhaseLobby {
				entry = a.vote.Current
			}
		}
	case a.cfg.Vote == nil && a.rotation != nil:
		entry = a.rotation.Current // the coming or current match
	}
	for _, e := range a.playlist.Entries {
		if e.ID == entry {
			o := ballotOption(e) // the same ID, name and thumbnail players see on ballots
			m.Entry, m.Name, m.Thumb = o.ID, o.Name, o.Thumb
			break
		}
	}
	return m
}
