package hostctl

import (
	"bytes"
	"context"
	"net"
	"testing"
	"time"
)

func TestSelectedReplyIsDistinctFromContentApplied(t *testing.T) {
	r := Reply{Code: CodeSelected, ID: 2, PID: 42, Gates: RequiredSelectionGates, Generation: 1, Pair: testPair()}
	var wire bytes.Buffer
	if err := EncodeReply(&wire, r); err != nil {
		t.Fatal(err)
	}
	got, err := DecodeReply(&wire)
	r.Version = 1
	if err != nil || got != r {
		t.Fatalf("selected roundtrip %+v %v", got, err)
	}
	if got.Gates&GateContent != 0 || got.Code == CodeApplied {
		t.Fatal("selection claimed content application")
	}
}

func TestSelectedBridgeChecksEvidenceAndEngineModeOperation(t *testing.T) {
	for _, op := range []uint16{OpPrepareEngine, OpInitialize} {
		for _, kind := range []string{"selected", "missing_gate", "wrong_pair", "zero_generation", "claims_content"} {
			t.Run(kind, func(t *testing.T) {
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
					req, err := DecodeRequest(b)
					if err != nil {
						return
					}
					if req.Op != op {
						return
					}
					reply := Reply{Code: CodeSelected, ID: req.ID, PID: 42, Gates: RequiredSelectionGates, Generation: 1, Pair: req.Pair}
					switch kind {
					case "missing_gate":
						reply.Gates &^= GateSelection
					case "wrong_pair":
						reply.Pair[0]++
					case "zero_generation":
						reply.Generation = 0
					case "claims_content":
						reply.Gates |= GateContent
					}
					EncodeReply(b, reply)
				}()
				peer, err := AcceptBridge(ctx, a, token, 42)
				if err != nil {
					t.Fatal(err)
				}
				operation := peer.PrepareEngine
				if op == OpInitialize {
					operation = peer.Initialize
				}
				reply, err := operation(ctx, testPair())
				if kind == "selected" {
					if err != nil || reply.Code != CodeSelected {
						t.Fatalf("selected %+v %v", reply, err)
					}
				} else if err == nil {
					t.Fatal("unverified selection accepted")
				}
			})
		}
	}
}

func TestAcknowledgementTicketsAreMonotonicAcrossStages(t *testing.T) {
	for _, kind := range []string{"selected_then_selected", "applied_then_selected", "selected_then_applied"} {
		t.Run(kind, func(t *testing.T) {
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
				for step := 0; step < 2; step++ {
					req, err := DecodeRequest(b)
					if err != nil {
						return
					}
					reply := Reply{Code: CodeSelected, ID: req.ID, PID: 42, Gates: RequiredSelectionGates, Generation: 1, Pair: req.Pair}
					if (kind == "applied_then_selected" && step == 0) || (kind == "selected_then_applied" && step == 1) {
						reply.Code = CodeApplied
						reply.Gates = RequiredGates
					}
					if EncodeReply(b, reply) != nil {
						return
					}
				}
			}()
			peer, err := AcceptBridge(ctx, a, token, 42)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := peer.Initialize(ctx, testPair()); err != nil {
				t.Fatal(err)
			}
			if _, err := peer.PrepareEngine(ctx, testPair()); err == nil {
				t.Fatal("repeated ticket accepted across stages")
			}
		})
	}
}
