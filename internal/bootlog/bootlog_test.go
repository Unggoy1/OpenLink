package bootlog

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestParse(t *testing.T) {
	ev, ok := Parse("2026-10-04T01:04:04.387Z\t0x00005104\tWaitForAllocation state transition: 'watingForSessionEstablishment' -> 'setupComplete'\n")
	if !ok || ev.PID != 20740 || ev.State != "setupComplete" {
		t.Fatalf("got %+v ok=%v", ev, ok)
	}
	if _, ok := Parse("garbage line"); ok {
		t.Fatal("garbage parsed")
	}
	ev, ok = Parse("2026-10-04T01:00:27.545Z\t0x00005104\tGame Session Established")
	if !ok || ev.State != "" || ev.Message != "Game Session Established" {
		t.Fatalf("got %+v", ev)
	}
}

func TestFollowerFiltersPIDAndStartsAtEnd(t *testing.T) {
	p := filepath.Join(t.TempDir(), "BootstrapLog.txt")
	if err := os.WriteFile(p, []byte("2026-10-04T00:00:00Z\t0x00000001\told line\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	stop, out := make(chan struct{}), make(chan Event, 8)
	go Follower{Path: p, PID: 2}.Run(stop, out)
	defer close(stop)
	time.Sleep(100 * time.Millisecond)
	fh, _ := os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0)
	fh.WriteString("2026-10-04T00:00:01Z\t0x00000001\tother pid\n")
	fh.WriteString("2026-10-04T00:00:02Z\t0x00000002\tWaitForAllocation state transition: 'a' -> 'setupComplete'\n")
	fh.Close()
	select {
	case ev := <-out:
		if ev.PID != 2 || ev.State != "setupComplete" {
			t.Fatalf("got %+v", ev)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no event")
	}
}
