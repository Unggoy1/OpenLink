// Package relay forwards the local game's UDP traffic to a remote server and
// back, one upstream socket per local client endpoint. Datagrams are never
// inspected or changed.
package relay

import (
	"context"
	"errors"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

// Stats are cumulative counters; read them with Snapshot.
type Stats struct {
	UpPackets, UpBytes, DownPackets, DownBytes atomic.Uint64
	LastUp, LastDown                           atomic.Int64 // unix nanoseconds
	Sessions                                   atomic.Int32
}

// Snapshot is a plain copy of Stats.
type Snapshot struct {
	UpPackets, UpBytes, DownPackets, DownBytes uint64
	LastUp, LastDown                           time.Time
	Sessions                                   int32
}

// Snapshot copies the counters.
func (s *Stats) Snapshot() Snapshot {
	ts := func(v int64) time.Time {
		if v == 0 {
			return time.Time{}
		}
		return time.Unix(0, v)
	}
	return Snapshot{s.UpPackets.Load(), s.UpBytes.Load(), s.DownPackets.Load(), s.DownBytes.Load(),
		ts(s.LastUp.Load()), ts(s.LastDown.Load()), s.Sessions.Load()}
}

// Forwarder relays between Listen (where the game sends) and Upstream (the server).
type Forwarder struct {
	Listen   *net.UDPConn
	Upstream *net.UDPAddr
	Idle     time.Duration // drop a client session after this long without traffic
	Stats    Stats

	mu       sync.Mutex
	sessions map[string]*session
}

type session struct {
	up     *net.UDPConn
	client *net.UDPAddr
	last   atomic.Int64
}

// Run forwards until ctx ends.
func (f *Forwarder) Run(ctx context.Context) error {
	if f.Idle == 0 {
		f.Idle = 2 * time.Minute
	}
	f.mu.Lock()
	f.sessions = map[string]*session{}
	f.mu.Unlock()
	go func() { <-ctx.Done(); f.Listen.Close() }()
	go f.reap(ctx)
	defer f.closeAll()

	buf := make([]byte, 2048)
	for {
		n, client, err := f.Listen.ReadFromUDP(buf)
		if err != nil {
			if ctx.Err() != nil || errors.Is(err, net.ErrClosed) {
				return nil
			}
			continue
		}
		s, err := f.session(ctx, client)
		if err != nil {
			continue
		}
		if _, err := s.up.WriteToUDP(buf[:n], f.Upstream); err == nil {
			now := time.Now().UnixNano()
			s.last.Store(now)
			f.Stats.UpPackets.Add(1)
			f.Stats.UpBytes.Add(uint64(n))
			f.Stats.LastUp.Store(now)
		}
	}
}

func (f *Forwarder) session(ctx context.Context, client *net.UDPAddr) (*session, error) {
	key := client.String()
	f.mu.Lock()
	defer f.mu.Unlock()
	if s, ok := f.sessions[key]; ok {
		return s, nil
	}
	network := "udp4"
	if f.Upstream.IP.To4() == nil {
		network = "udp6"
	}
	up, err := net.ListenUDP(network, nil)
	if err != nil {
		return nil, err
	}
	s := &session{up: up, client: client}
	s.last.Store(time.Now().UnixNano())
	f.sessions[key] = s
	f.Stats.Sessions.Add(1)
	go f.downstream(ctx, s)
	return s, nil
}

// downstream copies server replies back to the game.
func (f *Forwarder) downstream(ctx context.Context, s *session) {
	buf := make([]byte, 2048)
	for {
		n, from, err := s.up.ReadFromUDP(buf)
		if err != nil {
			if ctx.Err() != nil || errors.Is(err, net.ErrClosed) {
				return
			}
			continue
		}
		if !from.IP.Equal(f.Upstream.IP) || from.Port != f.Upstream.Port {
			continue // only the server may answer through this session
		}
		if _, err := f.Listen.WriteToUDP(buf[:n], s.client); err == nil {
			now := time.Now().UnixNano()
			s.last.Store(now)
			f.Stats.DownPackets.Add(1)
			f.Stats.DownBytes.Add(uint64(n))
			f.Stats.LastDown.Store(now)
		}
	}
}

func (f *Forwarder) reap(ctx context.Context) {
	t := time.NewTicker(10 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		cutoff := time.Now().Add(-f.Idle).UnixNano()
		f.mu.Lock()
		for k, s := range f.sessions {
			if s.last.Load() < cutoff {
				s.up.Close()
				delete(f.sessions, k)
				f.Stats.Sessions.Add(-1)
			}
		}
		f.mu.Unlock()
	}
}

func (f *Forwarder) closeAll() {
	f.mu.Lock()
	defer f.mu.Unlock()
	for k, s := range f.sessions {
		s.up.Close()
		delete(f.sessions, k)
	}
	f.Stats.Sessions.Store(0)
}
