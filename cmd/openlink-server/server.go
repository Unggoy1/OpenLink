package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os/exec"
	"slices"
	"strconv"
	"time"

	"halocommunity/internal/api"
	"halocommunity/internal/beacon"
	"halocommunity/internal/bootlog"
	"halocommunity/internal/directory"
	"halocommunity/internal/game"
	"halocommunity/internal/udpx"
)

// ---- server supervision --------------------------------------------------

// superviseServer starts the server bound to serverBind and restarts it when
// it exits. If one is already running (existing), it is watched instead.
func (a *agent) superviseServer(ctx context.Context, in game.Install, serverBind string, existing *game.Owner) {
	if existing != nil || !a.cfg.Manage {
		a.markUnmanagedControl()
		pid := 0
		if existing != nil {
			pid = existing.PID
			a.mu.Lock()
			a.pid = pid
			a.mu.Unlock()
			a.log.Warn("a server already owns UDP 1343: watching it without restarts", "pid", pid, "address", existing.IP)
			a.setStatus("ready") // the listing still needs fresh beacons to be joinable
		} else {
			a.log.Info("-manage=false: waiting for a manually started server")
		}
		a.followLog(ctx, in, pid)
		return
	}
	backoff := 5 * time.Second
	for ctx.Err() == nil {
		cmd := in.ServerCommand(a.cfg.Sandbox, serverBind)
		if err := cmd.Start(); err != nil {
			a.log.Error("server start failed", "err", err)
		} else {
			a.mu.Lock()
			a.pid = cmd.Process.Pid
			a.mu.Unlock()
			a.setStatus("starting")
			a.log.Info("server started", "pid", cmd.Process.Pid, "args", cmd.Args[1:])
			if a.onLaunch != nil {
				a.onLaunch(cmd.Process)
			}
			started := time.Now()
			a.waitServer(ctx, in, cmd, serverBind)
			if time.Since(started) > 10*time.Minute {
				backoff = 5 * time.Second // it ran fine for a while; restart quickly
			}
		}
		if ctx.Err() != nil {
			return
		}
		if !a.cfg.Restart {
			a.log.Info("single managed launch finished; restart disabled")
			return
		}
		a.log.Info("restarting server", "in", backoff)
		sleep(ctx, backoff)
		backoff = min(backoff*2, time.Minute)
	}
}

func (a *agent) waitServer(ctx context.Context, in game.Install, cmd *exec.Cmd, serverBind string) {
	logCtx, cancel := context.WithCancel(ctx)
	controlDone := make(chan struct{})
	go func() { defer close(controlDone); a.manageHostControl(logCtx, cmd) }()
	stopControl := func() { cancel(); <-controlDone }
	defer stopControl()
	go a.followLog(logCtx, in, cmd.Process.Pid)
	go a.checkGamePort(logCtx, cmd, serverBind)
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		a.setStatus("exited")
		a.log.Warn("server exited", "pid", cmd.Process.Pid, "err", err)
	case <-ctx.Done():
		// Complete bounded native cleanup while the owned target still exists.
		stopControl()
		if a.cfg.StopServer {
			a.log.Info("stopping server", "pid", cmd.Process.Pid)
			cmd.Process.Kill()
			<-done
		} else {
			a.log.Info("leaving server running", "pid", cmd.Process.Pid)
		}
	}
}

// checkGamePort stops a server that never binds UDP 1343 on its address. A
// second LAN server on the same IP still reaches setupComplete and advertises
// itself without a game socket, which would list an unjoinable server.
func (a *agent) checkGamePort(ctx context.Context, cmd *exec.Cmd, serverBind string) {
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
				a.log.Info("server owns UDP 1343", "pid", pid, "address", o.IP)
				if serverBind != "" && !o.Covers(serverBind) {
					a.log.Warn("server bound an unexpected address", "want", serverBind, "got", o.IP)
				}
				return
			}
		}
	}
	a.log.Error("server did not bind UDP 1343 within 90 s (another server on this address?); stopping it", "pid", pid)
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

// captureLoop listens for the server's beacons only while the server is ready,
// and releases the shared beacon port whenever the server is not ready or
// beacons stop, so a (re)starting server can always bind it.
func (a *agent) captureLoop(ctx context.Context) {
	local := udpx.LocalIPv4s()
	accept := func(src *net.UDPAddr) bool { return local[src.IP.String()] }
	for ctx.Err() == nil {
		if a.getStatus() != "ready" {
			sleep(ctx, time.Second)
			continue
		}
		sleep(ctx, a.cfg.CaptureDelay)
		if ctx.Err() != nil {
			return
		}
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

func (a *agent) players() int {
	if a.fwd == nil {
		return -1
	}
	return a.fwd.Active(playerWindow)
}

func (a *agent) directoryLoop(ctx context.Context) {
	dc := directory.NewClient(a.cfg.Directory, a.cfg.RegisterKey)
	var reg api.RegisterResponse
	backoff := 5 * time.Second
	var lastCheck time.Time
	for ctx.Err() == nil {
		if reg.ID == "" {
			r, err := dc.Register(ctx, api.RegisterRequest{Name: a.cfg.Name, Host: a.cfg.PublicHost,
				Port: a.cfg.PublicPort, Build: a.build, Region: a.cfg.Region})
			if err != nil {
				a.log.Warn("directory registration failed", "err", err, "retry_in", backoff)
				sleep(ctx, backoff)
				backoff = min(backoff*2, time.Minute)
				continue
			}
			reg, backoff = r, 5*time.Second
			a.mu.Lock()
			a.listingID, a.reachability, a.listed, a.waitNoted = reg.ID, api.ReachUnknown, false, false
			a.mu.Unlock()
			a.log.Info("listed in directory", "id", reg.ID, "endpoint", net.JoinHostPort(reg.Host, strconv.Itoa(a.cfg.PublicPort)))
		}
		hb := api.Heartbeat{Status: a.getStatus(), Players: a.players(), Proxy: a.fwd != nil || a.cfg.Simulate, Match: a.match()}
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
		if hb.Proxy && time.Since(lastCheck) > 30*time.Second {
			lastCheck = time.Now()
			a.updateReachability(ctx, dc, reg.ID, reg.Token)
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

// updateReachability reads the directory's probe result for our listing and
// logs it when it changes: the built-in "is my server reachable?" check. The
// directory shows a server to players only once its port has answered.
func (a *agent) updateReachability(ctx context.Context, dc *directory.Client, id, token string) {
	s, err := dc.Self(ctx, id, token)
	if err != nil {
		// An older directory has no self view; find the listing in the public list.
		list, lerr := dc.List(ctx, "")
		if lerr != nil {
			return
		}
		i := slices.IndexFunc(list, func(s api.ServerInfo) bool { return s.ID == id })
		if i < 0 {
			return
		}
		s, s.Listed = list[i], true
	}
	endpoint := net.JoinHostPort(s.Host, strconv.Itoa(s.Port))
	a.mu.Lock()
	changed := a.reachability != s.Reachability
	nowListed := s.Listed && !a.listed
	waiting := !s.Listed && s.Reachability == api.ReachUnknown && !a.waitNoted
	a.reachability, a.listed = s.Reachability, s.Listed
	a.waitNoted = a.waitNoted || waiting
	a.mu.Unlock()
	if waiting {
		a.log.Info("waiting for the directory to reach your server; players see it once it answers", "endpoint", endpoint)
	}
	if changed {
		switch s.Reachability {
		case api.ReachOK:
			a.log.Info("the directory reached your server from the internet", "endpoint", endpoint)
		case api.ReachUnreachable:
			a.log.Warn("the directory could NOT reach your server: check the UDP port forward and firewall. "+
				"Players will not see it until it answers", "endpoint", endpoint)
		}
	}
	if nowListed {
		a.log.Info("your server is now in the server list")
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
		a.log.Info("status", "server", a.getStatus(), "players", a.players(), "beacons_captured", n, "last_beacon_age", age)
	}
}
