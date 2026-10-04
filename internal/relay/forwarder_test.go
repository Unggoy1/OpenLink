package relay

import (
	"bytes"
	"context"
	"net"
	"sync/atomic"
	"testing"
	"time"
)

func loop(t *testing.T) *net.UDPConn {
	c, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// echoServer stands in for the game server: it echoes with a prefix.
func echoServer(t *testing.T) *net.UDPConn {
	server := loop(t)
	go func() {
		buf := make([]byte, 2048)
		for {
			n, a, err := server.ReadFromUDP(buf)
			if err != nil {
				return
			}
			server.WriteToUDP(append([]byte("echo:"), buf[:n]...), a)
		}
	}()
	return server
}

func TestForwardRoundTripTwoClients(t *testing.T) {
	server := echoServer(t)
	defer server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	f := &Forwarder{Listen: loop(t), Upstream: server.LocalAddr().(*net.UDPAddr)}
	go f.Run(ctx)

	for _, name := range []string{"clientA", "clientB"} {
		c := loop(t)
		defer c.Close()
		payload := bytes.Repeat([]byte(name), 100)
		c.WriteToUDP(payload, f.Listen.LocalAddr().(*net.UDPAddr))
		c.SetReadDeadline(time.Now().Add(2 * time.Second))
		buf := make([]byte, 2048)
		n, _, err := c.ReadFromUDP(buf)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if !bytes.Equal(buf[:n], append([]byte("echo:"), payload...)) {
			t.Fatalf("%s: wrong reply", name)
		}
	}
	s := f.Stats.Snapshot()
	if s.UpPackets != 2 || s.DownPackets != 2 || s.Sessions != 2 {
		t.Fatalf("stats %+v", s)
	}
}

func TestIgnoresStrangerReplies(t *testing.T) {
	server := loop(t)
	defer server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	f := &Forwarder{Listen: loop(t), Upstream: server.LocalAddr().(*net.UDPAddr)}
	go f.Run(ctx)

	c := loop(t)
	defer c.Close()
	c.WriteToUDP([]byte("hello"), f.Listen.LocalAddr().(*net.UDPAddr))
	buf := make([]byte, 64)
	server.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, upAddr, err := server.ReadFromUDP(buf)
	if err != nil {
		t.Fatal(err)
	}
	stranger := loop(t)
	defer stranger.Close()
	stranger.WriteToUDP([]byte("spoof"), upAddr) // not from the server address
	c.SetReadDeadline(time.Now().Add(300 * time.Millisecond))
	if _, _, err := c.ReadFromUDP(buf); err == nil {
		t.Fatal("stranger datagram was forwarded to the game")
	}
}

func send(t *testing.T, c *net.UDPConn, to *net.UDPAddr, msg string) (string, bool) {
	t.Helper()
	c.WriteToUDP([]byte(msg), to)
	c.SetReadDeadline(time.Now().Add(300 * time.Millisecond))
	buf := make([]byte, 2048)
	n, _, err := c.ReadFromUDP(buf)
	if err != nil {
		return "", false
	}
	return string(buf[:n]), true
}

func TestHostControls(t *testing.T) {
	server := echoServer(t)
	defer server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var banned atomic.Bool
	f := &Forwarder{Listen: loop(t), Upstream: server.LocalAddr().(*net.UDPAddr), MaxSessions: 1,
		Allow: func(net.IP) bool { return !banned.Load() },
		Intercept: func(b []byte) ([]byte, bool) {
			if string(b) == "probe" {
				return []byte("pong"), true
			}
			return nil, false
		}}
	go f.Run(ctx)
	to := f.Listen.LocalAddr().(*net.UDPAddr)

	a, b := loop(t), loop(t)
	defer a.Close()
	defer b.Close()
	if r, ok := send(t, a, to, "probe"); !ok || r != "pong" {
		t.Fatalf("intercept: %q %v", r, ok)
	}
	if f.Stats.Snapshot().UpPackets != 0 {
		t.Fatal("intercepted probe was forwarded")
	}
	if r, ok := send(t, a, to, "hi"); !ok || r != "echo:hi" {
		t.Fatalf("forward: %q %v", r, ok)
	}
	if _, ok := send(t, b, to, "hi"); ok {
		t.Fatal("second client admitted past MaxSessions=1")
	}
	if f.Active(time.Minute) != 1 || len(f.Sessions()) != 1 {
		t.Fatalf("active %d sessions %+v", f.Active(time.Minute), f.Sessions())
	}
	banned.Store(true)
	if _, ok := send(t, a, to, "hi"); ok {
		t.Fatal("banned client forwarded")
	}
	if f.Drop(net.IPv4(127, 0, 0, 1)) != 1 || len(f.Sessions()) != 0 {
		t.Fatal("drop did not remove the session")
	}
	if f.Stats.Snapshot().Dropped < 2 {
		t.Fatalf("dropped counter %d", f.Stats.Snapshot().Dropped)
	}
}

func TestRateLimit(t *testing.T) {
	server := loop(t) // counts what arrives
	defer server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	f := &Forwarder{Listen: loop(t), Upstream: server.LocalAddr().(*net.UDPAddr), MaxPPS: 20}
	go f.Run(ctx)
	c := loop(t)
	defer c.Close()
	for i := 0; i < 200; i++ { // a burst far above 20/s
		c.WriteToUDP([]byte("x"), f.Listen.LocalAddr().(*net.UDPAddr))
	}
	time.Sleep(300 * time.Millisecond)
	s := f.Stats.Snapshot()
	if s.UpPackets > 30 || s.Dropped < 150 {
		t.Fatalf("burst not limited: forwarded %d dropped %d", s.UpPackets, s.Dropped)
	}
}
