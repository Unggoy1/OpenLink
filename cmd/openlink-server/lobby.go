package main

import (
	"context"
	"fmt"
	"time"

	"halocommunity/internal/hostctl"
)

// autoStart starts each match from the server once enough players are in the
// lobby, replacing the lobby leader's Play.
type autoStart struct {
	MinPlayers   int `json:"min_players"`   // connected peers needed (default 1)
	DelaySeconds int `json:"delay_seconds"` // time they must stay before the start (default 10)
}

func (s autoStart) minPlayers() int32 {
	if s.MinPlayers < 1 {
		return 1
	}
	return int32(s.MinPlayers)
}
func (s autoStart) delay() time.Duration {
	if s.DelaySeconds < 0 {
		return 0
	}
	if s.DelaySeconds == 0 {
		return 10 * time.Second
	}
	return time.Duration(s.DelaySeconds) * time.Second
}

type lobbyController interface {
	Closed() bool
	Status(context.Context) (hostctl.Reply, error)
	Start(context.Context) (hostctl.Reply, error)
	ServerOwned(context.Context, uint32) (hostctl.Reply, error)
	SetLeader(context.Context, uint64) (hostctl.Reply, error)
	EndMatch(context.Context) (hostctl.Reply, error)
}

// lobbyInfo is the lobby state shown by the admin API.
type lobbyInfo struct {
	ServerOwned bool          `json:"server_owned"`
	AutoStart   bool          `json:"auto_start"`
	Starts      int           `json:"starts"`
	Lobby       hostctl.Lobby `json:"lobby"`
	State       int32         `json:"state"` // server lifecycle state from the DLL (match.go); 0 = not read yet
	Error       string        `json:"error,omitempty"`
}

func (a *agent) setLobby(update func(*lobbyInfo)) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.lobby == nil {
		a.lobby = &lobbyInfo{}
	}
	update(a.lobby)
}

const lobbyPoll = time.Second

// guardView is what the DLL reports about player requests it overrode.
type guardView struct {
	RestartGuard, EndMatch                bool
	NameFixes, BlockedRestart, RejoinTeam uint16
}

// logGuards logs the DLL's guards once, then each time a player's request was
// overridden: a server rename undone, a Restart Match dropped, or a rejoining
// player put back on their team.
func (a *agent) logGuards(l hostctl.Lobby, last **guardView) {
	v := guardView{l.RestartGuard, l.EndMatch, l.NameFixes, l.BlockedRestart, l.RejoinTeams}
	if *last == nil {
		if !v.RestartGuard && l.Flags&hostctl.LobbyServerOwned != 0 {
			a.log.Warn("the Restart Match guard is not installed: a modified client could restart a match")
		}
		if !v.EndMatch {
			a.log.Warn("End Match is unavailable in this game build: the end-match command and the watchdog's match limit do nothing")
		}
	} else {
		p := **last
		if !p.RestartGuard && v.RestartGuard {
			a.log.Info("Restart Match guard installed")
		}
		if v.NameFixes > p.NameFixes {
			a.log.Warn("a player renamed the server; the name was put back", "times", v.NameFixes)
		}
		if v.BlockedRestart > p.BlockedRestart {
			a.log.Warn("dropped a player's Restart Match request (only a modified client sends one)", "times", v.BlockedRestart)
		}
		if v.RejoinTeam > p.RejoinTeam {
			a.log.Info("a rejoining player was put back on their team", "times", v.RejoinTeam)
		}
	}
	*last = &v
}

// holdLeader makes the server keep the LAN lobby leader role with a placeholder
// XUID, so no player is leader. The DLL keeps re-asserting it on every engine
// tick, so a lobby that does not exist yet (Busy) still gets it later.
func (a *agent) holdLeader(ctx context.Context, controller lobbyController, xuid uint64, retry time.Duration) {
	for attempt := 0; attempt < 10 && ctx.Err() == nil && !controller.Closed(); attempt++ {
		request, cancel := context.WithTimeout(ctx, 4*time.Second)
		reply, err := controller.SetLeader(request, xuid)
		cancel()
		switch {
		case err != nil:
			a.log.Error("lobby leader not held; the first player to join becomes leader", "err", err)
			a.setLobby(func(l *lobbyInfo) { l.Error = "lobby leader failed" })
			return
		case reply.Code == hostctl.CodeOK:
			a.log.Info("server holds the lobby leader role: no player gets lobby options, map/mode menus, Play or End Game", "leader_xuid", xuid)
			return
		case reply.Code == hostctl.CodeBusy:
			a.log.Info("server will hold the lobby leader role once the lobby exists", "leader_xuid", xuid)
			return
		case reply.Code != hostctl.CodeNativePending:
			a.log.Error("lobby leader not held; the first player to join becomes leader", "code", reply.Code)
			a.setLobby(func(l *lobbyInfo) { l.Error = "lobby leader failed" })
			return
		}
		sleep(ctx, retry)
	}
}

// runLobby makes the server own its lobby (no player becomes leader), logs
// lobby changes, and with auto_start starts each match once enough players
// stay connected for the delay.
func (a *agent) runLobby(ctx context.Context, controller lobbyController, poll time.Duration) {
	a.setLobby(func(l *lobbyInfo) { l.AutoStart = a.cfg.AutoStart != nil })
	request, cancel := context.WithTimeout(ctx, 4*time.Second)
	reply, err := controller.ServerOwned(request, hostctl.ServerOwnedNoOwner)
	cancel()
	if err != nil || reply.Code != hostctl.CodeOK {
		a.log.Error("server-owned lobby unavailable; players keep lobby control", "err", err, "code", reply.Code)
		a.setLobby(func(l *lobbyInfo) { l.Error = "server-owned lobby failed" })
	} else {
		a.log.Info("server owns the lobby: player start, end-game and Restart Match requests are dropped")
		a.setLobby(func(l *lobbyInfo) { l.ServerOwned = true })
		a.holdLeader(ctx, controller, a.cfg.lobbyLeader(), poll)
	}
	var dog *watchdog // only when a stuck server gets restarted
	if a.cfg.Restart {
		dog = newWatchdog(a.cfg.Watchdog)
	}
	var last hostctl.Lobby
	var lastBots botView
	var lastGuards *guardView
	var lastState int32 = -2
	var since time.Time // when the lobby first had enough players
	for ctx.Err() == nil && !controller.Closed() {
		request, cancel := context.WithTimeout(ctx, 4*time.Second)
		st, err := controller.Status(request)
		cancel()
		if err != nil {
			sleep(ctx, poll)
			continue
		}
		if st.Version < 3 {
			a.log.Error("openlink-control.dll does not report the lobby; lobby control off")
			a.setLobby(func(l *lobbyInfo) { l.Error = "DLL has no lobby report (needs v3)" })
			return
		}
		l := st.Lobby
		a.setLobby(func(info *lobbyInfo) { info.Lobby, info.State = l, st.State })
		if act, reason := dog.observe(time.Now(), st.State, l.Flags&hostctl.LobbyStartMode != 0 && l.StartMode == 1,
			l.Ticks, st.Version >= 7); act != watchNone {
			a.watchdogAct(ctx, controller, act, reason)
		}
		if st.Version >= 5 {
			a.warnFFATeams(l.LobbyVariantTeams)
		}
		if st.Version >= 6 {
			a.logBots(l, &lastBots)
		}
		if st.Version >= 7 {
			a.logGuards(l, &lastGuards)
		}
		l.BotTicks, l.Ticks = 0, 0 // count every tick; not a lobby change
		if l != last || st.State != lastState {
			a.log.Info("lobby", "state", st.State, "connected", l.Connected, "peers", l.Peers, "mask", l.PeerMask, "player_mask", l.PlayerMask,
				"owner", l.Owner, "host_peer", l.HostPeer, "players", l.Players, "start_mode", l.StartMode,
				"allowed", l.Allowed, "prepared", l.Prepared, "prep", [2]int{int(l.PrepStarted), int(l.PrepDone)},
				"loading", l.Loading, "start", l.Start, "users_required", l.UsersRequired,
				"game_type", l.GameType, "session_kind", l.SessionKind, "flags", l.Flags,
				"blocked_start", l.BlockedStart, "blocked_end", l.BlockedEnd, "end_game", l.EndGame, "end_game_table", fmt.Sprintf("%#x", l.EndGameTable),
				"leader", l.Leader, "leader_held", l.Flags&hostctl.LobbyLeaderHeld != 0, "leader_sets", l.LeaderSets,
				"lobby_variant_teams", l.LobbyVariantTeams, "game_variant_teams", l.GameVariantTeams, "game_state", l.GameState,
				"last_teams_enabled", l.LastTeamsEnabled, "last_team_count", l.LastTeamCount, "forced_team_count", l.ForcedTeamCount, "team_fixes", l.TeamFixes,
				"peer_teams", l.TeamSummary())
			last, lastState = l, st.State
		}
		if a.cfg.AutoStart == nil {
			sleep(ctx, poll)
			continue
		}
		ready := st.State == int32(8) && l.Flags&hostctl.LobbyStartMode != 0 && l.StartMode == 0 &&
			l.Connected >= a.cfg.AutoStart.minPlayers()
		if !ready {
			since = time.Time{}
			sleep(ctx, poll)
			continue
		}
		if since.IsZero() {
			since = time.Now()
			a.log.Info("auto start: players ready", "connected", l.Connected, "starting_in", a.cfg.AutoStart.delay())
		}
		if time.Since(since) >= a.cfg.AutoStart.delay() {
			request, cancel := context.WithTimeout(ctx, 4*time.Second)
			reply, err := controller.Start(request)
			cancel()
			if err == nil && reply.Code == hostctl.CodeOK {
				a.log.Info("auto start: match start requested", "connected", reply.Lobby.Connected, "start_mode", reply.Lobby.StartMode)
				a.setLobby(func(info *lobbyInfo) { info.Starts++; info.Error = "" })
			} else {
				a.log.Warn("auto start failed", "err", err, "code", reply.Code)
				a.setLobby(func(info *lobbyInfo) { info.Error = "start failed" })
			}
			since = time.Time{}
		}
		sleep(ctx, poll)
	}
}
