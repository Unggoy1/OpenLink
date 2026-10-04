package main

import (
	"context"
	"errors"
	"sort"
	"strings"
	"sync"
	"time"

	"halocommunity/connect"
)

// App is bound to the frontend; its exported methods are callable from JS.
type App struct {
	ctx context.Context

	mu       sync.Mutex
	settings Settings
	sess     *connect.Session
}

// NewApp creates the app with saved settings.
func NewApp() *App {
	return &App{settings: loadSettings()}
}

func (a *App) startup(ctx context.Context) { a.ctx = ctx }

// shutdown releases the game ports when the window closes.
func (a *App) shutdown(context.Context) { a.Leave() }

// ServerView is a listing as shown in the browser.
type ServerView struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Region     string `json:"region"`
	Status     string `json:"status"`
	Joinable   bool   `json:"joinable"`
	Build      string `json:"build"`
	BuildMatch bool   `json:"buildMatch"` // false also when the local build is unknown
	Players    int    `json:"players"`    // -1 = unknown
}

// StatusView describes the active session.
type StatusView struct {
	ServerID   string  `json:"serverId"`
	ServerName string  `json:"serverName"`
	Mode       string  `json:"mode"`
	BeaconAge  float64 `json:"beaconAge"` // seconds; -1 = no beacon yet
	Connected  bool    `json:"connected"` // the server answered in the last few seconds
	UpKB       float64 `json:"upKB"`
	DownKB     float64 `json:"downKB"`
	Error      string  `json:"error"`
}

// GetSettings returns the current settings.
func (a *App) GetSettings() Settings {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.settings
}

// SaveSettings validates, stores and applies settings.
func (a *App) SaveSettings(s Settings) error {
	s.Directory = strings.TrimRight(strings.TrimSpace(s.Directory), "/")
	s.InstallDir = strings.TrimSpace(s.InstallDir)
	if s.Mode != connect.ModeLoopback && s.Mode != connect.ModeBroadcast {
		s.Mode = connect.ModeLoopback
	}
	if s.Directory != "" && !strings.HasPrefix(s.Directory, "https://") && !strings.HasPrefix(s.Directory, "http://") {
		return errors.New("the directory address must start with https://")
	}
	if err := saveSettings(s); err != nil {
		return err
	}
	a.mu.Lock()
	a.settings = s
	a.mu.Unlock()
	return nil
}

// LocalBuild returns the installed game build, or "" if the game was not found.
func (a *App) LocalBuild() string {
	return connect.LocalBuild(a.GetSettings().InstallDir)
}

// ListServers fetches all listings, joinable and matching-build first.
func (a *App) ListServers() ([]ServerView, error) {
	s := a.GetSettings()
	if s.Directory == "" {
		return nil, errors.New("no directory address set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	servers, err := connect.List(ctx, s.Directory, "")
	if err != nil {
		return nil, err
	}
	local := connect.LocalBuild(s.InstallDir)
	out := make([]ServerView, 0, len(servers))
	for _, sv := range servers {
		out = append(out, ServerView{ID: sv.ID, Name: sv.Name, Region: sv.Region, Status: sv.Status,
			Joinable: sv.Joinable, Build: sv.Build, BuildMatch: local != "" && sv.Build == local, Players: sv.Players})
	}
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
	switch {
	case s.Joinable && s.BuildMatch:
		return 0
	case s.Joinable:
		return 1
	default:
		return 2
	}
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
	sess, err := connect.Start(*target, connect.Options{Directory: s.Directory, Mode: s.Mode})
	if err != nil {
		return err
	}
	a.mu.Lock()
	a.sess = sess
	a.mu.Unlock()
	return nil
}

// Leave stops the current session, if any.
func (a *App) Leave() {
	a.mu.Lock()
	sess := a.sess
	a.sess = nil
	a.mu.Unlock()
	if sess != nil {
		sess.Stop()
	}
}

// Status returns the active session, or nil when not joined.
func (a *App) Status() *StatusView {
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
	return &StatusView{ServerID: st.Server.ID, ServerName: st.Server.Name, Mode: st.Mode, BeaconAge: age,
		Connected: st.Connected(), UpKB: float64(st.UpBytes) / 1024, DownKB: float64(st.DownBytes) / 1024, Error: st.Err}
}
