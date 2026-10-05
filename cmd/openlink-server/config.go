package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"halocommunity/internal/api"
)

// config is the agent's settings. Every field can be set in openlink-server.json;
// flags given on the command line override the file.
type config struct {
	Directory   string `json:"directory"`
	RegisterKey string `json:"register_key,omitempty"`
	Name        string `json:"name"`
	Region      string `json:"region,omitempty"`
	PublicHost  string `json:"public_host,omitempty"`
	PublicPort  int    `json:"public_port"`

	Install    string `json:"install,omitempty"`
	Sandbox    string `json:"sandbox"`
	Manage     bool   `json:"manage"`
	Restart    bool   `json:"restart"`
	StopServer bool   `json:"stop_server"`

	// Proxy mode: the server binds ServerIP:1343 and the agent listens on
	// Listen, forwarding players to it. This gives player counts, probes
	// (ping and reachability), bans and rate limits.
	Proxy      bool   `json:"proxy"`
	Listen     string `json:"listen"`    // public UDP address, e.g. 0.0.0.0:1343
	ServerIP   string `json:"server_ip"` // the server's -bindip in proxy mode
	MaxPlayers int    `json:"max_players"`
	MaxPPS     int    `json:"max_pps"`

	BindIP string `json:"bind_ip,omitempty"` // without proxy mode: the server's -bindip

	Admin    string `json:"admin"`     // local admin API address ("" = off)
	BansFile string `json:"bans_file"` // relative paths are next to the config file
	// HostControlDLL is openlink-control.dll, which a real server always
	// loads; empty in the file = the one next to the program. openlink-loader.exe
	// must sit next to the DLL.
	HostControlDLL    string `json:"control_dll,omitempty"`
	HostControlNative bool   `json:"-"` // always on for a real server
	// Playlist: map/mode rotation file (internal/playlist), required for a real server.
	Playlist string `json:"playlist,omitempty"`
	// ServerOwned: no player becomes lobby leader, so nobody gets Play or the
	// end-game option. Pair it with AutoStart or Vote.
	ServerOwned bool `json:"server_owned,omitempty"`
	// LobbyOwner, with ServerOwned: "none" (default, no player is lobby owner)
	// or "first_player" (the game's own owner; only that player sees the inert
	// Play/End Game, other players see none).
	LobbyOwner string `json:"lobby_owner,omitempty"`
	// AutoStart: the server starts each match itself.
	AutoStart *autoStart `json:"auto_start,omitempty"`
	// Vote, with a playlist and proxy mode: players vote in the OpenLink app
	// for the next match, which then starts by itself. Replaces AutoStart.
	Vote *voteConfig `json:"vote,omitempty"`

	Simulate     bool          `json:"-"`
	Loopback     bool          `json:"-"`
	CaptureDelay time.Duration `json:"-"`

	path string // the config file this came from, if any
}

func defaults() config {
	host, _ := os.Hostname()
	dir := api.Getenv("DIRECTORY")
	if dir == "" {
		dir = api.DefaultDirectory
	}
	return config{
		Directory: dir, Name: host, PublicPort: api.GamePort, Sandbox: "RETAIL", Manage: true, Restart: true,
		Proxy: true, Listen: fmt.Sprintf("0.0.0.0:%d", api.GamePort), ServerIP: "127.0.0.1",
		MaxPlayers: 32, MaxPPS: 500, Admin: "127.0.0.1:7180", BansFile: "bans.json",
		CaptureDelay: 5 * time.Second,
	}
}

// loadConfig builds the configuration: defaults, then the config file
// (-config, or openlink-server.json next to the executable if present), then flags
// that were set explicitly.
func loadConfig(fs *flag.FlagSet, args []string) (config, error) {
	c := defaults()
	f := c // flag targets; copied over the file values only when set
	cfgPath := fs.String("config", "", "config file (default: openlink-server.json next to the program, if present)")
	fs.StringVar(&f.Directory, "directory", c.Directory, "directory URL (env OPENLINK_DIRECTORY); empty = do not list")
	fs.StringVar(&f.RegisterKey, "register-key", api.Getenv("REGISTER_KEY"), "directory registration key, if the directory requires one (env OPENLINK_REGISTER_KEY)")
	fs.StringVar(&f.Name, "name", c.Name, "server name shown in the browser")
	fs.StringVar(&f.Region, "region", "", "region label, e.g. us-west")
	fs.StringVar(&f.PublicHost, "public-host", "", "address players connect to; empty = the directory uses this machine's public IP")
	fs.IntVar(&f.PublicPort, "public-port", c.PublicPort, "external UDP port players connect to (your port forward)")
	fs.StringVar(&f.Install, "install", "", "game install root (folder with version.txt); empty = search every Steam library")
	fs.StringVar(&f.Sandbox, "sandbox", c.Sandbox, "value for -lan_sandbox")
	fs.BoolVar(&f.Manage, "manage", c.Manage, "start the server and restart it if it exits (required for a real server)")
	fs.BoolVar(&f.Restart, "restart", c.Restart, "restart a managed server after exit (false for a single scoped test)")
	fs.BoolVar(&f.StopServer, "stop-server", false, "stop the managed server when the agent exits")
	fs.BoolVar(&f.Proxy, "proxy", c.Proxy, "proxy mode: front the server for player counts, probes, bans and rate limits")
	fs.StringVar(&f.Listen, "listen", c.Listen, "proxy mode: public UDP listen address")
	fs.StringVar(&f.ServerIP, "server-ip", c.ServerIP, "proxy mode: local address the server binds (use 127.0.0.2, .3, ... for more servers on one PC)")
	fs.IntVar(&f.MaxPlayers, "max-players", c.MaxPlayers, "proxy mode: most player connections at once (0 = no limit)")
	fs.IntVar(&f.MaxPPS, "max-pps", c.MaxPPS, "proxy mode: packets per second allowed from one player (0 = no limit)")
	fs.StringVar(&f.BindIP, "bind-ip", "", "without proxy mode: pass -bindip to the server")
	fs.StringVar(&f.Admin, "admin", c.Admin, "local admin API address for status/kick/ban commands (empty = off)")
	fs.StringVar(&f.BansFile, "bans", c.BansFile, "ban list file")
	fs.StringVar(&f.HostControlDLL, "control-dll", "", "path to openlink-control.dll (default: next to the program)")
	fs.StringVar(&f.Playlist, "playlist", "", "map/mode playlist file (required to run a server); the server picks every match from it")
	fs.BoolVar(&f.Simulate, "simulate", false, "no game: send simulated beacons and answer probes on the game port")
	fs.BoolVar(&f.Loopback, "loopback", false, "with -simulate: keep simulated beacons on 127.0.0.1")
	fs.DurationVar(&f.CaptureDelay, "capture-delay", c.CaptureDelay, "wait after setupComplete before listening for beacons")
	if err := fs.Parse(args); err != nil {
		return c, err
	}

	path := *cfgPath
	if path == "" {
		if dir := exeDir(); dir != "" {
			if p := filepath.Join(dir, "openlink-server.json"); fileExists(p) {
				path = p
			} else if fileExists(filepath.Join(dir, "hostagent.json")) {
				return c, errors.New("found hostagent.json next to the program: rename it to openlink-server.json")
			}
		}
	}
	if path != "" {
		b, err := os.ReadFile(path)
		if err != nil {
			return c, fmt.Errorf("read config: %w", err)
		}
		if err := json.Unmarshal(b, &c); err != nil {
			return c, fmt.Errorf("config %s: %w", path, err)
		}
		c.path, _ = filepath.Abs(path)
	}
	// Flags set on the command line win over the file.
	fs.Visit(func(fl *flag.Flag) {
		switch fl.Name {
		case "directory":
			c.Directory = f.Directory
		case "register-key":
			c.RegisterKey = f.RegisterKey
		case "name":
			c.Name = f.Name
		case "region":
			c.Region = f.Region
		case "public-host":
			c.PublicHost = f.PublicHost
		case "public-port":
			c.PublicPort = f.PublicPort
		case "install":
			c.Install = f.Install
		case "sandbox":
			c.Sandbox = f.Sandbox
		case "manage":
			c.Manage = f.Manage
		case "restart":
			c.Restart = f.Restart
		case "stop-server":
			c.StopServer = f.StopServer
		case "proxy":
			c.Proxy = f.Proxy
		case "listen":
			c.Listen = f.Listen
		case "server-ip":
			c.ServerIP = f.ServerIP
		case "max-players":
			c.MaxPlayers = f.MaxPlayers
		case "max-pps":
			c.MaxPPS = f.MaxPPS
		case "bind-ip":
			c.BindIP = f.BindIP
		case "admin":
			c.Admin = f.Admin
		case "bans":
			c.BansFile = f.BansFile
		case "control-dll":
			c.HostControlDLL = f.HostControlDLL
		case "playlist":
			c.Playlist = f.Playlist
		}
	})
	// Environment fallbacks for values the file left empty. (The directory's
	// default is set in defaults(), so an explicit "" in the file still means
	// "do not list".)
	if c.RegisterKey == "" {
		c.RegisterKey = f.RegisterKey
	}
	c.Simulate, c.Loopback, c.CaptureDelay = f.Simulate, f.Loopback, f.CaptureDelay
	if c.PublicPort < 1 || c.PublicPort > 65535 {
		return c, errors.New("public port must be 1-65535")
	}
	if c.Simulate {
		// No game: the control DLL, playlist and match settings do not apply,
		// so a full config file can still be used for the reachability check.
		c.HostControlDLL, c.HostControlNative = "", false
		c.Playlist, c.ServerOwned, c.AutoStart, c.Vote = "", false, nil, nil
	} else {
		// A real server always runs from a playlist through the control DLL.
		if !c.Manage {
			return c, errors.New(`openlink-server must start the game server itself: remove "manage": false`)
		}
		c.HostControlNative = true
		if c.HostControlDLL == "" {
			c.HostControlDLL = filepath.Join(exeDir(), "openlink-control.dll")
		}
	}
	if c.Vote != nil && !c.Proxy {
		return c, errors.New("vote requires proxy mode")
	}
	if c.Vote != nil && c.AutoStart != nil {
		return c, errors.New("use vote or auto_start, not both: vote starts each match after the vote")
	}
	if c.LobbyOwner != "" && c.LobbyOwner != "none" && c.LobbyOwner != "first_player" {
		return c, errors.New("lobby_owner must be none or first_player")
	}
	return c, nil
}

func (c config) lobbyOwner() string {
	if c.LobbyOwner == "" {
		return "none"
	}
	return c.LobbyOwner
}

// resolve makes a path relative to the config file's folder (or the working directory).
func (c config) resolve(p string) string {
	if p == "" || filepath.IsAbs(p) || c.path == "" {
		return p
	}
	return filepath.Join(filepath.Dir(c.path), p)
}

// save writes the configuration as JSON.
func (c config) save(path string) error {
	if c.HostControlDLL == filepath.Join(exeDir(), "openlink-control.dll") {
		c.HostControlDLL = "" // the default; keeps the file valid if the folder moves
	}
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0o600) // may contain the register key
}

// exeDir is the folder of the running program, or "" if unknown.
func exeDir() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	return filepath.Dir(exe)
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}
