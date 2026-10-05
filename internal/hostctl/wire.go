package hostctl

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

const (
	OpHello                uint16 = 1
	OpStatus               uint16 = 2
	OpPrepare              uint16 = 3
	OpPrepareEngine        uint16 = 4
	OpInitialize           uint16 = 5
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
	case OpStatus:
		return 48, nil
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
	case OpStatus:
		if len(b) != 48 {
			return r, errors.New("invalid status size")
		}
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
	b, err := readFrame(rd, 112)
	if err != nil {
		return r, err
	}
	if string(b[:min(len(b), 4)]) != "HICR" || len(b) < 96 {
		return r, errors.New("invalid bridge reply header")
	}
	r.Version = binary.LittleEndian.Uint16(b[4:])
	switch {
	case r.Version == 1 && len(b) == 96:
	case r.Version == 2 && len(b) == 112:
		r.State = int32(binary.LittleEndian.Uint32(b[96:]))
		r.Matches = binary.LittleEndian.Uint32(b[100:])
		r.Flags = binary.LittleEndian.Uint32(b[104:])
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
