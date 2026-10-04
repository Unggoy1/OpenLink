package sim

import (
	"bytes"
	"context"
	"net"
	"testing"
	"time"
)

func TestProbeEcho(t *testing.T) {
	srv, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go Echo(ctx, srv)
	res, err := Probe(ctx, srv.LocalAddr().(*net.UDPAddr), 3, time.Second)
	if err != nil || res.Sent != 3 || res.Received != 3 {
		t.Fatalf("%+v %v", res, err)
	}
}

func TestEchoIgnoresOtherTraffic(t *testing.T) {
	srv, _ := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go Echo(ctx, srv)
	c, _ := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	defer c.Close()
	c.WriteToUDP([]byte("not a probe"), srv.LocalAddr().(*net.UDPAddr))
	c.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
	if _, _, err := c.ReadFromUDP(make([]byte, 64)); err == nil {
		t.Fatal("echoed a non-probe datagram")
	}
}

func TestBeaconShape(t *testing.T) {
	a, b := Beacon(), Beacon()
	if len(a) != 79 || !bytes.HasPrefix(a, []byte(BeaconPrefix)) || bytes.Equal(a, b) {
		t.Fatal("unexpected simulated beacon")
	}
}
