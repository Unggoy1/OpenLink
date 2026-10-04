// hi-hostagent runs next to a retail LAN server. It starts and watches the
// server, captures the server's LAN beacons and keeps the directory listing
// fresh. With -simulate it stands in for the game so the network path can be
// tested without it.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"sync"
	"time"

	"halocommunity/internal/api"
	"halocommunity/internal/beacon"
	"halocommunity/internal/bootlog"
	"halocommunity/internal/directory"
	"halocommunity/internal/game"
	"halocommunity/internal/sim"
	"halocommunity/internal/udpx"
)

type config struct {
	directory, registerKey, name, region, publicHost string
	publicPort                                       int
	install, bindIP, sandbox                         string
	manage, stopServer, simulate, loopback           bool
	captureDelay                                     time.Duration
}

type agent struct {
	cfg     config
	log     *slog.Logger
	build   string
	beacons beacon.Store

	mu     sync.Mutex
	status string // starting, ready, exited, simulated
	pid    int
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
	var c config
	flag.StringVar(&c.directory, "directory", os.Getenv("HICOMM_DIRECTORY"), "directory URL (env HICOMM_DIRECTORY); empty = do not list")
	flag.StringVar(&c.registerKey, "register-key", os.Getenv("HICOMM_REGISTER_KEY"), "directory registration key, if the directory requires one")
	host, _ := os.Hostname()
	flag.StringVar(&c.name, "name", host, "server name shown in the browser")
	flag.StringVar(&c.region, "region", "", "region label, e.g. us-west")
	flag.StringVar(&c.publicHost, "public-host", "", "address players connect to; empty = the directory uses this machine's public IP")
	flag.IntVar(&c.publicPort, "public-port", api.GamePort, "external UDP port forwarded to the server's 1343")
	flag.StringVar(&c.install, "install", "", "game install root (folder with version.txt); empty = search common locations")
	flag.StringVar(&c.bindIP, "bind-ip", "", "pass -bindip to the server (one server per IP)")
	flag.StringVar(&c.sandbox, "sandbox", "RETAIL", "value for -lan_sandbox")
	flag.BoolVar(&c.manage, "manage", true, "start the server and restart it if it exits")
	flag.BoolVar(&c.stopServer, "stop-server", false, "kill the managed server when the agent exits")
	flag.BoolVar(&c.simulate, "simulate", false, "no game: send simulated beacons and echo probes on UDP 1343")
	flag.BoolVar(&c.loopback, "loopback", false, "with -simulate: keep simulated beacons on 127.0.0.1")
	flag.DurationVar(&c.captureDelay, "capture-delay", 5*time.Second, "wait after setupComplete before listening for beacons")
	flag.Parse()

	a := &agent{cfg: c, log: slog.New(slog.NewTextHandler(os.Stderr, nil))}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	if err := a.run(ctx); err != nil {
		a.log.Error("agent stopped", "err", err)
		os.Exit(1)
	}
}

func (a *agent) run(ctx context.Context) error {
	var wg sync.WaitGroup
	if a.cfg.simulate {
		a.build = "simulated"
		if in, err := game.FindInstall(a.cfg.install); err == nil {
			if b, err := in.Build(); err == nil {
				a.build = b // lets connectors on the same build list the simulated server
			}
		}
		if err := a.startSimulation(ctx, &wg); err != nil {
			return err
		}
	} else {
		in, err := game.FindInstall(a.cfg.install)
		if err != nil {
			return err
		}
		if a.build, err = in.Build(); err != nil {
			return fmt.Errorf("read build: %w", err)
		}
		a.log.Info("game install", "root", in.Root, "build", a.build)
		wg.Add(2)
		go func() { defer wg.Done(); a.superviseServer(ctx, in) }()
		go func() { defer wg.Done(); a.captureLoop(ctx) }()
	}
	if a.cfg.directory != "" {
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

// ---- simulation ----------------------------------------------------------

func (a *agent) startSimulation(ctx context.Context, wg *sync.WaitGroup) error {
	bind := a.cfg.bindIP
	if bind == "" {
		bind = "0.0.0.0"
	}
	gamePort, discPort := api.Ports()
	echo, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.ParseIP(bind), Port: gamePort})
	if err != nil {
		return fmt.Errorf("simulate: bind UDP %s:%d: %w (is a real server running?)", bind, gamePort, err)
	}
	// -loopback keeps the whole simulation on 127.0.0.1: no LAN broadcast and
	// no listening on network interfaces (useful for single-PC development).
	capAddr, txAddr, target := "0.0.0.0", "0.0.0.0", net.IPv4bcast
	if a.cfg.loopback {
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
		"note", "probes to the echo port are answered; beacons are fake and ignored by the game")
	local := udpx.LocalIPv4s()
	wg.Add(3)
	go func() { defer wg.Done(); sim.Echo(ctx, echo) }()
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

// ---- server supervision --------------------------------------------------

func (a *agent) superviseServer(ctx context.Context, in game.Install) {
	all, err := game.UDPPortOwners(api.GamePort)
	if err != nil {
		a.log.Warn("cannot read the UDP port table", "err", err)
	}
	var owners []int // processes already serving 1343 on our address
	for _, o := range all {
		if o.Covers(a.cfg.bindIP) {
			owners = append(owners, o.PID)
		}
	}
	if len(owners) > 0 || !a.cfg.manage {
		pid := 0
		if len(owners) > 0 {
			pid = owners[0]
			a.log.Warn("UDP 1343 is already owned by another process: watching it without restarts", "pid", pid)
			a.setStatus("ready") // the listing still needs fresh beacons to be joinable
		} else {
			a.log.Info("-manage=false: waiting for a manually started server")
		}
		a.followLog(ctx, in, pid)
		return
	}
	backoff := 5 * time.Second
	for ctx.Err() == nil {
		cmd := in.ServerCommand(a.cfg.sandbox, a.cfg.bindIP)
		if err := cmd.Start(); err != nil {
			a.log.Error("server start failed", "err", err)
		} else {
			a.mu.Lock()
			a.pid = cmd.Process.Pid
			a.mu.Unlock()
			a.setStatus("starting")
			a.log.Info("server started", "pid", cmd.Process.Pid, "args", cmd.Args[1:])
			started := time.Now()
			a.waitServer(ctx, in, cmd)
			if time.Since(started) > 10*time.Minute {
				backoff = 5 * time.Second // it ran fine for a while; restart quickly
			}
		}
		if ctx.Err() != nil {
			return
		}
		a.log.Info("restarting server", "in", backoff)
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		backoff = min(backoff*2, time.Minute)
	}
}

func (a *agent) waitServer(ctx context.Context, in game.Install, cmd *exec.Cmd) {
	logCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	go a.followLog(logCtx, in, cmd.Process.Pid)
	go a.checkGamePort(logCtx, cmd)
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		a.setStatus("exited")
		a.log.Warn("server exited", "pid", cmd.Process.Pid, "err", err)
	case <-ctx.Done():
		if a.cfg.stopServer {
			a.log.Info("stopping server", "pid", cmd.Process.Pid)
			cmd.Process.Kill()
			<-done
		} else {
			a.log.Info("leaving server running", "pid", cmd.Process.Pid)
		}
	}
}

// checkGamePort stops a server that never binds UDP 1343. A second LAN server
// on the same IP still reaches setupComplete and advertises itself without a
// game socket, which would list an unjoinable server.
func (a *agent) checkGamePort(ctx context.Context, cmd *exec.Cmd) {
	pid := cmd.Process.Pid
	deadline := time.Now().Add(90 * time.Second)
	for time.Now().Before(deadline) {
		sleep(ctx, 2*time.Second)
		if ctx.Err() != nil {
			return
		}
		owners, err := game.UDPPortOwners(api.GamePort)
		if err != nil {
			a.log.Warn("cannot verify the game port", "err", err)
			return
		}
		for _, o := range owners {
			if o.PID == pid {
				a.log.Info("server owns UDP 1343", "pid", pid)
				return
			}
		}
	}
	a.log.Error("server did not bind UDP 1343 within 90 s (another server on this IP?); stopping it", "pid", pid)
	cmd.Process.Kill()
}

// followLog turns BootstrapLog state transitions of pid (0 = any) into status.
func (a *agent) followLog(ctx context.Context, in game.Install, pid int) {
	events := make(chan bootlog.Event, 32)
	stop := make(chan struct{})
	go bootlog.Follower{Path: in.BootstrapLog(), PID: pid}.Run(stop, events)
	defer close(stop)
	for {
		select {
		case <-ctx.Done():
			return
		case ev := <-events:
			if ev.State == "" {
				continue
			}
			a.log.Info("server state", "pid", ev.PID, "state", ev.State)
			switch ev.State {
			case "setupComplete":
				a.setStatus("ready")
			case "errorAndMoveToTeardown":
				a.setStatus("starting")
			}
		}
	}
}

// ---- beacon capture ------------------------------------------------------

// captureLoop listens for the server's beacons only while the server is ready.
// Our socket shares UDP 7117 with the server's; holding it while the server
// (re)binds would block the server, so it is released whenever the server is
// not ready or beacons stop arriving.
func (a *agent) captureLoop(ctx context.Context) {
	local := udpx.LocalIPv4s()
	accept := func(src *net.UDPAddr) bool { return local[src.IP.String()] }
	for ctx.Err() == nil {
		if a.getStatus() != "ready" {
			sleep(ctx, time.Second)
			continue
		}
		sleep(ctx, a.cfg.captureDelay)
		conn, err := udpx.ListenShared("udp4", fmt.Sprintf("0.0.0.0:%d", api.DiscoveryPort))
		if err != nil {
			a.log.Warn("cannot listen for beacons; retrying", "err", err)
			sleep(ctx, 10*time.Second)
			continue
		}
		a.log.Info("listening for server beacons", "port", api.DiscoveryPort)
		capCtx, cancel := context.WithCancel(ctx)
		go beacon.Capture(capCtx, conn, accept, &a.beacons)
		opened := time.Now()
		for capCtx.Err() == nil {
			sleep(capCtx, 2*time.Second)
			_, at, _ := a.beacons.Latest()
			last := at
			if last.Before(opened) {
				last = opened
			}
			switch {
			case a.getStatus() != "ready":
				a.log.Info("server not ready: releasing beacon port")
				cancel()
			case time.Since(last) > 20*time.Second:
				a.log.Warn("no server beacons for 20 s: releasing beacon port for 30 s in case it blocks the server")
				cancel()
				sleep(ctx, 30*time.Second)
			}
		}
		cancel()
	}
}

// ---- directory -----------------------------------------------------------

func (a *agent) directoryLoop(ctx context.Context) {
	dc := directory.NewClient(a.cfg.directory, a.cfg.registerKey)
	var reg api.RegisterResponse
	backoff := 5 * time.Second
	for ctx.Err() == nil {
		if reg.ID == "" {
			r, err := dc.Register(ctx, api.RegisterRequest{Name: a.cfg.name, Host: a.cfg.publicHost,
				Port: a.cfg.publicPort, Build: a.build, Region: a.cfg.region})
			if err != nil {
				a.log.Warn("directory registration failed", "err", err, "retry_in", backoff)
				sleep(ctx, backoff)
				backoff = min(backoff*2, time.Minute)
				continue
			}
			reg, backoff = r, 5*time.Second
			a.log.Info("listed in directory", "id", reg.ID, "endpoint", net.JoinHostPort(reg.Host, strconv.Itoa(a.cfg.publicPort)))
		}
		hb := api.Heartbeat{Status: a.getStatus(), Players: -1}
		if b, at, _ := a.beacons.Latest(); b != nil {
			hb.Beacon, hb.BeaconAgeMS = b, time.Since(at).Milliseconds()
		}
		if err := dc.Heartbeat(ctx, reg.ID, reg.Token, hb); err != nil {
			var se *directory.StatusError
			if errors.As(err, &se) && (se.Code == 404 || se.Code == 401) {
				a.log.Warn("listing expired; registering again")
				reg = api.RegisterResponse{}
				continue
			}
			a.log.Warn("heartbeat failed", "err", err)
		}
		sleep(ctx, 2*time.Second)
	}
	if reg.ID != "" {
		shut, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := dc.Unregister(shut, reg.ID, reg.Token); err == nil {
			a.log.Info("removed from directory")
		}
	}
}

func (a *agent) report(ctx context.Context) {
	for ctx.Err() == nil {
		sleep(ctx, 30*time.Second)
		_, at, n := a.beacons.Latest()
		age := "none"
		if n > 0 {
			age = time.Since(at).Round(time.Second).String()
		}
		a.log.Info("status", "server", a.getStatus(), "beacons_captured", n, "last_beacon_age", age)
	}
}

func sleep(ctx context.Context, d time.Duration) {
	select {
	case <-ctx.Done():
	case <-time.After(d):
	}
}
