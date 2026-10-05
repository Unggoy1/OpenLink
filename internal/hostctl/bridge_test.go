package hostctl

import (
	"context"
	"net"
	"testing"
	"time"
)

func TestBridgeAuthenticationAndPrepareAcknowledgement(t *testing.T) {
	for _, kind := range []string{"pending", "applied", "wrong_token", "wrong_pid", "wrong_id", "missing_gates", "wrong_pair", "zero_generation"} {
		t.Run(kind, func(t *testing.T) {
			a, b := net.Pipe()
			defer a.Close()
			defer b.Close()
			var token [32]byte
			token[0] = 10
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			go func() {
				hello := Request{Op: OpHello, ID: 1, Token: token, PID: 42}
				if kind == "wrong_token" {
					hello.Token[0]++
				}
				if kind == "wrong_pid" {
					hello.PID++
				}
				if EncodeRequest(b, hello) != nil {
					return
				}
				if _, err := DecodeReply(b); err != nil {
					return
				}
				r, err := DecodeRequest(b)
				if err != nil {
					return
				}
				reply := Reply{Code: CodeApplied, ID: r.ID, PID: 42, Gates: RequiredGates, Generation: 2, Pair: r.Pair}
				switch kind {
				case "pending":
					reply.Code = CodeNativePending
					reply.Gates = 0
					reply.Generation = 0
					reply.Pair = AssetPair{}
				case "wrong_id":
					reply.ID++
				case "missing_gates":
					reply.Gates &^= GateLobby
				case "wrong_pair":
					reply.Pair[0]++
				case "zero_generation":
					reply.Generation = 0
				}
				EncodeReply(b, reply)
			}()
			peer, err := AcceptBridge(ctx, a, token, 42)
			if kind == "wrong_token" || kind == "wrong_pid" {
				if err == nil {
					t.Fatal("unauthenticated peer accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			r, err := peer.Prepare(ctx, testPair())
			if kind == "pending" || kind == "applied" {
				if err != nil {
					t.Fatal(err)
				}
				if kind == "pending" && r.Code != CodeNativePending {
					t.Fatal("pending became applied")
				}
				return
			}
			if err == nil {
				t.Fatal("invalid application acknowledgement accepted")
			}
		})
	}
}

func TestBridgeCancellationInterruptsIO(t *testing.T) {
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()
	var token [32]byte
	token[0] = 1
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	go func() {
		EncodeRequest(b, Request{Op: OpHello, ID: 1, PID: 42, Token: token})
		DecodeReply(b)
		DecodeRequest(b)
	}()
	peer, err := AcceptBridge(ctx, a, token, 42)
	if err != nil {
		t.Fatal(err)
	}
	short, stop := context.WithTimeout(ctx, 20*time.Millisecond)
	defer stop()
	start := time.Now()
	if _, err := peer.Status(short); err == nil {
		t.Fatal("canceled read succeeded")
	}
	if time.Since(start) > 500*time.Millisecond {
		t.Fatal("cancellation did not interrupt I/O")
	}
	if !peer.Closed() {
		t.Fatal("I/O cancellation was not marked terminal")
	}
}

func TestBridgeCancellationWhileAnotherCallIsWaiting(t *testing.T) {
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()
	var token [32]byte
	token[0] = 1
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	reading := make(chan struct{})
	go func() {
		EncodeRequest(b, Request{Op: OpHello, ID: 1, PID: 42, Token: token})
		DecodeReply(b)
		DecodeRequest(b)
		close(reading)
	}()
	peer, err := AcceptBridge(ctx, a, token, 42)
	if err != nil {
		t.Fatal(err)
	}
	first := make(chan struct{})
	go func() { peer.Status(ctx); close(first) }()
	<-reading
	short, stop := context.WithTimeout(ctx, 20*time.Millisecond)
	defer stop()
	second := make(chan error, 1)
	go func() { _, err := peer.Status(short); second <- err }()
	select {
	case err := <-second:
		if err == nil {
			t.Fatal("canceled queued call succeeded")
		}
	case <-time.After(250 * time.Millisecond):
		cancel()
		<-first
		<-second
		t.Fatal("queued call ignored cancellation")
	}
	if peer.Closed() {
		t.Fatal("queued cancellation closed healthy connection")
	}
	cancel()
	<-first
}

func TestBridgeConcurrentExchangesAndStaleGenerations(t *testing.T) {
	for _, stale := range []bool{false, true} {
		t.Run(map[bool]string{false: "concurrent", true: "stale"}[stale], func(t *testing.T) {
			a, b := net.Pipe()
			defer a.Close()
			defer b.Close()
			var token [32]byte
			token[0] = 1
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			go func() {
				if EncodeRequest(b, Request{Op: OpHello, ID: 1, PID: 42, Token: token}) != nil {
					return
				}
				if _, err := DecodeReply(b); err != nil {
					return
				}
				for generation := uint64(1); generation <= 2; generation++ {
					r, err := DecodeRequest(b)
					if err != nil {
						return
					}
					g := generation
					if stale {
						g = 1
					}
					if EncodeReply(b, Reply{Code: CodeApplied, ID: r.ID, PID: 42, Gates: RequiredGates, Generation: g, Pair: r.Pair}) != nil {
						return
					}
				}
			}()
			peer, err := AcceptBridge(ctx, a, token, 42)
			if err != nil {
				t.Fatal(err)
			}
			if stale {
				if _, err = peer.Prepare(ctx, testPair()); err != nil {
					t.Fatal(err)
				}
				if _, err = peer.Prepare(ctx, testPair()); err == nil {
					t.Fatal("stale generation accepted")
				}
				return
			}
			done := make(chan error, 2)
			for i := 0; i < 2; i++ {
				go func() { _, err := peer.Prepare(ctx, testPair()); done <- err }()
			}
			for i := 0; i < 2; i++ {
				if err := <-done; err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}
