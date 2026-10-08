// Package beacon captures the retail server's LAN beacons on the host and
// re-advertises them on a player's machine. Beacons are encrypted by the game
// and are always handled as opaque bytes.
package beacon

import (
	"context"
	"errors"
	"net"
	"sync"
	"time"

	"halocommunity/internal/api"
)

// Store keeps the most recent beacon.
type Store struct {
	mu     sync.Mutex
	latest []byte
	at     time.Time
	count  uint64
}

// Put records a beacon observed (or fetched) at time at.
func (s *Store) Put(b []byte, at time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.latest = append(s.latest[:0], b...)
	s.at = at
	s.count++
}

// Latest returns a copy of the newest beacon, when it was seen, and the total count.
func (s *Store) Latest() ([]byte, time.Time, uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.latest == nil {
		return nil, time.Time{}, s.count
	}
	return append([]byte(nil), s.latest...), s.at, s.count
}

// Valid reports whether a datagram can be a beacon: at least the 16-byte
// envelope (nonce + header) and not larger than the stored limit.
func Valid(b []byte) bool { return len(b) >= 16 && len(b) <= api.MaxBeaconBytes }

// Capture reads datagrams from conn and stores those accepted by accept (given
// the source and the datagram) until ctx ends or conn is closed.
func Capture(ctx context.Context, conn *net.UDPConn, accept func(*net.UDPAddr, []byte) bool, st *Store) error {
	go func() { <-ctx.Done(); conn.Close() }()
	buf := make([]byte, 2048)
	for {
		n, src, err := conn.ReadFromUDP(buf)
		if err != nil {
			if ctx.Err() != nil || errors.Is(err, net.ErrClosed) {
				return nil
			}
			continue // transient (e.g. Windows ICMP reset); keep reading
		}
		if Valid(buf[:n]) && accept(src, buf[:n]) {
			st.Put(buf[:n], time.Now())
		}
	}
}

// Advertise sends the newest beacon from src to target every interval while it
// is younger than maxAge. It returns when ctx ends.
func Advertise(ctx context.Context, conn *net.UDPConn, target *net.UDPAddr, src *Store, interval, maxAge time.Duration, sent func()) {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		if b, at, _ := src.Latest(); b != nil && time.Since(at) < maxAge {
			if _, err := conn.WriteToUDP(b, target); err == nil && sent != nil {
				sent()
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}
