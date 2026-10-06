package connect

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"net/http/httptest"
	"testing"
	"time"

	"halocommunity/internal/api"
	"halocommunity/internal/directory"
	"halocommunity/internal/sim"
	"halocommunity/vote"
)

func freePort(t *testing.T) int {
	c, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	return c.LocalAddr().(*net.UDPAddr).Port
}

// TestSessionEndToEnd: directory + simulated server + session on loopback.
// The "game" listens on the discovery port and sends to the session's local address.
func TestSessionEndToEnd(t *testing.T) {
	gamePort, discPort := freePort(t), freePort(t)
	t.Setenv("OPENLINK_DEV_PORTS", fmt.Sprintf("%d,%d", gamePort, discPort))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Simulated server on 127.0.0.2 (stands in for the remote host).
	srv, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 2), Port: 0})
	if err != nil {
		t.Skipf("127.0.0.2 unavailable: %v", err)
	}
	go sim.Echo(ctx, srv)
	srvPort := srv.LocalAddr().(*net.UDPAddr).Port

	// Directory with the server registered and a beacon posted.
	ts := httptest.NewServer(directory.New(directory.Config{RegisterKey: "k", ShowUnconfirmed: true}))
	defer ts.Close()
	dc := directory.NewClient(ts.URL, "k")
	reg, err := dc.Register(ctx, api.RegisterRequest{Name: "Sim", Host: "127.0.0.2", Port: srvPort, Build: "b"})
	if err != nil {
		t.Fatal(err)
	}
	beacon := sim.Beacon()
	if err := dc.Heartbeat(ctx, reg.ID, reg.Token, api.Heartbeat{Status: "simulated", Players: -1, Beacon: beacon}); err != nil {
		t.Fatal(err)
	}
	servers, err := List(ctx, ts.URL, "b")
	if err != nil || len(servers) != 1 || !servers[0].Joinable {
		t.Fatalf("list: %+v %v", servers, err)
	}

	// The "game": listens for beacons on the discovery port.
	game, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: discPort})
	if err != nil {
		t.Fatal(err)
	}
	defer game.Close()

	sess, err := Start(servers[0], Options{Directory: ts.URL})
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Stop()

	buf := make([]byte, 2048)
	game.SetReadDeadline(time.Now().Add(5 * time.Second))
	n, from, err := game.ReadFromUDP(buf)
	if err != nil {
		t.Fatalf("no beacon reached the game: %v", err)
	}
	if !bytes.Equal(buf[:n], beacon) || !from.IP.IsLoopback() {
		t.Fatalf("beacon altered or wrong source %v", from)
	}

	// The game dials the beacon's source on the game port; the session forwards it.
	probe := []byte(sim.ProbePrefix + "x")
	game.WriteToUDP(probe, &net.UDPAddr{IP: from.IP, Port: gamePort})
	game.SetReadDeadline(time.Now().Add(3 * time.Second))
	for {
		n, _, err = game.ReadFromUDP(buf)
		if err != nil {
			t.Fatalf("no reply through the session: %v", err)
		}
		if bytes.Equal(buf[:n], probe) {
			break // skip further beacons
		}
	}
	st := sess.Status()
	if st.UpPackets != 1 || st.DownPackets != 1 || st.BeaconAge < 0 || !st.Connected() {
		t.Fatalf("status %+v", st)
	}

	// Port is free again after Stop.
	sess.Stop()
	c, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: gamePort})
	if err != nil {
		t.Fatalf("port not released: %v", err)
	}
	c.Close()
}

func TestPing(t *testing.T) {
	srv, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go sim.Echo(ctx, srv)
	rtt, ok := Ping(ctx, Server{Host: "127.0.0.1", Port: srv.LocalAddr().(*net.UDPAddr).Port})
	if !ok || rtt < 0 || rtt > time.Second { // loopback can be below the Windows clock resolution (0)
		t.Fatalf("ping %v %v", rtt, ok)
	}
	dead, _ := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)}) // never answers
	defer dead.Close()
	if _, ok := Ping(ctx, Server{Host: "127.0.0.1", Port: dead.LocalAddr().(*net.UDPAddr).Port}); ok {
		t.Fatal("silent endpoint reported as answering")
	}
}

func TestSessionKeepsBallots(t *testing.T) {
	ss := &Session{}
	if ss.takeBallot([]byte("game datagram")) {
		t.Fatal("consumed a game datagram")
	}
	if !ss.takeBallot([]byte(vote.BallotPrefix + "{broken")) {
		t.Fatal("passed a malformed ballot to the game")
	}
	if _, _, ok := ss.Ballot(); ok {
		t.Fatal("kept a malformed ballot")
	}
	d, err := vote.EncodeBallot(vote.Ballot{Round: 2, Options: []vote.Option{{ID: "a", Name: "A"}}, Counts: []int{0},
		Mine: -1, Winner: -1, StartMS: -1, RemainingMS: 1000})
	if err != nil {
		t.Fatal(err)
	}
	if !ss.takeBallot(d) {
		t.Fatal("ballot not consumed")
	}
	if b, at, ok := ss.Ballot(); !ok || b.Round != 2 || at.IsZero() {
		t.Fatalf("%+v %v %v", b, at, ok)
	}
}
