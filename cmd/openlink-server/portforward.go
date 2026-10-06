package main

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"time"

	"halocommunity/internal/portmap"
)

// portMapper is portmap.Map; a variable for tests.
var portMapper = portmap.Map

// runPortForward, with auto_port_forward, asks the router to forward the
// public port to the proxy while the agent runs, renews the lease and removes
// the forward on exit. Failure only logs: the host can still forward by hand.
func (a *agent) runPortForward(ctx context.Context) {
	setState := func(s string) { a.mu.Lock(); a.portForward = s; a.mu.Unlock() }
	host, portText, err := net.SplitHostPort(a.cfg.Listen)
	internalPort, perr := strconv.Atoi(portText)
	if err != nil || perr != nil {
		setState("failed: bad listen address")
		return
	}
	ip := net.ParseIP(host).To4()
	if ip == nil || ip.IsUnspecified() || ip.IsLoopback() {
		if ip, err = portmap.LocalIPv4(); err != nil {
			setState("failed: no LAN address")
			a.log.Warn("automatic port forwarding: cannot find this PC's LAN address; forward the port by hand", "err", err)
			return
		}
	}
	ext := a.cfg.PublicPort
	a.log.Warn("auto_port_forward is on: asking your router to forward a port to this PC. Anyone on the internet can then send traffic to it while OpenLink Server runs",
		"port", fmt.Sprintf("UDP %d -> %s:%d", ext, ip, internalPort))
	req := portmap.Request{ExternalPort: ext, InternalPort: internalPort, InternalIP: ip, Description: "OpenLink Server", Lease: time.Hour}
	mctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	m, err := portMapper(mctx, req)
	cancel()
	if err != nil {
		setState("failed: " + err.Error())
		a.log.Warn("automatic port forwarding failed; forward the port in your router by hand (see HOSTING.md)",
			"port", fmt.Sprintf("UDP %d -> %s:%d", ext, ip, internalPort), "err", err)
		return
	}
	setState(fmt.Sprintf("on (%s, UDP %d -> %s:%d)", m.Method(), ext, ip, internalPort))
	a.log.Info("router forwards the port", "method", m.Method(), "port", fmt.Sprintf("UDP %d -> %s:%d", ext, ip, internalPort),
		"router_address", m.ExternalIP(), "lease", m.Lease())
	if e := net.ParseIP(m.ExternalIP()); portmap.SharedAddress(e) {
		a.log.Warn("your router's internet address is not public (carrier-grade NAT or a second router), so players outside cannot reach this server even with the forward; ask your internet provider for a public IPv4 address",
			"router_address", m.ExternalIP())
	}
	defer func() {
		rctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := m.Remove(rctx); err != nil {
			a.log.Warn("could not remove the router's port forward; remove it in the router if you no longer host", "err", err)
		} else {
			a.log.Info("router port forward removed")
		}
	}()
	renew := m.Lease() / 2
	if renew <= 0 {
		<-ctx.Done() // permanent mapping: nothing to renew
		return
	}
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(renew):
		}
		rctx, cancel := context.WithTimeout(ctx, 20*time.Second)
		err := m.Renew(rctx)
		cancel()
		if err != nil && ctx.Err() == nil {
			setState("renew failed: " + err.Error())
			a.log.Warn("could not renew the router's port forward; retrying in a minute", "err", err)
			renew = time.Minute
			continue
		}
		renew = m.Lease() / 2
	}
}
