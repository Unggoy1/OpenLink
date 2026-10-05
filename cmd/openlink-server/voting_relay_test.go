package main

import (
	"context"
	"net"
	"testing"
	"time"

	"halocommunity/internal/relay"
	"halocommunity/vote"
)

// A vote sent the way the app sends it reaches the voter through a real proxy.
func TestVoteThroughRealProxy(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	udp := func() *net.UDPConn {
		c, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
		if err != nil {
			t.Fatal(err)
		}
		return c
	}
	server := udp() // stands in for the game server: silent
	defer server.Close()
	v := newVoter(&voteServer{state: 8, connected: 1}, nil)
	host := &relay.Forwarder{Listen: udp(), Upstream: server.LocalAddr().(*net.UDPAddr), InterceptFrom: v.intercept}
	v.net = host
	go host.Run(ctx)
	ballots := make(chan vote.Ballot, 8)
	player := &relay.Forwarder{Listen: udp(), Upstream: host.Listen.LocalAddr().(*net.UDPAddr),
		InterceptDown: func(d []byte) bool {
			if b, ok := vote.DecodeBallot(d); ok {
				ballots <- b
				return true
			}
			return false
		}}
	go player.Run(ctx)
	game := udp()
	defer game.Close()
	game.WriteToUDP([]byte("game hello"), player.Listen.LocalAddr().(*net.UDPAddr))
	waitFor(t, "proxy session", func() bool { return len(host.Clients(time.Minute)) == 1 })

	v.openRound("", time.Now())
	v.broadcast(time.Now())
	b := <-ballots
	d, _ := vote.EncodeCast(vote.Cast{Round: b.Round, Choice: 1})
	if n := player.SendUpstream(d); n != 1 {
		t.Fatalf("sent through %d sessions", n)
	}
	select {
	case ack := <-ballots:
		if ack.Mine != 1 || ack.Counts[1] != 1 {
			t.Fatalf("ack %+v", ack)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no ack ballot")
	}
}
