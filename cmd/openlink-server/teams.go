package main

import (
	"context"
	"time"

	"halocommunity/internal/hostctl"
)

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
