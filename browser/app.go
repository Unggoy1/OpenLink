package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"halocommunity/connect"
	"halocommunity/internal/api"
	"halocommunity/internal/diag"
	"halocommunity/internal/game"
	"halocommunity/vote"
)

// App is bound to the frontend; its exported methods are callable from JS.
type App struct {
	ctx context.Context

	mu       sync.Mutex
	settings Settings
	sess     *connect.Session

	notified uint64       // last vote round the player was alerted to
	overlay  *voteOverlay // in-game vote overlay; nil off Windows

	events     *diag.Events // recent events for Diagnostics
	autoMode   bool         // this session uses broadcast because a server runs on this PC
	lastPhase  string       // session phase last recorded in events
	lastListOK string       // last listing result recorded in events
}

// NewApp creates the app with saved settings.
func NewApp() *App {
	a := &App{settings: loadSettings(), events: diag.NewEvents(200)}
	a.events.Add("OpenLink %s started", version)
	a.overlay = newVoteOverlay(a.Vote)
	return a
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	go a.watchVotes(ctx)
}

// shutdown releases the game ports when the window closes.
func (a *App) shutdown(context.Context) {
	a.overlay.close()
	a.Leave()
}

// ServerView is a listing as shown in the browser.
type ServerView struct {
	ID           string     `json:"id"`
	Key          string     `json:"key"` // host:port; stable identity for favourites
	Name         string     `json:"name"`
	Description  string     `json:"description"` // one line from the host; "" = none
	Region       string     `json:"region"`
	Status       string     `json:"status"`
	Joinable     bool       `json:"joinable"`
	Build        string     `json:"build"`
	BuildMatch   bool       `json:"buildMatch"`   // false also when the local build is unknown
	Players      int        `json:"players"`      // -1 = unknown
	Reachability string     `json:"reachability"` // unknown, ok, unreachable (directory's check)
	PingMS       int        `json:"pingMs"`       // -1 = no answer (host not in proxy mode)
	Favorite     bool       `json:"favorite"`
	Match        *MatchView `json:"match"` // what the server is playing; nil when the host does not say
}

// MatchView is what a server is playing, as its host agent reports it.
type MatchView struct {
	Phase string `json:"phase"` // lobby, voting, starting, in_game, post_game
	Name  string `json:"name"`  // "" when no entry applies (for example while voting)
	// Thumbs are the map thumbnail URLs to try in order (built here from the
	// reported map IDs, never taken from the directory). Empty = none.
	Thumbs []string `json:"thumbs"`
}

func matchView(m *api.Match) *MatchView {
	if m == nil || !m.Valid() {
		return nil
	}
	return &MatchView{Phase: m.Phase, Name: m.Name, Thumbs: vote.ThumbURLs(m.Thumb)}
}

// StatusView describes the active session.
type StatusView struct {
	ServerID   string `json:"serverId"`
	ServerName string `json:"serverName"`
	// GameName is how the server appears in Halo's in-game server list (capitals,
	// api.GameName); "" when the game shows the host's PC name.
	GameName  string  `json:"gameName"`
	AutoMode  bool    `json:"autoMode"` // broadcast chosen because a server runs on this PC
	Mode      string  `json:"mode"`
	BeaconAge float64 `json:"beaconAge"` // seconds; -1 = no beacon yet
	Connected bool    `json:"connected"` // the server answered in the last few seconds
	UpKB      float64 `json:"upKB"`
	DownKB    float64 `json:"downKB"`
	Error     string  `json:"error"`
}

// GetSettings returns the current settings.
func (a *App) GetSettings() Settings {
	a.mu.Lock()
	defer a.mu.Unlock()
	s := a.settings
	s.Favorites = slices.Clone(s.Favorites)
	return s
}

// SaveSettings validates, stores and applies settings. Favourites are kept as
// they are; change them with SetFavorite.
func (a *App) SaveSettings(s Settings) error {
	s.Directory = strings.TrimRight(strings.TrimSpace(s.Directory), "/")
	s.InstallDir = strings.TrimSpace(s.InstallDir)
	if s.Mode != connect.ModeLoopback && s.Mode != connect.ModeBroadcast {
		s.Mode = connect.ModeLoopback
	}
	if s.Directory != "" && !strings.HasPrefix(s.Directory, "https://") && !strings.HasPrefix(s.Directory, "http://") {
		return errors.New("the directory address must start with https://")
	}
	if err := normalizeOverlay(&s); err != nil {
		return err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	s.Favorites = a.settings.Favorites
	if err := saveSettings(s); err != nil {
		return err
	}
	a.settings = s
	return nil
}

// SetFavorite marks or unmarks a server (by key) as a favourite.
func (a *App) SetFavorite(key string, on bool) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	s := a.settings
	s.Favorites = slices.DeleteFunc(slices.Clone(s.Favorites), func(k string) bool { return k == key })
	if on {
		s.Favorites = append(s.Favorites, key)
	}
	if err := saveSettings(s); err != nil {
		return err
	}
	a.settings = s
	return nil
}

// LocalBuild returns the installed game build, or "" if the game was not found.
func (a *App) LocalBuild() string {
	return connect.LocalBuild(a.GetSettings().InstallDir)
}

// ListServers fetches all listings and pings the ones that answer probes.
// Favourites come first, then joinable servers on the player's build.
func (a *App) ListServers() ([]ServerView, error) {
	if demo != nil {
		return demo.servers(), nil
	}
	s := a.GetSettings()
	if s.Directory == "" {
		return nil, errors.New("no directory address set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	servers, err := connect.List(ctx, s.Directory, "")
	a.noteList(len(servers), err)
	if err != nil {
		return nil, err
	}
	local := connect.LocalBuild(s.InstallDir)
	out := make([]ServerView, len(servers))
	var wg sync.WaitGroup
	sem := make(chan struct{}, 8)
	for i, sv := range servers {
		key := net.JoinHostPort(sv.Host, strconv.Itoa(sv.Port))
		out[i] = ServerView{ID: sv.ID, Key: key, Name: sv.Name, Description: sv.Description, Region: sv.Region, Status: sv.Status,
			Joinable: sv.Joinable, Build: sv.Build, BuildMatch: local != "" && sv.Build == local, Players: sv.Players,
			Reachability: sv.Reachability, PingMS: -1, Favorite: slices.Contains(s.Favorites, key), Match: matchView(sv.Match)}
		if !sv.Proxy {
			continue // only proxy-mode hosts answer probes
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			pctx, pcancel := context.WithTimeout(ctx, 3*time.Second)
			defer pcancel()
			if rtt, ok := connect.Ping(pctx, sv); ok {
				out[i].PingMS = int(rtt.Milliseconds())
			}
		}()
	}
	wg.Wait()
	sort.SliceStable(out, func(i, j int) bool {
		ri, rj := rank(out[i]), rank(out[j])
		if ri != rj {
			return ri < rj
		}
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	return out, nil
}

func rank(s ServerView) int {
	r := 0
	if !s.Favorite {
		r += 4
	}
	switch {
	case s.Joinable && s.BuildMatch:
	case s.Joinable:
		r++
	default:
		r += 2
	}
	return r
}

// Join stops any current session and starts one for the server with this ID.
func (a *App) Join(id string) error {
	s := a.GetSettings()
	if s.Directory == "" {
		return errors.New("no directory address set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	servers, err := connect.List(ctx, s.Directory, "")
	if err != nil {
		return err
	}
	var target *connect.Server
	for i := range servers {
		if servers[i].ID == id {
			target = &servers[i]
		}
	}
	if target == nil {
		return errors.New("that server is no longer listed")
	}
	a.Leave() // free UDP 1343 before taking it again
	// With a server on this PC, it holds the discovery port and loopback
	// beacons never reach the game, so this session broadcasts instead.
	mode, auto := s.Mode, false
	if mode != connect.ModeBroadcast && game.LocalServerRunning() {
		mode, auto = connect.ModeBroadcast, true
	}
	sess, err := connect.Start(*target, connect.Options{Directory: s.Directory, Mode: mode})
	if err != nil {
		a.events.Add("join %q failed (mode %s): %v", target.Name, mode, err)
		return err
	}
	note := ""
	if auto {
		note = ", chosen because a server runs on this PC"
	}
	a.events.Add("joined %q (build %s, mode %s%s)", target.Name, target.Build, mode, note)
	a.mu.Lock()
	a.sess, a.autoMode, a.lastPhase = sess, auto, ""
	a.mu.Unlock()
	return nil
}

// noteList records listing results in the event log when they change.
func (a *App) noteList(n int, err error) {
	msg := fmt.Sprintf("server list: %d servers", n)
	if err != nil {
		msg = "server list failed: " + err.Error()
	}
	a.mu.Lock()
	changed := msg != a.lastListOK
	a.lastListOK = msg
	a.mu.Unlock()
	if changed {
		a.events.Add("%s", msg)
	}
}

// Leave stops the current session, if any.
func (a *App) Leave() {
	a.mu.Lock()
	sess := a.sess
	a.sess = nil
	a.mu.Unlock()
	if sess != nil {
		sess.Stop()
		a.events.Add("left %q", sess.Status().Server.Name)
	}
}

// Status returns the active session, or nil when not joined.
func (a *App) Status() *StatusView {
	if demo != nil {
		return demo.status()
	}
	a.mu.Lock()
	sess := a.sess
	a.mu.Unlock()
	if sess == nil {
		return nil
	}
	st := sess.Status()
	age := -1.0
	if st.BeaconAge >= 0 {
		age = st.BeaconAge.Seconds()
	}
	v := &StatusView{ServerID: st.Server.ID, ServerName: st.Server.Name, GameName: gameListName(st.Server.Name), Mode: st.Mode, BeaconAge: age,
		Connected: st.Connected(), UpKB: float64(st.UpBytes) / 1024, DownKB: float64(st.DownBytes) / 1024, Error: st.Err}
	a.mu.Lock()
	v.AutoMode = a.autoMode
	phase := sessionPhase(v)
	changed := phase != a.lastPhase
	a.lastPhase = phase
	a.mu.Unlock()
	if changed {
		a.events.Add("session: %s", phase)
	}
	return v
}

// sessionPhase matches the phases SessionBar shows.
func sessionPhase(v *StatusView) string {
	switch {
	case v.Error != "":
		return "error: " + v.Error
	case v.Connected:
		return "connected to the server"
	case v.BeaconAge < 0:
		return "contacting the server"
	case v.BeaconAge > 15:
		return "server stopped advertising"
	}
	return "ready, waiting for the game to join"
}

// gameListName is a server's name as Halo's in-game server list shows it.
func gameListName(name string) string { return strings.ToUpper(api.GameName(name)) }
