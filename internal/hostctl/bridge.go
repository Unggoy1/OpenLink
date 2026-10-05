package hostctl

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"net"
	"sync/atomic"
	"time"
)

// Bridge is one authenticated, serialized connection to a server-process DLL.
// It does not load a DLL, launch a game or treat transport success as application.
type Bridge struct {
	conn         net.Conn
	token        [32]byte
	pid          uint32
	serial       chan struct{}
	nextID       uint64
	lastApplied  uint64
	lastSelected uint64
	closed       atomic.Bool
}

func armIO(ctx context.Context, c net.Conn, closeConn func() error) (func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	deadline := time.Now().Add(5 * time.Second)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	if err := c.SetDeadline(deadline); err != nil {
		return nil, err
	}
	stop := context.AfterFunc(ctx, func() { closeConn() })
	return func() { stop(); c.SetDeadline(time.Time{}) }, nil
}

func AcceptBridge(ctx context.Context, c net.Conn, token [32]byte, expectedPID uint32) (*Bridge, error) {
	var nonzero byte
	for _, b := range token {
		nonzero |= b
	}
	if expectedPID == 0 || nonzero == 0 {
		c.Close()
		return nil, errors.New("bridge requires launch secret and expected PID")
	}
	cleanup, err := armIO(ctx, c, c.Close)
	if err != nil {
		c.Close()
		return nil, err
	}
	defer cleanup()
	ok := false
	defer func() {
		if !ok {
			c.Close()
		}
	}()
	r, err := DecodeRequest(c)
	if err != nil {
		return nil, err
	}
	if r.Op != OpHello || r.PID != expectedPID || subtle.ConstantTimeCompare(r.Token[:], token[:]) != 1 {
		return nil, errors.New("bridge authentication failed")
	}
	if err := EncodeReply(c, Reply{Code: CodeOK, ID: r.ID, PID: expectedPID}); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	ok = true
	return &Bridge{conn: c, token: token, pid: expectedPID, nextID: r.ID, serial: make(chan struct{}, 1)}, nil
}

func (b *Bridge) Close() error { b.closed.Store(true); return b.conn.Close() }

// Closed reports a terminal transport failure or explicit shutdown. Canceling
// a queued request before I/O leaves the connection usable.
func (b *Bridge) Closed() bool { return b.closed.Load() }
func (b *Bridge) Status(ctx context.Context) (Reply, error) {
	return b.exchange(ctx, OpStatus, AssetPair{})
}
func (b *Bridge) Prepare(ctx context.Context, pair AssetPair) (Reply, error) {
	if !pair.Valid() {
		return Reply{}, errors.New("map and mode require nonzero asset and version IDs")
	}
	return b.exchange(ctx, OpPrepare, pair)
}

// PrepareEngine selects a base engine-game-variant (native type10). Prepare
// selects a UGC game-variant (type6), including version-specific custom modes.
// CodeSelected establishes only native provider selection, never content load.
func (b *Bridge) PrepareEngine(ctx context.Context, pair AssetPair) (Reply, error) {
	if !pair.Valid() {
		return Reply{}, errors.New("map and mode require nonzero asset and version IDs")
	}
	return b.exchange(ctx, OpPrepareEngine, pair)
}

// Initialize explicitly supplies API-derived map type2 and UGC mode type6.
// Native Set initializes the provider; Selected is only descriptor readback.
func (b *Bridge) Initialize(ctx context.Context, pair AssetPair) (Reply, error) {
	if !pair.Valid() {
		return Reply{}, errors.New("map and mode require nonzero asset and version IDs")
	}
	return b.exchange(ctx, OpInitialize, pair)
}

// Start asks the server to start its lobby's match, as a lobby leader's Play
// does. CodeOK means the engine accepted start mode 1; the engine's own
// pregame checks (content prepared, a connected player) still decide when the
// match loads. CodeBusy means the server was not in HostPreGame.
func (b *Bridge) Start(ctx context.Context) (Reply, error) {
	return b.exchange(ctx, OpStart, AssetPair{})
}

// ServerOwned sets the lobby control mode (ServerOwned* constants). Both
// non-zero modes drop players' start and end-game requests; ServerOwnedNoOwner
// also stops the first joiner becoming lobby owner. Send it before players
// join. CodeOK on success; CodeUnsupported if the code bytes differ.
func (b *Bridge) ServerOwned(ctx context.Context, mode uint32) (Reply, error) {
	return b.send(ctx, Request{Op: OpServerOwned, Mode: mode})
}

// SetName sets the server's name in the in-game server list (Custom Game →
// Create Match → Server), which otherwise shows the PC name. Later beacons
// carry it. name must pass ValidName (use api.GameName). CodeOK once the
// engine tick wrote it; CodePending if no tick ran; CodeUnsupported if the
// game's beacon object did not match the expected layout.
func (b *Bridge) SetName(ctx context.Context, name string) (Reply, error) {
	if !ValidName(name) {
		return Reply{}, errors.New("server name must be 1-47 printable ASCII characters")
	}
	return b.send(ctx, Request{Op: OpSetName, Name: name})
}

func (b *Bridge) exchange(ctx context.Context, op uint16, pair AssetPair) (Reply, error) {
	return b.send(ctx, Request{Op: op, Pair: pair})
}

func (b *Bridge) send(ctx context.Context, req Request) (Reply, error) {
	op, pair := req.Op, req.Pair
	select {
	case b.serial <- struct{}{}:
	case <-ctx.Done():
		return Reply{}, ctx.Err()
	}
	defer func() { <-b.serial }()
	cleanup, err := armIO(ctx, b.conn, b.Close)
	if err != nil {
		if ctx.Err() == nil {
			b.Close()
		}
		return Reply{}, err
	}
	defer cleanup()
	fail := func(err error) (Reply, error) {
		b.Close()
		if ctx.Err() != nil {
			return Reply{}, ctx.Err()
		}
		return Reply{}, fmt.Errorf("bridge: %w", err)
	}
	if b.nextID == ^uint64(0) {
		return fail(errors.New("request IDs exhausted"))
	}
	b.nextID++
	req.ID, req.Token = b.nextID, b.token
	if err := EncodeRequest(b.conn, req); err != nil {
		return fail(err)
	}
	r, err := DecodeReply(b.conn)
	if err != nil {
		return fail(err)
	}
	if r.ID != b.nextID || r.PID != b.pid {
		return fail(errors.New("reply identity mismatch"))
	}
	if r.Code == CodeApplied {
		if (op != OpPrepare && op != OpPrepareEngine && op != OpInitialize) || r.Gates&RequiredGates != RequiredGates || r.Generation <= b.lastApplied || r.Generation <= b.lastSelected || r.Pair != pair {
			return fail(errors.New("unverified application acknowledgement"))
		}
		b.lastApplied = r.Generation
	}
	if r.Code == CodeSelected {
		if (op != OpPrepare && op != OpPrepareEngine && op != OpInitialize) || r.Gates&RequiredSelectionGates != RequiredSelectionGates || r.Gates&GateContent != 0 || r.Generation <= b.lastSelected || r.Generation <= b.lastApplied || r.Pair != pair {
			return fail(errors.New("unverified selection acknowledgement"))
		}
		b.lastSelected = r.Generation
	}
	if err := ctx.Err(); err != nil {
		return fail(err)
	}
	return r, nil
}
