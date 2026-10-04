package relay

import (
	"bytes"
	"context"
	"net"
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
