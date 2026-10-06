// Package portmap asks the home router to forward a UDP port to this PC,
// with UPnP IGD or, failing that, NAT-PMP. OpenLink Server uses it only when
// the host opts in ("auto_port_forward"): an open port is reachable by anyone
// on the internet.
package portmap

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"
)

// Mapping is a port forward the router accepted.
type Mapping interface {
	Method() string     // "UPnP" or "NAT-PMP"
	ExternalIP() string // the router's public address, "" if it did not say
	Lease() time.Duration
	Renew(ctx context.Context) error
	Remove(ctx context.Context) error
}

// Request describes the forward: UDP ExternalPort on the router to
// InternalIP:InternalPort.
type Request struct {
	ExternalPort, InternalPort int
	InternalIP                 net.IP
	Description                string
	Lease                      time.Duration // asked for; a router may grant another
}

// Map tries UPnP, then NAT-PMP. The error lists why each failed.
func Map(ctx context.Context, r Request) (Mapping, error) {
	if r.InternalIP == nil || r.InternalIP.To4() == nil {
		return nil, errors.New("no IPv4 address for this PC")
	}
	m, uerr := mapUPnP(ctx, r)
	if uerr == nil {
		return m, nil
	}
	m, perr := mapNATPMP(ctx, r)
	if perr == nil {
		return m, nil
	}
	return nil, fmt.Errorf("UPnP: %v; NAT-PMP: %v", uerr, perr)
}

// LocalIPv4 returns the address this PC uses to reach the internet (the
// default route's interface). No packet is sent.
func LocalIPv4() (net.IP, error) {
	c, err := net.Dial("udp4", "192.0.2.1:9") // TEST-NET-1; only selects a route
	if err != nil {
		return nil, err
	}
	defer c.Close()
	return c.LocalAddr().(*net.UDPAddr).IP.To4(), nil
}

// SharedAddress reports whether ip is not a public internet address: private,
// carrier-grade NAT (100.64.0.0/10) and similar. A router with such an
// external address is itself behind another NAT, so a forward on it does not
// make the server reachable.
func SharedAddress(ip net.IP) bool {
	if ip == nil {
		return false
	}
	_, cgnat, _ := net.ParseCIDR("100.64.0.0/10")
	return ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsUnspecified() || cgnat.Contains(ip)
}

func parseIP(s string) net.IP { return net.ParseIP(strings.TrimSpace(s)) }
