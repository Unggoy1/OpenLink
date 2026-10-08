package beacon

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

func TestAdvertiseToCapture(t *testing.T) {
	rx, tx := loop(t), loop(t)
	defer tx.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var src, got Store
	want := bytes.Repeat([]byte{0xab}, 79)
	src.Put(want, time.Now())
	go Capture(ctx, rx, func(a *net.UDPAddr, _ []byte) bool { return a.IP.IsLoopback() }, &got)
	go Advertise(ctx, tx, rx.LocalAddr().(*net.UDPAddr), &src, 20*time.Millisecond, time.Second, nil)

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if b, _, n := got.Latest(); n > 0 {
			if !bytes.Equal(b, want) {
				t.Fatal("bytes changed in transit")
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("beacon not captured")
}

func TestCaptureRejectsUnacceptedSource(t *testing.T) {
	rx, tx := loop(t), loop(t)
	defer tx.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var got Store
	go Capture(ctx, rx, func(*net.UDPAddr, []byte) bool { return false }, &got)
	tx.WriteToUDP(make([]byte, 79), rx.LocalAddr().(*net.UDPAddr))
	time.Sleep(200 * time.Millisecond)
	if _, _, n := got.Latest(); n != 0 {
		t.Fatal("captured a datagram from a rejected source")
	}
}

func TestAdvertiseSkipsStale(t *testing.T) {
	var src Store
	src.Put(bytes.Repeat([]byte{1}, 20), time.Now().Add(-time.Hour))
	rx, tx := loop(t), loop(t)
	defer rx.Close()
	defer tx.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	sent := 0
	Advertise(ctx, tx, rx.LocalAddr().(*net.UDPAddr), &src, 10*time.Millisecond, time.Second, func() { sent++ })
	if sent != 0 {
		t.Fatalf("sent %d stale beacons", sent)
	}
}

func TestValid(t *testing.T) {
	if Valid(make([]byte, 15)) || !Valid(make([]byte, 79)) || Valid(make([]byte, 600)) {
		t.Fatal("size bounds wrong")
	}
}
