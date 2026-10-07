package main

import (
	"context"
	"fmt"
	"math/rand/v2"
	"net"
	"sort"
	"sync"
	"time"

	"halocommunity/internal/hostctl"
	"halocommunity/internal/playlist"
	"halocommunity/vote"
)

// voteConfig turns on playlist voting: whenever the lobby has players and no
// match is queued, players vote in the OpenLink app between up to Options
// random playlist entries; the winner is selected and the match starts after
// StartDelaySeconds. It replaces auto_start and playlist rotation order.
type voteConfig struct {
	Seconds           int `json:"seconds"`             // vote window (default 30)
	Options           int `json:"options"`             // choices per vote, 1-4 (default 4)
	StartDelaySeconds int `json:"start_delay_seconds"` // after the vote closes (default 5)
}

func (v voteConfig) window() time.Duration {
	if v.Seconds <= 0 {
		return 30 * time.Second
	}
	return time.Duration(v.Seconds) * time.Second
}
func (v voteConfig) options() int {
	if v.Options <= 0 || v.Options > vote.MaxOptions {
		return vote.MaxOptions
	}
	return v.Options
}
func (v voteConfig) startDelay() time.Duration {
	if v.StartDelaySeconds < 0 {
		return 0
	}
	if v.StartDelaySeconds == 0 {
		return 5 * time.Second
	}
	return time.Duration(v.StartDelaySeconds) * time.Second
}

// voteController is the host control the vote loop needs.
type voteController interface {
	hostController
	Start(context.Context) (hostctl.Reply, error)
}

// ballotNet reaches the players' apps through the proxy (relay.Forwarder).
type ballotNet interface {
	Clients(window time.Duration) []*net.UDPAddr
	HasSession(client *net.UDPAddr, window time.Duration) bool
	SendTo(client *net.UDPAddr, b []byte) error
}

// voteInfo is the vote state shown by the admin API.
type voteInfo struct {
	Phase   string   `json:"phase"` // waiting, voting, starting, playing
	Round   uint64   `json:"round,omitempty"`
	Options []string `json:"options,omitempty"`
	Counts  []int    `json:"counts,omitempty"`
	Winner  string   `json:"winner,omitempty"`
	Current string   `json:"current,omitempty"` // entry of the last match the vote started
	Error   string   `json:"error,omitempty"`
}

type votePhase int

const (
	phaseWaiting votePhase = iota
	phaseVoting
	phaseStarting
	phasePlaying
)

func (p votePhase) String() string {
	return [...]string{"waiting", "voting", "starting", "playing"}[p]
}

type voteRound struct {
	id       uint64
	options  []playlist.Entry
	votes    map[string]int // client address -> choice
	deadline time.Time
	closed   bool
	winner   int
	startAt  time.Time
}

// voter runs playlist voting for one connected server.
type voter struct {
	log interface {
		Info(string, ...any)
		Warn(string, ...any)
		Error(string, ...any)
	}
	ctl     voteController
	net     ballotNet
	entries []playlist.Entry
	window  time.Duration
	delay   time.Duration
	options int
	poll    time.Duration
	rng     *rand.Rand
	publish func(voteInfo)                           // admin status; may be nil
	teams   func(*playlist.Entry) hostctl.TeamPolicy // team rules per entry; may be nil

	mu      sync.Mutex
	round   *voteRound
	phase   votePhase
	rounds  uint64
	current string // entry of the last match started; published for the directory listing
}

const (
	// voterWindow: a player counts for ballots and votes if their game sent
	// traffic this recently.
	voterWindow = 15 * time.Second
	// statePreGame is the server lifecycle state of the lobby (HostPreGame).
	statePreGame int32 = 8
	// startTimeout: a requested start that has not begun a match by then
	// (players left, or the engine refused) goes back to voting.
	startTimeout = time.Minute
)

// intercept handles vote datagrams from the proxy. It answers a valid vote
// from a connected player with that player's updated ballot and consumes any
// other vote-prefixed datagram so it never reaches the game server.
func (v *voter) intercept(b []byte, client *net.UDPAddr) ([]byte, bool) {
	if !vote.Is(b) {
		return nil, false
	}
	c, ok := vote.DecodeCast(b)
	if !ok || !v.net.HasSession(client, voterWindow) {
		return nil, true
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	r := v.round
	if r == nil || r.closed || c.Round != r.id || c.Choice >= len(r.options) || time.Now().After(r.deadline) {
		return nil, true
	}
	r.votes[client.String()] = c.Choice
	v.publishLocked("")
	d, err := vote.EncodeBallot(v.ballotLocked(client.String(), time.Now()))
	if err != nil {
		return nil, true
	}
	return d, true
}

// ballotLocked builds the ballot for one player. v.mu held.
func (v *voter) ballotLocked(client string, now time.Time) vote.Ballot {
	r := v.round
	b := vote.Ballot{Round: r.id, Counts: make([]int, len(r.options)), Mine: -1, Winner: -1, StartMS: -1}
	for _, e := range r.options {
		b.Options = append(b.Options, ballotOption(e))
	}
	for k, choice := range r.votes {
		b.Counts[choice]++
		if k == client {
			b.Mine = choice
		}
	}
	if r.closed {
		b.Closed, b.Winner = true, r.winner
		b.StartMS = max(0, r.startAt.Sub(now).Milliseconds())
	} else {
		b.RemainingMS = max(0, r.deadline.Sub(now).Milliseconds())
	}
	return b
}

// broadcast sends every connected player their ballot while a vote is open or
// its result is waiting for the match to start.
func (v *voter) broadcast(now time.Time) {
	v.mu.Lock()
	if v.round == nil || (v.phase != phaseVoting && v.phase != phaseStarting) {
		v.mu.Unlock()
		return
	}
	type out struct {
		to *net.UDPAddr
		d  []byte
	}
	var msgs []out
	for _, c := range v.net.Clients(voterWindow) {
		if d, err := vote.EncodeBallot(v.ballotLocked(c.String(), now)); err == nil {
			msgs = append(msgs, out{c, d})
		}
	}
	v.mu.Unlock()
	for _, m := range msgs {
		v.net.SendTo(m.to, m.d)
	}
}

func (v *voter) publishLocked(errText string) {
	if v.publish == nil {
		return
	}
	info := voteInfo{Phase: v.phase.String(), Current: v.current, Error: errText}
	if r := v.round; r != nil {
		info.Round = r.id
		info.Counts = make([]int, len(r.options))
		for _, e := range r.options {
			info.Options = append(info.Options, e.ID)
		}
		for _, c := range r.votes {
			info.Counts[c]++
		}
		if r.closed {
			info.Winner = r.options[r.winner].ID
		}
	}
	v.publish(info)
}

func (v *voter) setPhase(p votePhase, errText string) {
	v.mu.Lock()
	v.phase = p
	if p == phaseWaiting || p == phasePlaying {
		v.round = nil
	}
	v.publishLocked(errText)
	v.mu.Unlock()
}

// pickOptions draws up to n distinct entries at random, leaving out the entry
// just played when anything else is available.
func pickOptions(entries []playlist.Entry, last string, n int, rng *rand.Rand) []playlist.Entry {
	pool := make([]playlist.Entry, 0, len(entries))
	for _, e := range entries {
		if e.ID != last || len(entries) == 1 {
			pool = append(pool, e)
		}
	}
	rng.Shuffle(len(pool), func(i, j int) { pool[i], pool[j] = pool[j], pool[i] })
	return pool[:min(n, len(pool))]
}

// openRound starts a vote. v.mu not held.
func (v *voter) openRound(last string, now time.Time) {
	opts := pickOptions(v.entries, last, v.options, v.rng)
	v.mu.Lock()
	v.rounds++
	r := &voteRound{id: v.rounds, options: opts, votes: map[string]int{}, deadline: now.Add(v.window), winner: -1}
	if len(opts) == 1 {
		r.deadline = now // nothing to choose
	}
	v.round, v.phase = r, phaseVoting
	v.publishLocked("")
	v.mu.Unlock()
	names := make([]string, len(opts))
	for i, e := range opts {
		names[i] = e.ID
	}
	v.log.Info("vote open", "round", r.id, "options", names, "seconds", v.window.Seconds())
}

// ranking orders the choices by votes, ties broken at random; with no votes
// at all every choice ties.
func ranking(counts []int, rng *rand.Rand) []int {
	order := rng.Perm(len(counts))
	sort.SliceStable(order, func(i, j int) bool { return counts[order[i]] > counts[order[j]] })
	return order
}

// closeRound tallies the vote and selects the winner, falling back to the
// next-ranked choice if the server refuses a selection.
func (v *voter) closeRound(ctx context.Context, now time.Time) {
	v.mu.Lock()
	r := v.round
	counts := make([]int, len(r.options))
	for _, c := range r.votes {
		counts[c]++
	}
	v.mu.Unlock()
	var lastErr error
	for _, i := range ranking(counts, v.rng) {
		e := r.options[i]
		selection := entrySelection(e, false)
		pair, err := parseSelection(selection)
		var reply hostctl.Reply
		if err == nil {
			request, cancel := context.WithTimeout(ctx, 4*time.Second)
			reply, err = dispatchSelection(request, v.ctl, selection, pair)
			cancel()
		}
		if err == nil && reply.Code == hostctl.CodeSelected {
			v.mu.Lock()
			r.closed, r.winner, r.startAt = true, i, now.Add(v.delay)
			v.phase = phaseStarting
			v.publishLocked("")
			v.mu.Unlock()
			v.log.Info("vote closed", "round", r.id, "winner", e.ID, "counts", counts, "starting_in", v.delay)
			v.sendTeams(ctx, &e)
			return
		}
		if err == nil {
			err = fmt.Errorf("server replied %v", controlReply(reply)["status"])
		}
		lastErr = err
		v.log.Warn("vote winner could not be selected; trying the next choice", "entry", e.ID, "err", err)
	}
	v.log.Error("no vote choice could be selected; opening a new vote", "err", lastErr)
	v.setPhase(phaseWaiting, fmt.Sprintf("selection failed: %v", lastErr))
}

// initialize makes the server's first selection (a random entry), which the
// native provider needs before any later selection. Returns false if ctx ends.
func (v *voter) initialize(ctx context.Context) bool {
	for ctx.Err() == nil && !v.ctl.Closed() {
		st, err := v.status(ctx)
		if err == nil && st.Gates&hostctl.GateLobby != 0 {
			e := v.entries[v.rng.IntN(len(v.entries))]
			selection := entrySelection(e, true)
			pair, perr := parseSelection(selection)
			if perr == nil {
				request, cancel := context.WithTimeout(ctx, 4*time.Second)
				reply, err := dispatchSelection(request, v.ctl, selection, pair)
				cancel()
				if err == nil && reply.Code == hostctl.CodeSelected {
					v.log.Info("vote: server selection initialized", "entry", e.ID)
					v.sendTeams(ctx, &e)
					return true
				}
				v.log.Warn("vote: initial selection failed", "entry", e.ID, "err", err, "code", reply.Code)
			}
		}
		sleep(ctx, v.poll)
	}
	return false
}

func (v *voter) sendTeams(ctx context.Context, e *playlist.Entry) {
	if v.teams != nil {
		sendTeams(ctx, v.ctl, v.teams(e), v.log, e.ID)
	}
}

func (v *voter) status(ctx context.Context) (hostctl.Reply, error) {
	request, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	return v.ctl.Status(request)
}

// run drives voting until ctx ends or the transport closes.
func (v *voter) run(ctx context.Context) {
	v.setPhase(phaseWaiting, "")
	if !v.initialize(ctx) {
		return
	}
	var last string     // entry of the last started match
	var baseline uint32 // matches started when our Start was accepted
	var startTries int  // failed Start attempts for the current result
	var startedAt time.Time
	var nextSend time.Time // ballot broadcast pacing
	for ctx.Err() == nil && !v.ctl.Closed() {
		st, err := v.status(ctx)
		if err != nil {
			sleep(ctx, v.poll)
			continue
		}
		if st.Version < 3 {
			v.log.Error("openlink-control.dll does not report the lobby; voting off")
			v.setPhase(phaseWaiting, "DLL has no lobby report (needs v3)")
			return
		}
		now := time.Now()
		lobby := st.State == statePreGame && st.Gates&hostctl.GateLobby != 0
		players := st.Lobby.Connected
		// Everyone left: the next players are a new group, so their first vote
		// may offer any entry, including the one played last.
		if lobby && players == 0 && last != "" {
			v.log.Info("vote: lobby empty; every playlist entry can be offered again", "last_played", last)
			last = ""
		}
		v.mu.Lock()
		phase := v.phase
		v.mu.Unlock()
		switch phase {
		case phaseWaiting:
			if lobby && players >= 1 {
				v.openRound(last, now)
				nextSend = time.Time{}
			}
		case phaseVoting, phaseStarting:
			if st.Matches > baseline && !lobby {
				// A match began without our Start (players cannot start one; kept as a fallback).
				v.setPhase(phasePlaying, "")
				baseline = st.Matches
				break
			}
			if players == 0 {
				v.log.Info("vote: lobby emptied; waiting for players")
				v.setPhase(phaseWaiting, "")
				break
			}
			v.mu.Lock()
			r := v.round
			due := phase == phaseVoting && !now.Before(r.deadline)
			startDue := phase == phaseStarting && !now.Before(r.startAt)
			v.mu.Unlock()
			if due {
				v.closeRound(ctx, now)
				nextSend = time.Time{}
			}
			if startDue && lobby {
				request, cancel := context.WithTimeout(ctx, 4*time.Second)
				reply, err := v.ctl.Start(request)
				cancel()
				if err == nil && reply.Code == hostctl.CodeOK {
					v.mu.Lock()
					last = r.options[r.winner].ID
					v.current = last
					v.mu.Unlock()
					baseline, startTries, startedAt = st.Matches, 0, now
					v.log.Info("vote: match start requested", "entry", last)
					v.setPhase(phasePlaying, "")
				} else if startTries++; startTries >= 5 {
					v.log.Error("vote: match start failed", "err", err, "code", reply.Code)
					startTries = 0
					v.setPhase(phaseWaiting, "start failed")
				}
			}
		case phasePlaying:
			if st.Matches > baseline && lobby {
				v.setPhase(phaseWaiting, "")
			} else if st.Matches == baseline && lobby && (players == 0 || now.Sub(startedAt) > startTimeout) {
				// The players left before the match began (the DLL withdraws the
				// start), or the engine never began it: vote again.
				v.log.Info("vote: requested match did not begin; waiting for players", "players", players)
				v.setPhase(phaseWaiting, "")
			}
		}
		if now.After(nextSend) {
			v.broadcast(now)
			nextSend = now.Add(time.Second)
		}
		sleep(ctx, v.poll)
	}
}

// interceptVote routes vote datagrams from the proxy to the running vote.
func (a *agent) interceptVote(b []byte, client *net.UDPAddr) ([]byte, bool) {
	a.mu.Lock()
	v := a.voter
	a.mu.Unlock()
	if v == nil {
		if vote.Is(b) {
			return nil, true // never forward vote messages to the game server
		}
		return nil, false
	}
	return v.intercept(b, client)
}

// ballotOption is how a playlist entry appears on a ballot. The playlist
// check (playlistcheck.go) sizes ballots with it too.
func ballotOption(e playlist.Entry) vote.Option {
	name := e.Name
	if name == "" {
		name = e.ID
	}
	return vote.Option{ID: vote.Name(e.ID), Name: vote.Name(name), Thumb: e.ThumbRef()}
}

// runVoting runs voting for the connected server with the playlist checked
// at agent start.
func (a *agent) runVoting(ctx context.Context, controller voteController) {
	f := a.playlist
	if f == nil {
		a.log.Error("no checked playlist; voting off")
		a.mu.Lock()
		a.vote = &voteInfo{Phase: "off", Error: "no checked playlist"}
		a.mu.Unlock()
		return
	}
	cfg := *a.cfg.Vote
	v := &voter{log: a.log, ctl: controller, net: a.fwd, entries: f.Entries, window: cfg.window(),
		delay: cfg.startDelay(), options: cfg.options(), poll: 250 * time.Millisecond,
		rng:     rand.New(rand.NewPCG(uint64(time.Now().UnixNano()), 0x6f706c6b)),
		publish: func(info voteInfo) { a.mu.Lock(); a.vote = &info; a.mu.Unlock() },
		teams:   a.entryTeams}
	a.mu.Lock()
	a.voter = v
	a.mu.Unlock()
	defer func() { a.mu.Lock(); a.voter = nil; a.mu.Unlock() }()
	a.log.Info("playlist voting on", "entries", len(f.Entries), "options", v.options,
		"vote_seconds", v.window.Seconds(), "start_delay_seconds", v.delay.Seconds())
	v.run(ctx)
}
