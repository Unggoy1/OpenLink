package hostctl

import (
	"bytes"
	"encoding/binary"
	"io"
	"strings"
	"testing"
)

func testPair() AssetPair {
	var p AssetPair
	for i := range p {
		p[i] = byte(i + 1)
	}
	return p
}

func TestRequestWireShapesAndBounds(t *testing.T) {
	for _, op := range []uint16{OpHello, OpStatus, OpPrepare, OpPrepareEngine, OpInitialize} {
		r := Request{Op: op, ID: 17, PID: 123, Pair: testPair()}
		r.Token[0] = 8
		var b bytes.Buffer
		if err := EncodeRequest(&b, r); err != nil {
			t.Fatal(err)
		}
		want := map[uint16]int{OpHello: 52, OpStatus: 48, OpPrepare: 112, OpPrepareEngine: 112, OpInitialize: 112}[op]
		if int(binary.LittleEndian.Uint32(b.Bytes())) != want {
			t.Fatal("wrong frame length")
		}
		got, err := DecodeRequest(&b)
		if err != nil || got.Op != op || got.ID != r.ID || got.Token != r.Token {
			t.Fatalf("roundtrip: %+v %v", got, err)
		}
		if (op == OpPrepare || op == OpPrepareEngine || op == OpInitialize) && got.Pair != r.Pair {
			t.Fatal("pair changed")
		}
	}
	for _, n := range []uint32{0, 47, 113, 0xffffffff} {
		var b bytes.Buffer
		binary.Write(&b, binary.LittleEndian, n)
		if _, err := DecodeRequest(&b); err == nil {
			t.Fatalf("accepted length %d", n)
		}
	}
}

func TestRequestRejectsBadInputs(t *testing.T) {
	for _, r := range []Request{{Op: OpStatus}, {Op: 99, ID: 1}, {Op: OpHello, ID: 1}, {Op: OpPrepare, ID: 1}} {
		if err := EncodeRequest(&bytes.Buffer{}, r); err == nil {
			t.Fatalf("accepted %+v", r)
		}
	}
	var b bytes.Buffer
	EncodeRequest(&b, Request{Op: OpStatus, ID: 1})
	data := b.Bytes()
	data[8] = 2 // version, after length and magic
	if _, err := DecodeRequest(bytes.NewReader(data)); err == nil {
		t.Fatal("accepted wrong version")
	}
	data[8] = 1
	data[10] = byte(OpHello) // shape mismatch
	if _, err := DecodeRequest(bytes.NewReader(data)); err == nil {
		t.Fatal("accepted mismatched shape")
	}
}

func TestReplyWireAndTruncation(t *testing.T) {
	r := Reply{Code: CodeApplied, ID: 9, PID: 70, Gates: RequiredGates, Generation: 5, Pair: testPair()}
	var b bytes.Buffer
	if err := EncodeReply(&b, r); err != nil {
		t.Fatal(err)
	}
	data := append([]byte(nil), b.Bytes()...)
	got, err := DecodeReply(&b)
	r.Version = 1
	if err != nil || got != r {
		t.Fatalf("reply roundtrip: %+v %v", got, err)
	}
	for _, n := range []int{0, 3, 4, 20, 99} {
		if _, err := DecodeReply(bytes.NewReader(data[:n])); err == nil {
			t.Fatalf("accepted truncation %d", n)
		}
	}
	data[10] = 255
	if _, err := DecodeReply(bytes.NewReader(data)); err == nil {
		t.Fatal("accepted unknown code")
	}
}

// Native backend replies use version 2: v1 fields plus state/matches/flags.
func TestReplyVersion2Lifecycle(t *testing.T) {
	var v1 bytes.Buffer
	if err := EncodeReply(&v1, Reply{Code: CodeNativePending, ID: 3, PID: 70, Gates: GateBuild | GateLocked, Generation: 2, Pair: testPair()}); err != nil {
		t.Fatal(err)
	}
	frame := v1.Bytes()[4:]
	v2 := make([]byte, 112)
	copy(v2, frame)
	binary.LittleEndian.PutUint16(v2[4:], 2)
	binary.LittleEndian.PutUint32(v2[96:], 8)
	binary.LittleEndian.PutUint32(v2[100:], 3)
	binary.LittleEndian.PutUint32(v2[104:], 2)
	var wire bytes.Buffer
	if err := writeFrame(&wire, v2); err != nil {
		t.Fatal(err)
	}
	got, err := DecodeReply(&wire)
	if err != nil || got.Version != 2 || got.State != 8 || got.Matches != 3 || got.Flags != 2 || got.Generation != 2 || got.Pair != testPair() {
		t.Fatalf("v2 decode: %+v %v", got, err)
	}
	// A v2 header on a 96-byte frame, or v1 on 112 bytes, is malformed.
	for _, bad := range [][]byte{append([]byte(nil), v2[:96]...), append(append([]byte(nil), frame...), make([]byte, 16)...)} {
		var w bytes.Buffer
		writeFrame(&w, bad)
		if _, err := DecodeReply(&w); err == nil {
			t.Fatal("accepted mismatched version/length")
		}
	}
}

// Version 3 replies append the lobby observation at byte 112.
func TestReplyVersion3Lobby(t *testing.T) {
	b := make([]byte, 176)
	copy(b, "HICR")
	binary.LittleEndian.PutUint16(b[4:], 3)
	binary.LittleEndian.PutUint64(b[8:], 9)
	binary.LittleEndian.PutUint32(b[16:], 70)
	binary.LittleEndian.PutUint32(b[96:], 8)
	binary.LittleEndian.PutUint32(b[112:], LobbyValid|LobbyStartMode)
	binary.LittleEndian.PutUint32(b[116:], 2)
	binary.LittleEndian.PutUint32(b[128:], 0xffffffff)
	binary.LittleEndian.PutUint32(b[140:], 1)
	b[144], b[149] = 1, 1
	binary.LittleEndian.PutUint32(b[152:], 1)
	var w bytes.Buffer
	writeFrame(&w, b)
	got, err := DecodeReply(&w)
	l := got.Lobby
	if err != nil || got.Version != 3 || got.State != 8 || l.Connected != 2 || l.Owner != -1 || l.StartMode != 1 ||
		l.Allowed != 1 || l.Start != 1 || l.UsersRequired != 1 || l.Flags != LobbyValid|LobbyStartMode {
		t.Fatalf("v3 decode: %+v %v", got, err)
	}
	var short bytes.Buffer
	writeFrame(&short, b[:112])
	if _, err := DecodeReply(&short); err == nil {
		t.Fatal("accepted v3 header on a v2-length frame")
	}
}

func TestServerOwnedRequest(t *testing.T) {
	for _, mode := range []uint32{ServerOwnedOff, ServerOwnedNoOwner} {
		var w bytes.Buffer
		if err := EncodeRequest(&w, Request{Op: OpServerOwned, ID: 2, Mode: mode}); err != nil {
			t.Fatal(err)
		}
		got, err := DecodeRequest(&w)
		if err != nil || got.Op != OpServerOwned || got.Mode != mode {
			t.Fatalf("round trip %v: %+v %v", mode, got, err)
		}
	}
	if err := EncodeRequest(&bytes.Buffer{}, Request{Op: OpServerOwned, ID: 2, Mode: 2}); err == nil {
		t.Fatal("encoded server-owned mode 2")
	}
	b := make([]byte, 52)
	copy(b, "HICT")
	binary.LittleEndian.PutUint16(b[4:], 1)
	binary.LittleEndian.PutUint16(b[6:], OpServerOwned)
	binary.LittleEndian.PutUint64(b[8:], 2)
	binary.LittleEndian.PutUint32(b[48:], 2)
	var w bytes.Buffer
	writeFrame(&w, b)
	if _, err := DecodeRequest(&w); err == nil {
		t.Fatal("accepted server-owned mode 2")
	}
}

func TestSetNameRequest(t *testing.T) {
	long := strings.Repeat("x", MaxNameLength)
	for _, name := range []string{"A", "Bob's Server #1 ~ (US-West)", long} {
		var w bytes.Buffer
		if err := EncodeRequest(&w, Request{Op: OpSetName, ID: 2, Name: name}); err != nil {
			t.Fatal(err)
		}
		if n := binary.LittleEndian.Uint32(w.Bytes()); n != 96 {
			t.Fatalf("frame length %d", n)
		}
		got, err := DecodeRequest(&w)
		if err != nil || got.Op != OpSetName || got.Name != name {
			t.Fatalf("round trip %q: %+v %v", name, got, err)
		}
	}
	for _, name := range []string{"", long + "x", "tab\there", "café", "nul\x00"} {
		if err := EncodeRequest(&bytes.Buffer{}, Request{Op: OpSetName, ID: 2, Name: name}); err == nil {
			t.Fatalf("encoded %q", name)
		}
	}
	frame := func(field []byte) *bytes.Buffer {
		b := make([]byte, 48, 96)
		copy(b, "HICT")
		binary.LittleEndian.PutUint16(b[4:], 1)
		binary.LittleEndian.PutUint16(b[6:], OpSetName)
		binary.LittleEndian.PutUint64(b[8:], 2)
		var w bytes.Buffer
		writeFrame(&w, append(b, field...))
		return &w
	}
	padded := func(s string) []byte { f := make([]byte, 48); copy(f, s); return f }
	unterminated := []byte(long + "x")
	gap := padded("ab")
	gap[5] = 'c'
	for name, field := range map[string][]byte{"empty": padded(""), "unterminated": unterminated, "nonzero padding": gap,
		"control": padded("a\x01b"), "high byte": padded("a\xe9"), "short": []byte("abc\x00")} {
		if _, err := DecodeRequest(frame(field)); err == nil {
			t.Fatalf("accepted %s", name)
		}
	}
}

func TestSetLeaderRequest(t *testing.T) {
	for _, xuid := range []uint64{0, 2533274962600518, ^uint64(0)} {
		var w bytes.Buffer
		if err := EncodeRequest(&w, Request{Op: OpSetLeader, ID: 2, XUID: xuid}); err != nil {
			t.Fatal(err)
		}
		if w.Len() != 4+56 {
			t.Fatalf("set-leader frame is %d bytes", w.Len())
		}
		got, err := DecodeRequest(&w)
		if err != nil || got.Op != OpSetLeader || got.XUID != xuid {
			t.Fatalf("round trip %d: %+v %v", xuid, got, err)
		}
	}
}

// Version 4 replies add the lobby leader XUID at 176 and re-asserts at 184.
func TestReplyVersion4Leader(t *testing.T) {
	b := make([]byte, 192)
	copy(b, "HICR")
	binary.LittleEndian.PutUint16(b[4:], 4)
	binary.LittleEndian.PutUint64(b[8:], 9)
	binary.LittleEndian.PutUint32(b[16:], 70)
	binary.LittleEndian.PutUint32(b[112:], LobbyValid|LobbyLeaderValid|LobbyLeaderHeld)
	binary.LittleEndian.PutUint32(b[116:], 2)
	binary.LittleEndian.PutUint64(b[176:], 2533274962600518)
	binary.LittleEndian.PutUint32(b[184:], 3)
	var w bytes.Buffer
	writeFrame(&w, b)
	got, err := DecodeReply(&w)
	l := got.Lobby
	if err != nil || got.Version != 4 || l.Connected != 2 || l.Leader != 2533274962600518 || l.LeaderSets != 3 ||
		l.Flags != LobbyValid|LobbyLeaderValid|LobbyLeaderHeld {
		t.Fatalf("v4 decode: %+v %v", got, err)
	}
	var short bytes.Buffer
	writeFrame(&short, b[:176])
	if _, err := DecodeReply(&short); err == nil {
		t.Fatal("accepted v4 header on a v3-length frame")
	}
}

// Version 5 replies add team diagnostics at 192.
func TestReplyVersion5Teams(t *testing.T) {
	b := make([]byte, 256)
	copy(b, "HICR")
	binary.LittleEndian.PutUint16(b[4:], 5)
	binary.LittleEndian.PutUint64(b[8:], 9)
	binary.LittleEndian.PutUint32(b[16:], 70)
	binary.LittleEndian.PutUint32(b[112:], LobbyValid)
	binary.LittleEndian.PutUint32(b[124:], 0b101)
	binary.LittleEndian.PutUint64(b[176:], 7)
	b[192], b[193], b[194], b[195] = 1, 0xff, 1, 2
	binary.LittleEndian.PutUint32(b[196:], 8)
	binary.LittleEndian.PutUint32(b[204:], 3)
	for i := 208; i < 256; i++ {
		b[i] = 0xff
	}
	b[208], b[209], b[210] = 0xff, 0, 0  // peer 0
	b[214], b[215], b[216] = 31, 1, 0xff // peer 2
	var w bytes.Buffer
	writeFrame(&w, b)
	got, err := DecodeReply(&w)
	l := got.Lobby
	if err != nil || got.Version != 5 || l.Leader != 7 || l.LobbyVariantTeams != 1 || l.GameVariantTeams != -1 ||
		l.LastTeamsEnabled != 1 || l.TeamFixes != 2 || l.LastTeamCount != 8 || l.GameState != 3 || l.PeerTeams[2] != [3]int8{31, 1, -1} {
		t.Fatalf("v5 decode: %+v %v", l, err)
	}
	if s := l.TeamSummary(); s != "0:-1/0/0 2:31/1/-1" {
		t.Fatalf("summary %q", s)
	}
}

func TestTeamPolicyRequest(t *testing.T) {
	for _, p := range []TeamPolicy{{}, {Flags: TeamGuardFFA}, {Flags: TeamGuardFFA | TeamBalance, Mode: TeamModeShuffle, Count: 4, Size: 3},
		{Flags: TeamBalance, Count: MaxTeams, Size: MaxTeamSize}} {
		var w bytes.Buffer
		if err := EncodeRequest(&w, Request{Op: OpTeamPolicy, ID: 2, Teams: p}); err != nil {
			t.Fatal(err)
		}
		if w.Len() != 4+64 {
			t.Fatalf("team-policy frame is %d bytes", w.Len())
		}
		got, err := DecodeRequest(&w)
		if err != nil || got.Op != OpTeamPolicy || got.Teams != p {
			t.Fatalf("round trip %+v: %+v %v", p, got, err)
		}
	}
	for _, bad := range []TeamPolicy{{Flags: 4}, {Mode: 2}, {Count: MaxTeams + 1}, {Size: MaxTeamSize + 1}} {
		if err := EncodeRequest(io.Discard, Request{Op: OpTeamPolicy, ID: 2, Teams: bad}); err == nil {
			t.Fatalf("invalid team policy %+v accepted", bad)
		}
	}
}

// Version 6 replies add bot backfill at 256.
func TestReplyVersion6Bots(t *testing.T) {
	b := make([]byte, 320)
	copy(b, "HICR")
	binary.LittleEndian.PutUint16(b[4:], 6)
	binary.LittleEndian.PutUint64(b[8:], 9)
	binary.LittleEndian.PutUint32(b[16:], 70)
	b[192], b[193] = 0, 0
	b[256], b[257], b[258], b[259], b[260], b[261] = 1, BotModeFFA, 3, 2, BotStateFilled, 1
	binary.LittleEndian.PutUint16(b[262:], 4)
	binary.LittleEndian.PutUint16(b[264:], 1)
	binary.LittleEndian.PutUint16(b[266:], 2)
	binary.LittleEndian.PutUint32(b[268:], 500)
	b[272], b[273], b[274] = 1, 0, 1
	binary.LittleEndian.PutUint32(b[276:], 1234)
	binary.LittleEndian.PutUint16(b[280:], 1<<6|1<<8)
	var w bytes.Buffer
	writeFrame(&w, b)
	got, err := DecodeReply(&w)
	l := got.Lobby
	if err != nil || got.Version != 6 || l.BotsEnabled != 1 || l.ModeBots != BotModeFFA || l.BotCount != 3 || l.BotHumans != 2 ||
		l.BotState != BotStateFilled || l.BotThreadRole != 1 || l.BotAdds != 4 || l.BotRemoves != 1 || l.BotRefused != 2 ||
		l.BotTicks != 500 || !l.BotSupported || l.GameVariantTeams != 0 || l.NavState != 0 || !l.NavFromVariant || l.NavFaces != 1234 || l.BotDifficulties != 1<<6|1<<8 {
		t.Fatalf("v6 decode: %+v %v", l, err)
	}
	var short bytes.Buffer
	writeFrame(&short, b[:256])
	if _, err := DecodeReply(&short); err == nil {
		t.Fatal("accepted v6 header on a v5-length frame")
	}
}

func TestBotPolicyRequest(t *testing.T) {
	for _, p := range []BotPolicy{{}, {Flags: BotBackfill, FillTo: 8, MaxBots: MaxBots, Difficulty: BotMarine},
		{Flags: BotBackfill, FillTo: MaxBotFill, MaxBots: 1, Difficulty: BotSpartan}, {FillTo: 0, MaxBots: 0}} {
		var w bytes.Buffer
		if err := EncodeRequest(&w, Request{Op: OpBotPolicy, ID: 2, Bots: p}); err != nil {
			t.Fatal(err)
		}
		if w.Len() != 4+64 {
			t.Fatalf("bot-policy frame is %d bytes", w.Len())
		}
		got, err := DecodeRequest(&w)
		if err != nil || got.Op != OpBotPolicy || got.Bots != p {
			t.Fatalf("round trip %+v: %+v %v", p, got, err)
		}
	}
	for _, bad := range []BotPolicy{{Flags: 2}, {Flags: BotBackfill, FillTo: 1, MaxBots: 8}, {Flags: BotBackfill, FillTo: 8},
		{FillTo: MaxBotFill + 1}, {MaxBots: MaxBots + 1}, {Difficulty: BotSpartan + 1}} {
		if err := EncodeRequest(io.Discard, Request{Op: OpBotPolicy, ID: 2, Bots: bad}); err == nil {
			t.Fatalf("invalid bot policy %+v accepted", bad)
		}
	}
}
