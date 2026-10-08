package main

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"halocommunity/internal/hostctl"
	"halocommunity/internal/playlist"
)

// botBackfill is the server's bot backfill setting ("bot_backfill"): matches
// are topped up with bots to fill_to players, and a bot leaves each time a
// player joins. Bots are added only during a match (the game allows no bots in
// the lobby) and only in modes that have bots enabled; modes that spawn or
// backfill bots themselves are left alone. A playlist entry's "bots" can turn
// it off or on, or override fill_to and difficulty.
type botBackfill struct {
	FillTo     int    `json:"fill_to,omitempty"`    // players plus bots per match (default 8)
	MaxBots    int    `json:"max_bots,omitempty"`   // at most this many bots (default and limit 8)
	Difficulty string `json:"difficulty,omitempty"` // recruit, marine (default), odst or spartan
}

const defaultBotFill = 8

// entryBotsDefault is whether entries without "bots" get backfill when the
// server has it on.
const entryBotsDefault = false

func (b botBackfill) fillTo() int {
	if b.FillTo == 0 {
		return defaultBotFill
	}
	return b.FillTo
}

func (b botBackfill) maxBots() int {
	if b.MaxBots == 0 {
		return hostctl.MaxBots
	}
	return b.MaxBots
}

func (b botBackfill) validate() error {
	switch {
	case b.FillTo != 0 && (b.FillTo < playlist.MinBotFill || b.FillTo > playlist.MaxBotFill):
		return fmt.Errorf("bot_backfill.fill_to must be %d-%d", playlist.MinBotFill, playlist.MaxBotFill)
	case b.MaxBots < 0 || b.MaxBots > hostctl.MaxBots:
		return fmt.Errorf("bot_backfill.max_bots must be 1-%d", hostctl.MaxBots)
	case !playlist.ValidBotDifficulty(b.Difficulty):
		return fmt.Errorf("bot_backfill.difficulty must be one of %v", playlist.BotDifficulties)
	}
	return nil
}

// botDifficulty maps a difficulty name to hostctl.Bot*; "" is marine.
func botDifficulty(name string) uint32 {
	if i := slices.Index(playlist.BotDifficulties, name); i >= 0 {
		return uint32(i)
	}
	return hostctl.BotMarine
}

// botPolicy is the bot backfill for a match of entry (nil: no playlist entry).
func (c config) botPolicy(entry *playlist.Entry) hostctl.BotPolicy {
	b := c.BotBackfill
	if b == nil {
		return hostctl.BotPolicy{}
	}
	on, fill, difficulty := entryBotsDefault, b.fillTo(), b.Difficulty
	if entry != nil && entry.Bots != nil {
		on = entry.Bots.Enabled
		if entry.Bots.FillTo != 0 {
			fill = entry.Bots.FillTo
		}
		if entry.Bots.Difficulty != "" {
			difficulty = entry.Bots.Difficulty
		}
	}
	if !on {
		return hostctl.BotPolicy{}
	}
	return hostctl.BotPolicy{Flags: hostctl.BotBackfill, FillTo: uint32(fill), MaxBots: uint32(b.maxBots()), Difficulty: botDifficulty(difficulty)}
}

// botController is a server bridge that takes bot backfill (hostctl.Bridge).
type botController interface {
	BotPolicy(context.Context, hostctl.BotPolicy) (hostctl.Reply, error)
}

// sendBots gives the server the bot backfill for the match of the entry just
// selected. Controllers without bot backfill are skipped.
func sendBots(ctx context.Context, controller any, p hostctl.BotPolicy, log interface {
	Info(string, ...any)
	Warn(string, ...any)
}, entry string) {
	bc, ok := controller.(botController)
	if !ok {
		return
	}
	request, cancel := context.WithTimeout(ctx, 4*time.Second)
	reply, err := bc.BotPolicy(request, p)
	cancel()
	if err != nil || reply.Code != hostctl.CodeOK {
		log.Warn("bot backfill not set for this match", "entry", entry, "err", err, "code", reply.Code)
		return
	}
	if p.Flags&hostctl.BotBackfill == 0 {
		log.Info("bot backfill off for this match", "entry", entry)
		return
	}
	log.Info("bot backfill", "entry", entry, "fill_to", p.FillTo, "max_bots", p.MaxBots, "difficulty", playlist.BotDifficulties[p.Difficulty])
}

// botStateNames are hostctl.BotState* for logs.
var botStateNames = map[uint8]string{
	hostctl.BotStateNone: "none", hostctl.BotStateOff: "off", hostctl.BotStateModeBots: "mode_bots",
	hostctl.BotStateWaiting: "waiting", hostctl.BotStateFilled: "filled", hostctl.BotStateChanged: "changed",
	hostctl.BotStateRefused: "refused", hostctl.BotStateNoNavmesh: "no_navmesh",
}

// botView is the part of the DLL's lobby report the bots log line shows.
type botView struct {
	Supported                 bool
	Enabled                   int8
	ModeBots, State           uint8
	Bots, Players, ThreadRole int8
	Adds, Removes, Refused    uint16
	Nav                       int8
	NavFromVariant            bool
	Difficulties              uint16
}

// logBots logs bot backfill when it changes: whether the mode has bots enabled
// and spawns its own, bots and players in the match, what backfill did last
// (BotState*), and counts of bots added, removed and not created.
func (a *agent) logBots(l hostctl.Lobby, last *botView) {
	v := botView{l.BotSupported, l.BotsEnabled, l.ModeBots, l.BotState, l.BotCount, l.BotHumans, l.BotThreadRole,
		l.BotAdds, l.BotRemoves, l.BotRefused, l.NavState, l.NavFromVariant, l.BotDifficulties}
	if v == *last {
		return
	}
	*last = v
	a.log.Info("bots", "supported", v.Supported, "mode_bots_enabled", v.Enabled, "mode_spawns_bots", v.ModeBots,
		"state", botStateNames[v.State], "bots", v.Bots, "players", v.Players, "thread_role", v.ThreadRole,
		"added", v.Adds, "removed", v.Removes, "refused", v.Refused,
		"navmesh", map[int8]string{-1: "unknown", 0: "none", 1: "yes"}[v.Nav], "nav_faces", l.NavFaces, "nav_from_forge_map", v.NavFromVariant,
		"mode_difficulties", registeredDifficulties(v.Difficulties), "ticks", l.BotTicks)
}

// registeredDifficulties names the bot difficulties a mode registered
// (Lobby.BotDifficulties bits by engine code), "none" when it registered none.
func registeredDifficulties(mask uint16) string {
	var names []string
	for i, code := range []uint{9, 6, 7, 8} {
		if mask>>code&1 != 0 {
			names = append(names, playlist.BotDifficulties[i])
		}
	}
	if len(names) == 0 {
		return "none"
	}
	return strings.Join(names, ",")
}
