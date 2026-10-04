package main

import (
	"encoding/json"
	"errors"
	"net"
	"os"
	"sync"
	"time"
)

// banList holds banned player IPs, persisted as JSON {ip: expiry}. A zero
// expiry is permanent.
type banList struct {
	path string
	mu   sync.Mutex
	bans map[string]time.Time
}

func loadBans(path string) (*banList, error) {
	b := &banList{path: path, bans: map[string]time.Time{}}
	if path == "" {
		return b, nil
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return b, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, &b.bans); err != nil {
		return nil, err
	}
	return b, nil
}

// Allowed reports whether ip may connect; it is called for every datagram.
func (b *banList) Allowed(ip net.IP) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.bans) == 0 {
		return true
	}
	until, banned := b.bans[ip.String()]
	if !banned {
		return true
	}
	if !until.IsZero() && time.Now().After(until) {
		delete(b.bans, ip.String())
		return true
	}
	return false
}

// Ban bans ip for d (0 = permanent).
func (b *banList) Ban(ip net.IP, d time.Duration) error {
	var until time.Time
	if d > 0 {
		until = time.Now().Add(d)
	}
	b.mu.Lock()
	b.bans[ip.String()] = until
	b.mu.Unlock()
	return b.save()
}

// Unban removes ip; it reports whether it was banned.
func (b *banList) Unban(ip net.IP) (bool, error) {
	b.mu.Lock()
	_, was := b.bans[ip.String()]
	delete(b.bans, ip.String())
	b.mu.Unlock()
	return was, b.save()
}

// List returns current bans (expired ones are dropped).
func (b *banList) List() map[string]time.Time {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := map[string]time.Time{}
	for ip, until := range b.bans {
		if until.IsZero() || time.Now().Before(until) {
			out[ip] = until
		}
	}
	return out
}

func (b *banList) save() error {
	if b.path == "" {
		return nil
	}
	data, err := json.MarshalIndent(b.List(), "", "  ")
	if err != nil {
		return err
	}
	tmp := b.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, b.path)
}
