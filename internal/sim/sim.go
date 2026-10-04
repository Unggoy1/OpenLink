// Package sim provides a stand-in for the game server so the network path
// (port forwarding, directory, connector) can be tested without the game.
// Its datagrams carry plain-text prefixes so they can never be mistaken for
// game traffic; a real game client ignores them.
package sim

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"net"
	"time"
)

const (
	// ProbePrefix marks probe datagrams that the simulated server echoes.
	ProbePrefix = "HICOMM-PROBE "
	// BeaconPrefix marks simulated beacons.
	BeaconPrefix = "HICOMM-SIMBEACON "
)

// Echo answers probe datagrams on conn until ctx ends. Anything else is dropped.
func Echo(ctx context.Context, conn *net.UDPConn) {
	go func() { <-ctx.Done(); conn.Close() }()
	buf := make([]byte, 2048)
	for {
		n, from, err := conn.ReadFromUDP(buf)
		if err != nil {
			if ctx.Err() != nil || errors.Is(err, net.ErrClosed) {
				return
			}
			continue
		}
		if n <= 256 && bytes.HasPrefix(buf[:n], []byte(ProbePrefix)) {
			conn.WriteToUDP(buf[:n], from)
		}
	}
}

// Beacon returns a simulated beacon: the prefix plus random bytes, sized like
// the retail beacon (79 bytes) so relays see realistic sizes.
func Beacon() []byte {
	b := make([]byte, 79)
	copy(b, BeaconPrefix)
	rand.Read(b[len(BeaconPrefix):])
	return b
}

// Beacons sends a fresh simulated beacon to target every interval until ctx ends.
func Beacons(ctx context.Context, conn *net.UDPConn, target *net.UDPAddr, interval time.Duration) {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		conn.WriteToUDP(Beacon(), target)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// ProbeResult summarises a probe run.
type ProbeResult struct {
	Sent, Received int
	RTTs           []time.Duration
}

// Probe sends n probes to server and collects echoes, waiting up to timeout per probe.
func Probe(ctx context.Context, server *net.UDPAddr, n int, timeout time.Duration) (ProbeResult, error) {
	network := "udp4"
	if server.IP.To4() == nil {
		network = "udp6"
	}
	conn, err := net.ListenUDP(network, nil)
	if err != nil {
		return ProbeResult{}, err
	}
	defer conn.Close()
	var res ProbeResult
	buf := make([]byte, 512)
	for i := 0; i < n && ctx.Err() == nil; i++ {
		msg := []byte(fmt.Sprintf("%s%d %d", ProbePrefix, i, time.Now().UnixNano()))
		start := time.Now()
		if _, err := conn.WriteToUDP(msg, server); err != nil {
			return res, err
		}
		res.Sent++
		conn.SetReadDeadline(start.Add(timeout))
		for {
			m, from, err := conn.ReadFromUDP(buf)
			if err != nil {
				break // timeout: count as lost
			}
			if from.IP.Equal(server.IP) && bytes.Equal(buf[:m], msg) {
				res.Received++
				res.RTTs = append(res.RTTs, time.Since(start))
				break
			}
		}
	}
	return res, nil
}
