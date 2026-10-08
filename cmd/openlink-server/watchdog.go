package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"halocommunity/internal/hostctl"
)

// watchdogConfig: the watchdog restarts a game server that stopped working,
// using the lobby report OpenLink Server already reads every second. It is on
// unless Off. A server counts as stuck when its engine stops ticking, or when it
// stays between matches (starting, ending, tearing down, or in the lobby with a
// start that never happens) for StuckMinutes. With MaxMatchMinutes, a match that
// runs that long is ended (as End Game would), and the server is restarted if
// the match has still not ended two minutes later.
type watchdogConfig struct {
	Off             bool `json:"off,omitempty"`
	StuckMinutes    int  `json:"stuck_minutes,omitempty"`     // default 10
	MaxMatchMinutes int  `json:"max_match_minutes,omitempty"` // default 0: no limit
}

func (c *watchdogConfig) validate() error {
	if c.StuckMinutes < 0 || c.StuckMinutes > 24*60 || c.MaxMatchMinutes < 0 || c.MaxMatchMinutes > 24*60 {
		return fmt.Errorf("watchdog stuck_minutes and max_match_minutes must be 0-%d", 24*60)
	}
	return nil
}

const (
	defaultStuck = 10 * time.Minute
	frozenAfter  = 2 * time.Minute // no engine tick for this long (map loads take well under a minute)
	endGrace     = 2 * time.Minute // after End Match, before a restart
)

// Server lifecycle states the watchdog times (statePreGame, stateInGame,
// stateEndGame and stateStarting are in match.go and voting.go).
const (
	stateSetup    int32 = 6
	stateWaiting  int32 = 7
	stateTeardown int32 = 12
)

type watchAction int

const (
	watchNone watchAction = iota
	watchEndMatch
	watchRestart
)

// watchdog decides from successive lobby reports; one per game server process.
type watchdog struct {
	stuck, maxMatch time.Duration
	phase           int32 // state, or -1 for the lobby with a start requested
	since           time.Time
	ticks           uint32
	ticksAt         time.Time
	ended           time.Time // End Match sent in this match
	done            bool      // restart requested; nothing more to do for this process
}

func newWatchdog(c *watchdogConfig) *watchdog {
	if c != nil && c.Off {
		return nil
	}
	w := &watchdog{stuck: defaultStuck, phase: -2}
	if c != nil {
		if c.StuckMinutes > 0 {
			w.stuck = time.Duration(c.StuckMinutes) * time.Minute
		}
		w.maxMatch = time.Duration(c.MaxMatchMinutes) * time.Minute
	}
	return w
}

// observe takes one lobby report. haveTicks is false for DLLs that do not count
// ticks; startRequested is the lobby's start mode 1.
func (w *watchdog) observe(now time.Time, state int32, startRequested bool, ticks uint32, haveTicks bool) (watchAction, string) {
	if w == nil || w.done {
		return watchNone, ""
	}
	phase := state
	if state == statePreGame && startRequested {
		phase = -1
	}
	if phase != w.phase {
		w.phase, w.since, w.ended = phase, now, time.Time{}
	}
	if haveTicks {
		if ticks != w.ticks || w.ticksAt.IsZero() {
			w.ticks, w.ticksAt = ticks, now
		} else if now.Sub(w.ticksAt) >= frozenAfter {
			w.done = true
			return watchRestart, fmt.Sprintf("the game server stopped running (no engine tick for %s)", frozenAfter)
		}
	}
	in := now.Sub(w.since)
	switch phase {
	case stateSetup, stateWaiting, stateStarting, stateEndGame, stateTeardown, -1:
		if in >= w.stuck {
			w.done = true
			return watchRestart, fmt.Sprintf("stuck %s for %s", phaseName(phase), w.stuck)
		}
	case stateInGame:
		switch {
		case w.maxMatch > 0 && w.ended.IsZero() && in >= w.maxMatch:
			w.ended = now
			return watchEndMatch, fmt.Sprintf("the match ran for %s (max_match_minutes)", w.maxMatch)
		case !w.ended.IsZero() && now.Sub(w.ended) >= endGrace:
			w.done = true
			return watchRestart, fmt.Sprintf("the match did not end %s after End Match", endGrace)
		}
	}
	return watchNone, ""
}

func phaseName(phase int32) string {
	switch phase {
	case -1:
		return "in the lobby waiting for the match to start"
	case stateSetup, stateWaiting:
		return "setting up"
	case stateStarting:
		return "starting a match"
	case stateEndGame:
		return "ending a match"
	case stateTeardown:
		return "tearing down"
	}
	return fmt.Sprintf("in state %d", phase)
}

// watchdogAct carries out a watchdog decision.
func (a *agent) watchdogAct(ctx context.Context, controller lobbyController, act watchAction, reason string) {
	switch act {
	case watchEndMatch:
		request, cancel := context.WithTimeout(ctx, 4*time.Second)
		reply, err := controller.EndMatch(request)
		cancel()
		if err != nil || reply.Code != hostctl.CodeOK {
			a.log.Warn("watchdog: End Match failed; the server restarts if the match does not end", "reason", reason, "err", err, "code", reply.Code)
			return
		}
		a.log.Warn("watchdog: ended the match", "reason", reason)
	case watchRestart:
		a.restartServer("watchdog: " + reason)
	}
}

// restartServer ends the game server process; superviseServer starts it again.
func (a *agent) restartServer(reason string) {
	a.mu.Lock()
	pid := a.pid
	a.mu.Unlock()
	if pid == 0 {
		return
	}
	a.log.Warn("restarting the game server", "reason", reason, "pid", pid)
	if p, err := os.FindProcess(pid); err == nil {
		if err := p.Kill(); err != nil {
			a.log.Error("could not stop the game server", "pid", pid, "err", err)
		}
	}
}
