package main

import (
	"testing"
	"time"
)

// step feeds one report and returns the decision.
type report struct {
	at             time.Duration
	state          int32
	startRequested bool
	ticks          uint32
}

func run(w *watchdog, reports []report) (watchAction, string, int) {
	t0 := time.Date(2026, 10, 8, 20, 0, 0, 0, time.UTC)
	for i, r := range reports {
		if act, why := w.observe(t0.Add(r.at), r.state, r.startRequested, r.ticks, true); act != watchNone {
			return act, why, i
		}
	}
	return watchNone, "", -1
}

func TestWatchdogLeavesHealthyServerAlone(t *testing.T) {
	w := newWatchdog(nil)
	// A long lobby wait, a 40-minute match, a short postgame: all normal.
	act, why, _ := run(w, []report{
		{0, statePreGame, false, 1}, {3 * time.Hour, statePreGame, false, 2},
		{3*time.Hour + time.Minute, statePreGame, true, 3}, {3*time.Hour + 2*time.Minute, stateStarting, true, 4},
		{3*time.Hour + 3*time.Minute, stateInGame, false, 5}, {3*time.Hour + 43*time.Minute, stateInGame, false, 6},
		{3*time.Hour + 44*time.Minute, stateEndGame, false, 7}, {3*time.Hour + 46*time.Minute, statePreGame, false, 8},
	})
	if act != watchNone {
		t.Fatalf("acted on a healthy server: %v %s", act, why)
	}
}

func TestWatchdogRestartsStuckServer(t *testing.T) {
	for name, reports := range map[string][]report{
		"starting":      {{0, stateStarting, false, 1}, {9 * time.Minute, stateStarting, false, 2}, {10 * time.Minute, stateStarting, false, 3}},
		"postgame":      {{0, stateEndGame, false, 1}, {10 * time.Minute, stateEndGame, false, 2}},
		"start ignored": {{0, statePreGame, true, 1}, {10 * time.Minute, statePreGame, true, 2}},
		"frozen":        {{0, stateInGame, false, 7}, {time.Minute, stateInGame, false, 7}, {2 * time.Minute, stateInGame, false, 7}},
	} {
		act, why, at := run(newWatchdog(nil), reports)
		if act != watchRestart || at != len(reports)-1 {
			t.Errorf("%s: %v %q at report %d", name, act, why, at)
		}
	}
	// Changing state starts the clock over.
	act, _, _ := run(newWatchdog(nil), []report{{0, stateStarting, false, 1}, {9 * time.Minute, stateInGame, false, 2},
		{10 * time.Minute, stateStarting, false, 3}, {19 * time.Minute, stateStarting, false, 4}})
	if act != watchNone {
		t.Fatal("restarted although no state lasted the limit")
	}
}

func TestWatchdogMatchLimit(t *testing.T) {
	w := newWatchdog(&watchdogConfig{MaxMatchMinutes: 30})
	act, _, at := run(w, []report{{0, stateInGame, false, 1}, {29 * time.Minute, stateInGame, false, 2}, {30 * time.Minute, stateInGame, false, 3}})
	if act != watchEndMatch || at != 2 {
		t.Fatalf("no End Match at the limit: %v at %d", act, at)
	}
	t0 := time.Date(2026, 10, 8, 20, 0, 0, 0, time.UTC)
	if act, _ := w.observe(t0.Add(31*time.Minute), stateInGame, false, 4, true); act != watchNone {
		t.Fatal("acted again during the grace period")
	}
	if act, _ := w.observe(t0.Add(32*time.Minute), stateInGame, false, 5, true); act != watchRestart {
		t.Fatal("no restart when the match did not end")
	}
	if act, _ := w.observe(t0.Add(40*time.Minute), stateStarting, false, 6, true); act != watchNone {
		t.Fatal("acted after its restart")
	}
	// End Match worked: no restart, and the next match gets its own limit.
	w = newWatchdog(&watchdogConfig{MaxMatchMinutes: 30})
	if act, _, _ := run(w, []report{{0, stateInGame, false, 1}, {30 * time.Minute, stateInGame, false, 2}}); act != watchEndMatch {
		t.Fatalf("no End Match: %v", act)
	}
	for i, r := range []report{{31 * time.Minute, stateEndGame, false, 3}, {33 * time.Minute, statePreGame, false, 4},
		{34 * time.Minute, stateInGame, false, 5}, {63 * time.Minute, stateInGame, false, 6}} {
		if act, _ := w.observe(t0.Add(r.at), r.state, r.startRequested, r.ticks, true); act != watchNone {
			t.Fatalf("report %d: acted after the match ended: %v", i, act)
		}
	}
}

func TestWatchdogOffAndOldDLL(t *testing.T) {
	var none *watchdog = newWatchdog(&watchdogConfig{Off: true})
	if act, _ := none.observe(time.Now(), stateStarting, false, 0, true); act != watchNone {
		t.Fatal("an off watchdog acted")
	}
	w := newWatchdog(nil)
	t0 := time.Now()
	for i := 0; i < 5; i++ { // no tick counter: never "frozen"
		if act, _ := w.observe(t0.Add(time.Duration(i)*time.Minute), statePreGame, false, 0, false); act != watchNone {
			t.Fatal("restarted on a DLL without tick counts")
		}
	}
}

func TestWatchdogConfig(t *testing.T) {
	for _, bad := range []watchdogConfig{{StuckMinutes: -1}, {MaxMatchMinutes: -1}, {StuckMinutes: 24*60 + 1}} {
		if bad.validate() == nil {
			t.Errorf("accepted %+v", bad)
		}
	}
	if w := newWatchdog(&watchdogConfig{StuckMinutes: 3}); w.stuck != 3*time.Minute || w.maxMatch != 0 {
		t.Fatalf("limits %v %v", w.stuck, w.maxMatch)
	}
}
