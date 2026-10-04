// Package udpx opens UDP sockets with the options the relay needs.
package udpx

import (
	"context"
	"net"
	"syscall"
)

// ListenShared binds a UDP socket with SO_REUSEADDR and SO_BROADCAST, so it can
// coexist with a socket the game already bound on the same port. Bind it only
// after the game's own socket exists: on Windows a later bind without
// SO_REUSEADDR (the game's) fails while ours is open.
func ListenShared(network, addr string) (*net.UDPConn, error) {
	lc := net.ListenConfig{Control: func(_, _ string, c syscall.RawConn) error {
		var serr error
		if err := c.Control(func(fd uintptr) { serr = setShared(fd) }); err != nil {
			return err
		}
		return serr
	}}
	pc, err := lc.ListenPacket(context.Background(), network, addr)
	if err != nil {
		return nil, err
	}
	return pc.(*net.UDPConn), nil
}

// ListenBroadcast binds a UDP socket that may send to broadcast addresses.
func ListenBroadcast(network, addr string) (*net.UDPConn, error) {
	lc := net.ListenConfig{Control: func(_, _ string, c syscall.RawConn) error {
		var serr error
		if err := c.Control(func(fd uintptr) { serr = setBroadcast(fd) }); err != nil {
			return err
		}
		return serr
	}}
	pc, err := lc.ListenPacket(context.Background(), network, addr)
	if err != nil {
		return nil, err
	}
	return pc.(*net.UDPConn), nil
}

// PrimaryIPv4 returns the local IPv4 address used for the default route.
// No packet is sent: connecting a UDP socket only selects a route.
func PrimaryIPv4() (net.IP, error) {
	c, err := net.Dial("udp4", "192.0.2.1:9") // TEST-NET-1, never contacted
	if err != nil {
		return nil, err
	}
	defer c.Close()
	return c.LocalAddr().(*net.UDPAddr).IP.To4(), nil
}

// LocalIPv4s lists the IPv4 addresses assigned to this machine, including loopback.
func LocalIPv4s() map[string]bool {
	out := map[string]bool{"127.0.0.1": true}
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return out
	}
	for _, a := range addrs {
		if n, ok := a.(*net.IPNet); ok && n.IP.To4() != nil {
			out[n.IP.To4().String()] = true
		}
	}
	return out
}
