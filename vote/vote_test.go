package vote

import (
	"strings"
	"testing"
)

func testBallot() Ballot {
	return Ballot{Round: 3, Options: []Option{{ID: "a", Name: "Fiesta Slayer on Interference"}, {ID: "b", Name: "CTF: Arena on Kusini Bay"}},
		RemainingMS: 9000, Counts: []int{1, 0}, Mine: 0, Winner: -1, StartMS: -1}
}

func TestBallotRoundTrip(t *testing.T) {
	d, err := EncodeBallot(testBallot())
	if err != nil {
		t.Fatal(err)
	}
	got, ok := DecodeBallot(d)
	if !ok || got.Round != 3 || len(got.Options) != 2 || got.Options[1].Name != "CTF: Arena on Kusini Bay" || got.Mine != 0 || got.Winner != -1 {
		t.Fatalf("%+v %v", got, ok)
	}
	if !Is(d) {
		t.Fatal("prefix not recognised")
	}
}

func TestBallotRejectsMalformed(t *testing.T) {
	bad := []func(*Ballot){
		func(b *Ballot) { b.Round = 0 },
		func(b *Ballot) { b.Options = nil; b.Counts = nil },
		func(b *Ballot) { b.Counts = []int{1} },
		func(b *Ballot) { b.Mine = 2 },
		func(b *Ballot) { b.Closed = true },
		func(b *Ballot) { b.Counts[0] = -1 },
		func(b *Ballot) { b.Options[0].ID = "" },
		func(b *Ballot) { b.Options[1].ID = b.Options[0].ID }, // duplicate IDs break the app's keyed list
		func(b *Ballot) {
			for len(b.Options) <= MaxOptions {
				b.Options = append(b.Options, Option{ID: "x", Name: "x"})
				b.Counts = append(b.Counts, 0)
			}
		},
	}
	for i, f := range bad {
		b := testBallot()
		b.Options = append([]Option(nil), b.Options...)
		b.Counts = append([]int(nil), b.Counts...)
		f(&b)
		if _, err := EncodeBallot(b); err == nil {
			t.Fatalf("case %d encoded", i)
		}
	}
	for _, d := range []string{"HICOMM-BALLOT {", "HICOMM-BALLOT {\"round\":1,\"extra\":1}", "nonsense",
		BallotPrefix + `{"round":1,"options":[{"id":"a","name":"A"}],"counts":[0],"mine":-1,"winner":-1} trailing`} {
		if _, ok := DecodeBallot([]byte(d)); ok {
			t.Fatalf("decoded %q", d)
		}
	}
}

func TestCastRoundTrip(t *testing.T) {
	d, err := EncodeCast(Cast{Round: 5, Choice: 3})
	if err != nil {
		t.Fatal(err)
	}
	if c, ok := DecodeCast(d); !ok || c.Round != 5 || c.Choice != 3 {
		t.Fatalf("%+v %v", c, ok)
	}
	for _, c := range []Cast{{0, 0}, {1, -1}, {1, MaxOptions}} {
		if _, err := EncodeCast(c); err == nil {
			t.Fatalf("encoded %+v", c)
		}
	}
	if _, ok := DecodeCast([]byte(CastPrefix + `{"round":1,"choice":9}`)); ok {
		t.Fatal("decoded out-of-range choice")
	}
	if _, ok := DecodeCast(d[:len(d)-1]); ok {
		t.Fatal("decoded truncated cast")
	}
}

func TestNameAndSize(t *testing.T) {
	long := strings.Repeat("é", MaxNameBytes)
	n := Name(long)
	if len(n) > MaxNameBytes || !strings.HasPrefix(long, n) || len(n)%2 != 0 {
		t.Fatalf("cut %d bytes", len(n))
	}
	b := Ballot{Round: 1, Mine: -1, Winner: -1, StartMS: -1}
	for i := 0; i < MaxOptions; i++ {
		b.Options = append(b.Options, Option{ID: strings.Repeat("i", MaxNameBytes-1) + string(rune('0'+i)), Name: Name(long)})
		b.Counts = append(b.Counts, 99)
	}
	if d, err := EncodeBallot(b); err != nil || len(d) > MaxDatagram {
		t.Fatalf("largest ballot: %d bytes, %v", len(d), err)
	}
}

func TestLargestBallotKeepsOrDropsThumbs(t *testing.T) {
	b := Ballot{Round: 1, Mine: -1, Winner: -1, StartMS: -1}
	for i := 0; i < MaxOptions; i++ {
		b.Options = append(b.Options, Option{ID: strings.Repeat("i", MaxNameBytes-1) + string(rune('0'+i)), Name: strings.Repeat("n", MaxNameBytes),
			Thumb: ifA + "/" + ifV})
		b.Counts = append(b.Counts, 99)
	}
	d, err := EncodeBallot(b)
	if err != nil || len(d) > MaxDatagram {
		t.Fatalf("%d bytes %v", len(d), err)
	}
	got, ok := DecodeBallot(d)
	if !ok || len(got.Options) != MaxOptions {
		t.Fatal("ballot lost")
	}
	// Typical names keep their thumbnails.
	for i := range b.Options {
		b.Options[i].ID, b.Options[i].Name = "kusini-ctf-"+string(rune('0'+i)), "CTF: Arena on Kusini Bay"
	}
	d, _ = EncodeBallot(b)
	if got, _ := DecodeBallot(d); got.Options[3].Thumb == "" {
		t.Fatal("typical ballot dropped thumbnails")
	}
	b.Options[0].Thumb = "https://evil.example/x.jpg"
	if _, err := EncodeBallot(b); err == nil {
		t.Fatal("encoded a URL as a thumbnail reference")
	}
}
