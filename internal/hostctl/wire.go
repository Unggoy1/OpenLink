package hostctl

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"

	"halocommunity/internal/api"
)

const (
	OpHello                uint16 = 1
	OpStatus               uint16 = 2
	OpPrepare              uint16 = 3
	OpPrepareEngine        uint16 = 4
	OpInitialize           uint16 = 5
	OpStart                uint16 = 6 // start the lobby's match (start mode 1), HostPreGame only
	OpServerOwned          uint16 = 7 // lobby control mode (ServerOwned* constants)
	OpSetName              uint16 = 8 // name in the in-game server list (ValidName)
	CodeOK                 uint16 = 0
	CodeUnsupported        uint16 = 1
	CodeNativePending      uint16 = 2
	CodeBusy               uint16 = 3
	CodeInvalid            uint16 = 4
	CodeFailed             uint16 = 5
	CodeApplied            uint16 = 6
	CodeSelected           uint16 = 7
	GateBuild              uint32 = 1
	GateRole               uint32 = 2
	GateDispatch           uint32 = 4
	GateContent            uint32 = 8
	GateLobby              uint32 = 16
	GateSelection          uint32 = 32
	GateLocked             uint32 = 64  // server-owned selection lock active
	GateIntercepted        uint32 = 128 // lock rewrote another writer's entry0
	RequiredGates                 = GateBuild | GateRole | GateDispatch | GateContent | GateLobby
	RequiredSelectionGates        = GateBuild | GateRole | GateDispatch | GateLobby | GateSelection
	KnownGates                    = RequiredGates | GateSelection | GateLocked | GateIntercepted
)

// AssetPair contains map asset, map version, mode asset and mode version,
// in that order. Each is sixteen RFC UUID bytes, not a native GUID layout.
type AssetPair [64]byte

func (p AssetPair) Valid() bool {
	for base := 0; base < len(p); base += 16 {
		var nonzero byte
		for _, b := range p[base : base+16] {
			nonzero |= b
		}
		if nonzero == 0 {
			return false
		}
	}
	return true
}

type Request struct {
	Op    uint16
	ID    uint64
	Token [32]byte
	PID   uint32
	Pair  AssetPair
	Mode  uint32 // OpServerOwned
	Name  string // OpSetName
}

// MaxNameLength is the longest in-game server name (api.GameName).
const MaxNameLength = api.MaxGameNameLength

// ValidName reports whether s can be sent with OpSetName: 1-47 printable
// ASCII characters (space through tilde).
func ValidName(s string) bool {
	if len(s) == 0 || len(s) > MaxNameLength {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < 0x20 || s[i] > 0x7e {
			return false
		}
	}
	return true
}


// OpServerOwned modes.
const (
	ServerOwnedOff         uint32 = 0 // stock lobby: owner assigned, player requests applied
	ServerOwnedNoOwner     uint32 = 1 // no owner; player start/end-game requests dropped
	ServerOwnedFilterOnly  uint32 = 2 // game assigns the owner; player start/end-game requests dropped
	serverOwnedHighestMode        = ServerOwnedFilterOnly
)

// Lobby flags (Reply.Lobby.Flags).
const (
	LobbyValid       uint32 = 1  // session membership was readable
	LobbyStartMode   uint32 = 2  // StartMode holds the validated start-mode value
	LobbyHandler     uint32 = 4  // pregame handler bytes are live
	LobbyServerOwned uint32 = 8  // join-time lobby-owner assignment is disabled
	LobbyStartSent   uint32 = 16 // a Start succeeded since the last match began
	LobbyNoOwner     uint32 = 32 // join-time lobby-owner assignment is disabled
)

// Lobby is the DLL's per-tick lobby observation (version 3 replies).
type Lobby struct {
	Flags         uint32 `json:"flags"`
	Connected     int32  `json:"connected"`      // peers in the connected state
	Peers         int32  `json:"peers"`          // membership peer count
	PeerMask      uint32 `json:"peer_mask"`      // membership peer bits
	Owner         int32  `json:"owner"`          // lobby owner (leader) peer, -1 none
	HostPeer      int32  `json:"host_peer"`      // membership host peer
	Players       int32  `json:"players"`        // session player count
	StartMode     int32  `json:"start_mode"`     // 0 none, 1 custom start requested; -1 unknown
	Allowed       uint8  `json:"allowed"`        // allowed-to-start byte
	Prepared      uint8  `json:"prepared"`       // pregame handler one-shot bytes
	PrepStarted   uint8  `json:"prep_started"`   //
	PrepDone      uint8  `json:"prep_done"`      //
	Loading       uint8  `json:"loading"`        //
	Start         uint8  `json:"start"`          // set when the engine decided to start
	BlockedStart  uint8  `json:"blocked_start"`  // player start requests dropped (server-owned)
	BlockedEnd    uint8  `json:"blocked_end"`    // player end-game requests dropped (server-owned)
	UsersRequired int32  `json:"users_required"` // host-init users required to start
	GameType      int32  `json:"game_type"`      // host-init game type
	SessionKind   int32  `json:"session_kind"`
	EndGameTable  int32  `json:"end_game_table"` // RVA of the end-game component table
	EndGame       int32  `json:"end_game"`       // end-game request value, -1 unknown
}

type Reply struct {
	Code       uint16
	ID         uint64
	PID        uint32
	Gates      uint32
	Generation uint64
	// Version 2 replies (native backend) also carry the engine lifecycle state
	// (8 HostPreGame, 11 HostStarting, 9 HostInGame, 10 HostEndGame), the number
	// of transitions into HostInGame, and backend flags. Zero in version 1.
	Version uint16
	State   int32
	Matches uint32
	Flags   uint32
	Pair    AssetPair
	// Version 3 replies add the lobby observation.
	Lobby Lobby
}

func requestSize(r Request) (int, error) {
	if r.ID == 0 {
		return 0, errors.New("zero request ID")
	}
	switch r.Op {
	case OpHello:
		if r.PID == 0 {
			return 0, errors.New("zero server PID")
		}
		return 52, nil
	case OpStatus, OpStart:
		return 48, nil
	case OpServerOwned:
		if r.Mode > serverOwnedHighestMode {
			return 0, errors.New("invalid server-owned mode")
		}
		return 52, nil
	case OpSetName:
		if !ValidName(r.Name) {
			return 0, errors.New("server name must be 1-47 printable ASCII characters")
		}
		return 48 + MaxNameLength + 1, nil
	case OpPrepare, OpPrepareEngine, OpInitialize:
		if !r.Pair.Valid() {
			return 0, errors.New("map and mode require nonzero asset and version IDs")
		}
		return 112, nil
	default:
		return 0, errors.New("unknown bridge operation")
	}
}

func EncodeRequest(w io.Writer, r Request) error {
	n, err := requestSize(r)
	if err != nil {
		return err
	}
	b := make([]byte, n)
	copy(b, "HICT")
	binary.LittleEndian.PutUint16(b[4:], 1)
	binary.LittleEndian.PutUint16(b[6:], r.Op)
	binary.LittleEndian.PutUint64(b[8:], r.ID)
	copy(b[16:48], r.Token[:])
	if r.Op == OpHello {
		binary.LittleEndian.PutUint32(b[48:], r.PID)
	}
	if r.Op == OpPrepare || r.Op == OpPrepareEngine || r.Op == OpInitialize {
		copy(b[48:], r.Pair[:])
	}
	if r.Op == OpServerOwned {
		binary.LittleEndian.PutUint32(b[48:], r.Mode)
	}
	if r.Op == OpSetName {
		copy(b[48:], r.Name) // zero-padded; the last byte stays the terminator
	}
	return writeFrame(w, b)
}

func DecodeRequest(rd io.Reader) (Request, error) {
	var r Request
	b, err := readFrame(rd, 112)
	if err != nil {
		return r, err
	}
	if len(b) < 48 || string(b[:4]) != "HICT" || binary.LittleEndian.Uint16(b[4:]) != 1 {
		return r, errors.New("invalid bridge request header")
	}
	r.Op = binary.LittleEndian.Uint16(b[6:])
	r.ID = binary.LittleEndian.Uint64(b[8:])
	copy(r.Token[:], b[16:48])
	switch r.Op {
	case OpHello:
		if len(b) != 52 {
			return r, errors.New("invalid hello size")
		}
		r.PID = binary.LittleEndian.Uint32(b[48:])
	case OpStatus, OpStart:
		if len(b) != 48 {
			return r, errors.New("invalid status size")
		}
	case OpServerOwned:
		if len(b) != 52 {
			return r, errors.New("invalid server-owned size")
		}
		if r.Mode = binary.LittleEndian.Uint32(b[48:]); r.Mode > serverOwnedHighestMode {
			return r, errors.New("invalid server-owned mode")
		}
	case OpSetName:
		if len(b) != 48+MaxNameLength+1 {
			return r, errors.New("invalid set-name size")
		}
		field := b[48:]
		n := 0
		for n < len(field) && field[n] != 0 {
			n++
		}
		for _, c := range field[n:] {
			if c != 0 {
				return r, errors.New("set-name padding must be zero")
			}
		}
		r.Name = string(field[:n])
	case OpPrepare, OpPrepareEngine, OpInitialize:
		if len(b) != 112 {
			return r, errors.New("invalid prepare size")
		}
		copy(r.Pair[:], b[48:])
	default:
		return r, errors.New("unknown bridge operation")
	}
	_, err = requestSize(r)
	return r, err
}

func EncodeReply(w io.Writer, r Reply) error {
	if r.ID == 0 || r.PID == 0 || r.Code > CodeSelected || r.Gates&^KnownGates != 0 {
		return errors.New("invalid bridge reply")
	}
	b := make([]byte, 96)
	copy(b, "HICR")
	binary.LittleEndian.PutUint16(b[4:], 1)
	binary.LittleEndian.PutUint16(b[6:], r.Code)
	binary.LittleEndian.PutUint64(b[8:], r.ID)
	binary.LittleEndian.PutUint32(b[16:], r.PID)
	binary.LittleEndian.PutUint32(b[20:], r.Gates)
	binary.LittleEndian.PutUint64(b[24:], r.Generation)
	copy(b[32:], r.Pair[:])
	return writeFrame(w, b)
}

func DecodeReply(rd io.Reader) (Reply, error) {
	var r Reply
	b, err := readFrame(rd, 176)
	if err != nil {
		return r, err
	}
	if string(b[:min(len(b), 4)]) != "HICR" || len(b) < 96 {
		return r, errors.New("invalid bridge reply header")
	}
	r.Version = binary.LittleEndian.Uint16(b[4:])
	switch {
	case r.Version == 1 && len(b) == 96:
	case r.Version == 2 && len(b) == 112, r.Version == 3 && len(b) == 176:
		r.State = int32(binary.LittleEndian.Uint32(b[96:]))
		r.Matches = binary.LittleEndian.Uint32(b[100:])
		r.Flags = binary.LittleEndian.Uint32(b[104:])
		if r.Version == 3 {
			r.Lobby = decodeLobby(b[112:])
		}
	default:
		return r, errors.New("invalid bridge reply header")
	}
	r.Code = binary.LittleEndian.Uint16(b[6:])
	r.ID = binary.LittleEndian.Uint64(b[8:])
	r.PID = binary.LittleEndian.Uint32(b[16:])
	r.Gates = binary.LittleEndian.Uint32(b[20:])
	r.Generation = binary.LittleEndian.Uint64(b[24:])
	copy(r.Pair[:], b[32:])
	if r.ID == 0 || r.PID == 0 || r.Code > CodeSelected || r.Gates&^KnownGates != 0 {
		return r, errors.New("invalid bridge reply fields")
	}
	return r, nil
}

func decodeLobby(b []byte) Lobby {
	i32 := func(at int) int32 { return int32(binary.LittleEndian.Uint32(b[at:])) }
	return Lobby{
		Flags: binary.LittleEndian.Uint32(b[0:]), Connected: i32(4), Peers: i32(8),
		PeerMask: binary.LittleEndian.Uint32(b[12:]), Owner: i32(16), HostPeer: i32(20),
		Players: i32(24), StartMode: i32(28), Allowed: b[32], Prepared: b[33], PrepStarted: b[34],
		PrepDone: b[35], Loading: b[36], Start: b[37], BlockedStart: b[38], BlockedEnd: b[39],
		UsersRequired: i32(40), GameType: i32(44), EndGameTable: i32(52), EndGame: i32(56),
		SessionKind: i32(48),
	}
}

func readFrame(rd io.Reader, max uint32) ([]byte, error) {
	var length [4]byte
	if _, err := io.ReadFull(rd, length[:]); err != nil {
		return nil, err
	}
	n := binary.LittleEndian.Uint32(length[:])
	if n == 0 || n > max {
		return nil, fmt.Errorf("bridge frame length %d exceeds bounds", n)
	}
	b := make([]byte, n)
	_, err := io.ReadFull(rd, b)
	return b, err
}

func writeFrame(w io.Writer, b []byte) error {
	var length [4]byte
	binary.LittleEndian.PutUint32(length[:], uint32(len(b)))
	for _, data := range [][]byte{length[:], b} {
		for len(data) > 0 {
			n, err := w.Write(data)
			if err != nil {
				return err
			}
			if n <= 0 || n > len(data) {
				return io.ErrShortWrite
			}
			data = data[n:]
		}
	}
	return nil
}
