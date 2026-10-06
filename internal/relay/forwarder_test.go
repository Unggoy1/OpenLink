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
	// The forwarder counts a reply after sending it, so the client can read
	// the reply before the count is updated: wait briefly for the stats.
	var s Snapshot
	for deadline := time.Now().Add(time.Second); ; time.Sleep(5 * time.Millisecond) {
		s = f.Stats.Snapshot()
		if s.UpPackets == 2 && s.DownPackets == 2 && s.Sessions == 2 || time.Now().After(deadline) {
			break
		}
	}
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

// A player-side forwarder chained to a host-side forwarder, as in the real
// setup: a message the player side sends upstream reaches the host's
// InterceptFrom from the same address as the player's game traffic, and a
// message the host sends to that client is consumed by InterceptDown instead
// of reaching the game.
func TestSideChannelThroughSessions(t *testing.T) {
	server := echoServer(t)
	defer server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	type msg struct {
		b    []byte
		from string
	}
	got := make(chan msg, 4)
	host := &Forwarder{Listen: loop(t), Upstream: server.LocalAddr().(*net.UDPAddr)}
	host.InterceptFrom = func(b []byte, client *net.UDPAddr) ([]byte, bool) {
		if !bytes.HasPrefix(b, []byte("VOTE ")) {
			return nil, false
		}
		got <- msg{append([]byte(nil), b...), client.String()}
		return nil, true
	}
	go host.Run(ctx)
	downs := make(chan []byte, 4)
	player := &Forwarder{Listen: loop(t), Upstream: host.Listen.LocalAddr().(*net.UDPAddr)}
	player.InterceptDown = func(b []byte) bool {
		if !bytes.HasPrefix(b, []byte("BALLOT ")) {
			return false
		}
		downs <- append([]byte(nil), b...)
		return true
	}
	go player.Run(ctx)

	game := loop(t)
	defer game.Close()
	game.WriteToUDP([]byte("game"), player.Listen.LocalAddr().(*net.UDPAddr))
	game.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 2048)
	if n, _, err := game.ReadFromUDP(buf); err != nil || string(buf[:n]) != "echo:game" {
		t.Fatalf("game round trip: %q %v", buf[:n], err)
	}
	clients := host.Clients(time.Minute)
	if len(clients) != 1 || !host.HasSession(clients[0], time.Minute) {
		t.Fatalf("host sessions %v", clients)
	}
	if n := player.SendUpstream([]byte("VOTE 1")); n != 1 {
		t.Fatalf("sent through %d sessions", n)
	}
	select {
	case m := <-got:
		if string(m.b) != "VOTE 1" || m.from != clients[0].String() {
			t.Fatalf("vote %q from %s, session %s", m.b, m.from, clients[0])
		}
	case <-time.After(2 * time.Second):
		t.Fatal("vote not intercepted")
	}
	if err := host.SendTo(clients[0], []byte("BALLOT 1")); err != nil {
		t.Fatal(err)
	}
	select {
	case b := <-downs:
		if string(b) != "BALLOT 1" {
			t.Fatalf("ballot %q", b)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("ballot not intercepted")
	}
	game.SetReadDeadline(time.Now().Add(300 * time.Millisecond))
	if n, _, err := game.ReadFromUDP(buf); err == nil {
		t.Fatalf("ballot leaked to the game: %q", buf[:n])
	}
	if s := host.Stats.Snapshot(); s.UpPackets != 1 {
		t.Fatalf("vote was forwarded to the server: %d up packets", s.UpPackets)
	}
	stranger := loop(t)
	defer stranger.Close()
	if host.HasSession(stranger.LocalAddr().(*net.UDPAddr), time.Minute) {
		t.Fatal("stranger has a session")
	}
}
