package main

import (
	"context"
	"time"

	"halocommunity/internal/hostctl"
	"halocommunity/internal/playlist"
)

// entryTeams returns the team rules for entry and remembers it as the entry of
// the coming match, for warnFFATeams.
func (a *agent) entryTeams(entry *playlist.Entry) hostctl.TeamPolicy {
	a.mu.Lock()
	a.teamEntry = entry
	a.mu.Unlock()
	return a.cfg.teamPolicy(entry)
}

// warnFFATeams logs once per entry when the match being prepared is a
// free-for-all mode (lobbyVariantTeams 0) but its playlist entry sets "teams":
// the server ignores them, and the host should remove them.
func (a *agent) warnFFATeams(lobbyVariantTeams int8) {
	if lobbyVariantTeams != 0 {
		return
	}
	a.mu.Lock()
	e := a.teamEntry
	warn := e != nil && e.Teams != nil && !a.teamWarned[e.ID]
	if warn {
		if a.teamWarned == nil {
			a.teamWarned = map[string]bool{}
		}
		a.teamWarned[e.ID] = true
	}
	a.mu.Unlock()
	if warn {
		a.log.Warn("playlist entry sets teams, but its mode is free-for-all; the teams are ignored (remove them from the playlist)", "entry", e.ID)
	}
}

// teamController is a server bridge that takes team rules (hostctl.Bridge).
type teamController interface {
	TeamPolicy(context.Context, hostctl.TeamPolicy) (hostctl.Reply, error)
}

// sendTeams gives the server the team rules for the match of the entry just
// selected, before that match is prepared (the DLL applies them once per match,
// before players spawn). Controllers without team rules are skipped.
func sendTeams(ctx context.Context, controller any, p hostctl.TeamPolicy, log interface {
	Info(string, ...any)
	Warn(string, ...any)
}, entry string) {
	tc, ok := controller.(teamController)
	if !ok {
		return
	}
	request, cancel := context.WithTimeout(ctx, 4*time.Second)
	reply, err := tc.TeamPolicy(request, p)
	cancel()
	if err != nil || reply.Code != hostctl.CodeOK {
		log.Warn("team rules not set; the game's own team handling applies", "entry", entry, "err", err, "code", reply.Code)
		return
	}
	balance := "off"
	if p.Flags&hostctl.TeamBalance != 0 {
		balance = map[uint32]string{hostctl.TeamModeEven: "even", hostctl.TeamModeShuffle: "shuffle"}[p.Mode]
	}
	log.Info("team rules", "entry", entry, "balance", balance, "teams", p.Count, "team_size", p.Size,
		"ffa_own_teams", p.Flags&hostctl.TeamGuardFFA != 0)
}
