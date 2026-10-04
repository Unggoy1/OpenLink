// hi-hostagent runs next to a retail LAN server. It starts and watches the
// server, captures the server's LAN beacons and keeps the directory listing
// fresh. In proxy mode (the default) it also fronts the server's game port, so
// it can count players, answer reachability probes, and ban or rate-limit
// clients. With -simulate it stands in for the game to test the network path.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"sync"
	"syscall"
	"time"

	"halocommunity/internal/api"
	"halocommunity/internal/beacon"
	"halocommunity/internal/game"
	"halocommunity/internal/relay"
	"halocommunity/internal/sim"
	"halocommunity/internal/udpx"
)

// version is set at build time (-ldflags "-X main.version=v0.1.0").
var version = "dev"

// playerWindow: a player counts as connected if they sent traffic this recently.
const playerWindow = 15 * time.Second

const usage = `hi-hostagent — run a Halo Infinite community server

usage:
  hi-hostagent [flags]                  run the agent (starts the server, lists it, proxies players)
  hi-hostagent [flags] init-config      save the given flags to hostagent.json next to the program
  hi-hostagent autostart enable|disable|status   start the agent at logon (Task Scheduler)
  hi-hostagent status                   show server, players and reachability (agent must be running)
  hi-hostagent kick <ip> [minutes]      disconnect a player and keep them out (default 10 min)
  hi-hostagent ban <ip> [minutes]       ban a player (default: permanent)
  hi-hostagent unban <ip> | bans        remove a ban | list bans
  hi-hostagent version

flags:
`

type agent struct {
	cfg     config
	log     *slog.Logger
	build   string
	beacons beacon.Store
	bans    *banList
	fwd     *relay.Forwarder // nil without proxy mode

	mu           sync.Mutex
	status       string // starting, ready, exited, simulated
	pid          int
	listingID    string
	reachability string
}

func (a *agent) setStatus(s string) {
	a.mu.Lock()
	changed := a.status != s
	a.status = s
	a.mu.Unlock()
	if changed {
		a.log.Info("server status", "status", s)
	}
}

func (a *agent) getStatus() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.status
}

func main() {
	fs := flag.NewFlagSet("hi-hostagent", flag.ExitOnError)
	fs.Usage = func() { fmt.Fprint(os.Stderr, usage); fs.PrintDefaults() }
	c, err := loadConfig(fs, os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(2)
	}
	if args := fs.Args(); len(args) > 0 {
		if err := runCommand(c, args); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
		return
	}

	a := &agent{cfg: c, log: slog.New(slog.NewTextHandler(os.Stderr, nil))}
	a.log.Info("hi-hostagent", "version", version, "config", orDash(c.path))
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := a.run(ctx); err != nil {
		a.log.Error("agent stopped", "err", err)
		os.Exit(1)
	}
}

func runCommand(c config, args []string) error {
	switch args[0] {
	case "version":
		fmt.Println(version)
		return nil
	case "init-config":
		path := c.path
		if path == "" {
			exe, err := os.Executable()
			if err != nil {
				return err
			}
			path = filepath.Join(filepath.Dir(exe), "hostagent.json")
		}
		if err := c.save(path); err != nil {
			return err
		}
		fmt.Printf("Saved %s. Start the agent with no flags to use it, or edit the file.\n", path)
		return nil
	case "autostart":
		return runAutostart(c, args)
	case "status", "kick", "ban", "unban", "bans":
		return runAdminCommand(c.Admin, args)
	}
	return fmt.Errorf("unknown command %q (run with -h for help)", args[0])
}

func (a *agent) run(ctx context.Context) error {
	var err error
	if a.bans, err = loadBans(a.cfg.resolve(a.cfg.BansFile)); err != nil {
		return fmt.Errorf("ban list: %w", err)
	}
	var wg sync.WaitGroup
	if a.cfg.Simulate {
		a.build = "simulated"
		if in, err := game.FindInstall(a.cfg.Install); err == nil {
			if b, err := in.Build(); err == nil {
				a.build = b // lets players on the same build list the simulated server
			}
		}
		if err := a.startSimulation(ctx, &wg); err != nil {
			return err
		}
	} else {
		in, err := game.FindInstall(a.cfg.Install)
		if err != nil {
			return err
		}
		if a.build, err = in.Build(); err != nil {
			return fmt.Errorf("read build: %w", err)
		}
		a.log.Info("game install", "root", in.Root, "build", a.build)

		existing := a.serverAlreadyRunning()
		serverBind := a.cfg.BindIP
		if a.cfg.Proxy {
			serverBind = a.cfg.ServerIP
			if existing != nil && existing.IP.IsUnspecified() {
				a.log.Warn("a server started without -bindip owns UDP 1343 on all addresses; proxy mode is off until it is restarted by the agent",
					"pid", existing.PID)
				a.cfg.Proxy = false
			} else if err := a.startProxy(ctx, &wg); err != nil {
				return err
			}
		}
		wg.Add(2)
		go func() { defer wg.Done(); a.superviseServer(ctx, in, serverBind, existing) }()
		go func() { defer wg.Done(); a.captureLoop(ctx) }()
	}
	if a.cfg.Admin != "" {
		wg.Add(1)
		go func() { defer wg.Done(); a.serveAdmin(ctx) }()
	}
	if a.cfg.Directory != "" {
		wg.Add(1)
		go func() { defer wg.Done(); a.directoryLoop(ctx) }()
	} else {
		a.log.Warn("no -directory given: the server will not be listed")
	}
	wg.Add(1)
	go func() { defer wg.Done(); a.report(ctx) }()
	wg.Wait()
	return nil
}

// serverAlreadyRunning returns a game socket on UDP 1343 owned by another
// process, if any. (The agent's own proxy socket is excluded.)
func (a *agent) serverAlreadyRunning() *game.Owner {
	owners, err := game.UDPPortOwners(api.GamePort)
	if err != nil {
		a.log.Warn("cannot read the UDP port table", "err", err)
		return nil
	}
	want := a.cfg.BindIP
	if a.cfg.Proxy {
		want = a.cfg.ServerIP
	}
	for _, o := range owners {
		if o.PID != os.Getpid() && o.Covers(want) {
			return &o
		}
	}
	return nil
}

// startProxy listens on the public game port and forwards players to the
// server's private address.
func (a *agent) startProxy(ctx context.Context, wg *sync.WaitGroup) error {
	laddr, err := net.ResolveUDPAddr("udp4", a.cfg.Listen)
	if err != nil {
		return fmt.Errorf("proxy listen address: %w", err)
	}
	conn, err := net.ListenUDP("udp4", laddr)
	if err != nil {
		return fmt.Errorf("proxy: listen on %s: %w", a.cfg.Listen, err)
	}
	server := &net.UDPAddr{IP: net.ParseIP(a.cfg.ServerIP), Port: api.GamePort}
	if server.IP == nil {
		return fmt.Errorf("bad server-ip %q", a.cfg.ServerIP)
	}
	a.fwd = &relay.Forwarder{Listen: conn, Upstream: server, Allow: a.bans.Allowed, Intercept: sim.Answer,
		MaxSessions: a.cfg.MaxPlayers, MaxPPS: a.cfg.MaxPPS}
	a.log.Info("proxy listening", "public", conn.LocalAddr(), "server", server,
		"max_players", a.cfg.MaxPlayers, "max_pps", a.cfg.MaxPPS)
	wg.Add(1)
	go func() { defer wg.Done(); a.fwd.Run(ctx) }()
	return nil
}

func (a *agent) startSimulation(ctx context.Context, wg *sync.WaitGroup) error {
	gamePort, discPort := api.Ports()
	host, port, err := net.SplitHostPort(a.cfg.Listen)
	if err != nil {
		return fmt.Errorf("listen address: %w", err)
	}
	if os.Getenv("HICOMM_DEV_PORTS") != "" {
		port = strconv.Itoa(gamePort)
	}
	echo, err := net.ListenPacket("udp4", net.JoinHostPort(host, port))
	if err != nil {
		return fmt.Errorf("simulate: bind UDP %s:%s: %w (is a real server running?)", host, port, err)
	}
	// -loopback keeps the whole simulation on 127.0.0.1: no LAN broadcast and
	// no listening on network interfaces (useful for single-PC development).
	capAddr, txAddr, target := "0.0.0.0", "0.0.0.0", net.IPv4bcast
	if a.cfg.Loopback {
		capAddr, txAddr, target = "127.0.0.1", "127.0.0.1", net.IPv4(127, 0, 0, 1)
	}
	capConn, err := udpx.ListenShared("udp4", fmt.Sprintf("%s:%d", capAddr, discPort))
	if err != nil {
		return fmt.Errorf("simulate: listen %d: %w", discPort, err)
	}
	tx, err := udpx.ListenBroadcast("udp4", txAddr+":0")
	if err != nil {
		return err
	}
	a.setStatus("simulated")
	a.log.Info("simulation running", "echo", echo.LocalAddr(), "beacons_to", target,
		"note", "probes to the game port are answered; beacons are fake and ignored by the game")
	local := udpx.LocalIPv4s()
	wg.Add(3)
	go func() { defer wg.Done(); sim.Echo(ctx, echo.(*net.UDPConn)) }()
	go func() {
		defer wg.Done()
		beacon.Capture(ctx, capConn, func(src *net.UDPAddr) bool { return local[src.IP.String()] }, &a.beacons)
	}()
	go func() {
		defer wg.Done()
		defer tx.Close()
		sim.Beacons(ctx, tx, &net.UDPAddr{IP: target, Port: discPort}, api.BeaconInterval)
	}()
	return nil
}

func sleep(ctx context.Context, d time.Duration) {
	select {
	case <-ctx.Done():
	case <-time.After(d):
	}
}
