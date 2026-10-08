package connect

import (
	"context"
	"fmt"
	"net"
	"net/http/httptest"
	"testing"
	"time"

	"halocommunity/internal/api"
	"halocommunity/internal/dirclient"
	"halocommunity/internal/directory"
	"halocommunity/internal/sim"
)

// goneFixture: a directory with one registered server (an echo on 127.0.0.2)
// and a session joined to it, with short gone timings.
type goneFixture struct {
	dc   *dirclient.Client
	reg  api.RegisterResponse
	port int
	sess *Session
	game *net.UDPConn // stands in for the local game: sends through the session
}

func newGoneFixture(t *testing.T) *goneFixture {
	t.Helper()
	gamePort, discPort := freePort(t), freePort(t)
	t.Setenv("OPENLINK_DEV_PORTS", fmt.Sprintf("%d,%d", gamePort, discPort))
	oldGone, oldPoll := goneAfter, beaconPoll
	goneAfter, beaconPoll = 400*time.Millisecond, 50*time.Millisecond
	t.Cleanup(func() { goneAfter, beaconPoll = oldGone, oldPoll })
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	srv, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 2)})
	if err != nil {
		t.Skipf("127.0.0.2 unavailable: %v", err)
	}
	t.Cleanup(func() { srv.Close() })
	go sim.Echo(ctx, srv)
	ts := httptest.NewServer(directory.New(directory.Config{RegisterKey: "k", ShowUnconfirmed: true, AllowPrivateHosts: true}))
	t.Cleanup(ts.Close)
	f := &goneFixture{dc: dirclient.NewClient(ts.URL, "k"), port: srv.LocalAddr().(*net.UDPAddr).Port}
	f.reg = f.register(t)
	servers, err := List(ctx, ts.URL, "")
	if err != nil || len(servers) != 1 {
		t.Fatalf("list: %+v %v", servers, err)
	}
	if f.sess, err = Start(servers[0], Options{Directory: ts.URL}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(f.sess.Stop)
	if f.game, err = net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.game.Close() })
	return f
}

func (f *goneFixture) register(t *testing.T) api.RegisterResponse {
	t.Helper()
	ctx := context.Background()
	reg, err := f.dc.Register(ctx, api.RegisterRequest{Name: "Sim", Host: "127.0.0.2", Port: f.port, Build: "b"})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.dc.Heartbeat(ctx, reg.ID, reg.Token, api.Heartbeat{Status: "simulated", Players: -1, Beacon: sim.Beacon()}); err != nil {
		t.Fatal(err)
	}
	return reg
}

// play sends one game datagram through the session; the echo answers it.
func (f *goneFixture) play() {
	_, port, _ := net.SplitHostPort(f.sess.local)
	var p int
	fmt.Sscan(port, &p)
	f.game.WriteToUDP([]byte(sim.ProbePrefix+"x"), &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: p})
}

func waitFor(t *testing.T, within time.Duration, cond func() bool) bool {
	t.Helper()
	for deadline := time.Now().Add(within); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		if cond() {
			return true
		}
	}
	return false
}

func TestSessionGoneWhenUnlisted(t *testing.T) {
	f := newGoneFixture(t)
	time.Sleep(3 * goneAfter)
	if f.sess.Gone() {
		t.Fatal("gone while the server is listed")
	}
	if err := f.dc.Unregister(context.Background(), f.reg.ID, f.reg.Token); err != nil {
		t.Fatal(err)
	}
	if !waitFor(t, 3*time.Second, f.sess.Gone) {
		t.Fatalf("not gone after the listing disappeared: %+v", f.sess.Status())
	}
}

// A directory that lost its listings gets the server back under a new ID; the
// session follows it by address instead of leaving.
func TestSessionFollowsNewListing(t *testing.T) {
	f := newGoneFixture(t)
	if err := f.dc.Unregister(context.Background(), f.reg.ID, f.reg.Token); err != nil {
		t.Fatal(err)
	}
	again := f.register(t)
	if !waitFor(t, 3*time.Second, func() bool { return f.sess.Status().Server.ID == again.ID }) {
		t.Fatalf("session did not follow the new listing: %+v", f.sess.Status().Server)
	}
	time.Sleep(3 * goneAfter)
	if f.sess.Gone() {
		t.Fatal("gone although the server is listed again")
	}
}

// While the server still answers the game, a missing listing alone (for
// example the directory being restarted) does not end the session.
func TestSessionStaysWhileServerAnswers(t *testing.T) {
	f := newGoneFixture(t)
	if err := f.dc.Unregister(context.Background(), f.reg.ID, f.reg.Token); err != nil {
		t.Fatal(err)
	}
	for end := time.Now().Add(4 * goneAfter); time.Now().Before(end); time.Sleep(50 * time.Millisecond) {
		f.play()
		if f.sess.Gone() {
			t.Fatal("gone while the server answers the game")
		}
	}
	if !waitFor(t, 3*time.Second, f.sess.Gone) {
		t.Fatal("not gone after the game traffic stopped")
	}
}
