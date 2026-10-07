package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"halocommunity/internal/api"
	"halocommunity/internal/hostctl"
	"halocommunity/internal/playlist"
)

// config is the agent's settings. Every field can be set in openlink-server.json;
// flags given on the command line override the file.
type config struct {
	Directory   string `json:"directory"`
	RegisterKey string `json:"register_key,omitempty"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"` // one line shown under the name in the app
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

	// AutoPortForward asks the router (UPnP, then NAT-PMP) to forward
	// public_port to this PC while OpenLink Server runs. Opt-in: see HOSTING.md.
	AutoPortForward bool `json:"auto_port_forward,omitempty"`

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
	// LobbyLeaderXUID: the XUID the server keeps as LAN lobby leader so that no
	// player is leader (no lobby options, map/mode menus, Play or End Game).
	// A real server always owns its lobby. 0 = defaultLobbyLeader.
	LobbyLeaderXUID uint64 `json:"lobby_leader_xuid,omitempty"`
	// TeamBalance, for team modes at the start of every match: "even" (default:
	// even teams, players stay on their current team where the counts allow),
	// "shuffle" (random even teams) or "off" (players keep their own picks).
	// A playlist entry's "teams" sets the number of teams (default 2) and is
	// always applied, as "even" when this is "off", so every player ends up on
	// one of that entry's teams. FFA modes always keep every player on their
	// own team.
	TeamBalance string `json:"team_balance,omitempty"`
	// AutoStart: the server starts each match itself. A real server without
	// Vote gets the defaults, since no player can press Play.
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
	fs.StringVar(&f.Description, "description", "", "one line about the server, shown under its name in the app (up to 120 characters)")
	fs.StringVar(&f.Region, "region", "", "region label, e.g. us-west")
	fs.StringVar(&f.PublicHost, "public-host", "", "address players connect to; empty = the directory uses this machine's public IP")
	fs.IntVar(&f.PublicPort, "public-port", c.PublicPort, "external UDP port players connect to (your port forward)")
	fs.StringVar(&f.Install, "install", "", "game install root (folder with version.txt); empty = search every Steam library")
	fs.StringVar(&f.Sandbox, "sandbox", c.Sandbox, "value for -lan_sandbox")
	fs.BoolVar(&f.Manage, "manage", c.Manage, "start the server and restart it if it exits (required for a real server)")
	fs.BoolVar(&f.Restart, "restart", c.Restart, "restart a managed server after exit (false for a single scoped test)")
	fs.BoolVar(&f.StopServer, "stop-server", false, "stop the managed server when the agent exits")
	fs.BoolVar(&f.Proxy, "proxy", c.Proxy, "proxy mode: front the server for player counts, probes, bans and rate limits")
	fs.BoolVar(&f.AutoPortForward, "auto-port-forward", false, "opt in: ask your router (UPnP or NAT-PMP) to forward the public port to this PC while running")
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
		case "description":
			c.Description = f.Description
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
		case "auto-port-forward":
			c.AutoPortForward = f.AutoPortForward
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
		c.Playlist, c.AutoStart, c.Vote = "", nil, nil
	} else {
		// A real server always runs from a playlist through the control DLL.
		if !c.Manage {
			return c, errors.New(`openlink-server must start the game server itself: remove "manage": false`)
		}
		// The directory lists a server only once its port answers a probe,
		// and only proxy mode answers probes.
		if !c.Proxy && c.Directory != "" {
			return c, errors.New(`a listed server needs proxy mode: remove "proxy": false`)
		}
		c.HostControlNative = true
		if c.HostControlDLL == "" {
			c.HostControlDLL = filepath.Join(exeDir(), "openlink-control.dll")
		}
	}
	if c.Directory != "" && !api.ValidServerName(c.Name) {
		return c, errors.New("name must be 1-48 characters with no control characters (the in-game server list shows up to 47 of them, printable ASCII only)")
	}
	if !api.ValidDescription(strings.TrimSpace(c.Description)) {
		return c, errors.New("description must be at most 120 characters with no control characters")
	}
	if c.AutoPortForward && !c.Proxy {
		return c, errors.New("auto_port_forward requires proxy mode")
	}
	if c.Vote != nil && !c.Proxy {
		return c, errors.New("vote requires proxy mode")
	}
	if c.Vote != nil && c.AutoStart != nil {
		return c, errors.New("use vote or auto_start, not both: vote starts each match after the vote")
	}
	if !c.Simulate && c.Vote == nil && c.AutoStart == nil {
		// The server owns the lobby, so a match starts only from the server.
		c.AutoStart = &autoStart{}
	}
	if c.TeamBalance != "" && c.TeamBalance != "even" && c.TeamBalance != "shuffle" && c.TeamBalance != "off" {
		return c, errors.New(`team_balance must be "even", "shuffle" or "off"`)
	}
	return c, nil
}

// teamPolicy is the server's team rules for a match of entry (nil: no playlist
// entry, two teams).
func (c config) teamPolicy(entry *playlist.Entry) hostctl.TeamPolicy {
	p := hostctl.TeamPolicy{Flags: hostctl.TeamGuardFFA}
	teams := entry != nil && entry.Teams != nil
	switch {
	case c.TeamBalance == "off" && !teams:
		return p
	case c.TeamBalance == "shuffle":
		p.Mode = hostctl.TeamModeShuffle
	}
	// "off" with an entry's teams still balances (even), so nobody stays on a
	// team the entry does not have.
	p.Flags |= hostctl.TeamBalance
	if teams {
		p.Count, p.Size = uint32(entry.Teams.Count), uint32(entry.Teams.Size)
	}
	return p
}

// defaultLobbyLeader is the placeholder lobby leader XUID: Xbox user format
// (0x0009 prefix, like 2533274962600518), but far above any issued XUID, so no
// player matches it.
const defaultLobbyLeader uint64 = 0x0009_ffff_ffff_ffff

// lobbyLeader is the XUID the server holds as lobby leader.
func (c config) lobbyLeader() uint64 {
	if c.LobbyLeaderXUID != 0 {
		return c.LobbyLeaderXUID
	}
	return defaultLobbyLeader
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
