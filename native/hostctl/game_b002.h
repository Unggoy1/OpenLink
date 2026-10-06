#pragma once
#include <cstdint>
#include "lan_provider.h"

namespace hostctl {
// Engine lifecycle IDs from B002 name table 0x143cefcc0.
enum EngineState : int32_t {
    StateUnknown=-1,HostSetup=6,HostWaitForAllocation=7,HostPreGame=8,HostInGame=9,
    HostEndGame=10,HostStarting=11,HostTeardown=12
};
enum BackendFlag : uint32_t {
    FlagInstalled=1,FlagTicking=2,FlagCommandPending=4,FlagProviderReady=8,FlagDesiredActive=16,FlagFaulted=32
};
// GateContent/Applied remain reserved for content completion; Selected is only
// native provider readback. Wire support must be added before IPC integration.
// GateLocked: the server-owned selection lock is active (entry0 of every other
// provider write is replaced). GateIntercepted: the lock has rewritten at least
// one write from a non-hostctl caller (e.g. a lobby leader's request).
enum BackendGate : uint32_t { GateBuild=1,GateRole=2,GateDispatch=4,GateContent=8,GateLobby=16,GateSelection=32,
    GateLocked=64,GateIntercepted=128 };
enum BackendCode : uint16_t { CodeOK=0,CodeUnsupported=1,CodePending=2,CodeBusy=3,CodeInvalid=4,CodeFailed=5,CodeApplied=6,CodeSelected=7 };

enum LobbyFlag : uint32_t {
    LobbyValid=1,        // session/membership readable this tick
    LobbyStartMode=2,    // start-mode component validated; start_mode is its value
    LobbyHandler=4,      // pregame handler validated; handler bytes are live
    LobbyServerOwned=8,  // player start/end-game requests are dropped (BackendServerOwned mode 1 or 2)
    LobbyStartSent=16,   // a Start command set start mode in the current lobby
    LobbyNoOwner=32,     // join-time lobby-owner assignment is disabled (mode 1)
    LobbyLeaderValid=64, // leader component validated and set; leader is its value
    LobbyLeaderHeld=128  // the server's BackendSetLeader XUID is the current leader
};
// BackendServerOwned modes. FilterOnly keeps the game's own owner (first joiner,
// handed to the longest-present player by 142e1c454 when the owner leaves): that
// player's client still shows Play/End Game (inert), other clients hide them
// because an owner exists that is not them (O071, user report).
enum ServerOwnedMode : uint32_t { ServerOwnedOff=0,ServerOwnedNoOwner=1,ServerOwnedFilterOnly=2 };
// Read on the engine tick. B002 offsets (session = context+0x78): membership at
// session+0x60 (peer count +0xa0, mask +0xa4, owner +0x6c, host peer +0x70, peer
// state session+0xb0+i*0xc0, 8 = connected, as 142ded82c counts); session+0x18a8
// player count (142f4252c "NoPlayersConnected"); start mode = int component at
// simulation+0x541f0 (table 143d44a80, value +0xc8); context+0x280 allowed-to-start
// byte and +0xec users required (142fbe864); context+0xe8 host-init game type;
// pregame handler 144dc51e8 one-shot bytes +0x58/+0x68/+0x69/+0x74/+0x76.
struct LobbyProbe {
    uint32_t flags;
    int32_t connected;
    int32_t peers;
    uint32_t peer_mask;
    int32_t owner;
    int32_t host_peer;
    int32_t players;
    int32_t start_mode;
    uint8_t allowed,content_prepared,prep_started,prep_done,loading,start;
    uint8_t blocked_start,blocked_end; // player requests dropped by the server-owned filter (saturating)
    int32_t users_required;
    int32_t game_type;
    int32_t session_kind; // *(144c253b0)+8; selects the host-init content branch in 142fbc7c8
    int32_t end_game_table; // RVA of simulation+0xb4c4b0's table (end-game request), 0 unknown
    int32_t end_game;       // its value (+0xc8 byte), -1 unknown
    uint64_t leader;        // lobby leader XUID (simulation+0xb4ba60 +0xc8), 0 none/unknown
    uint32_t leader_sets;   // times the tick re-asserted the server's leader (saturating)
    // Team diagnostics (read-only, FFA/team investigation). Teams-enabled bytes are
    // variant+0x10bc: -1 when that variant is absent. lobby = simulation+0x542c0
    // component (+0xd0), game = loaded game globals (*145121d28 + idx*0x1134f0 + 0x28).
    int8_t lobby_variant_teams,game_variant_teams;
    uint8_t last_teams_enabled; // 144dcf9ac: teams-enabled the last team assignment used (140c82890)
    uint8_t team_fixes;         // FFA team guard corrections (TickTeamGuard), saturating
    int32_t last_team_count;    // 144dcf9b0: team count it used
    int32_t forced_team_count;  // 144dc340c: >0 forces team = peer % count at start (142fc426c)
    int32_t game_state;         // loaded game globals +0 (1404f178c tests 3), -1 unknown
    // Per peer 0-15 (session + i*0x1470): requested team +0x2505, assigned +0x2cf5,
    // host/selected +0x2cf6 (142ebfcbc). -1 also when the peer is absent.
    int8_t peer_team[16][3];
};

struct BackendReport {
    uint16_t code;
    uint32_t gates;
    uint64_t generation;      // selected command ticket, never content application
    ContentSelection observed; // provider entry0, native GUID order
    int32_t state;            // MP lifecycle state, StateUnknown before the first tick
    uint32_t flags;
    uint64_t ticks;
    uint32_t detail;          // install error or last ProviderResult
    uint32_t matches;         // transitions into HostInGame observed by the tick hook
    LobbyProbe lobby;         // last engine-tick lobby observation
};

// Verifies that the host process is the B002 dedicated LAN server and installs
// the server-tick hook. Returns ERROR_SUCCESS, ERROR_NOT_SUPPORTED (another
// executable or build), ERROR_INVALID_FUNCTION (not a LAN server) or a Win32
// error. Idempotent. Does not call native game functions on the calling thread.
// Installation changes a game vtable and requires coordinated runtime scope.
// One installation attempt after validation per process lifetime; after Stop,
// restarting requires a new server process, matching the bridge's single Start.
uint32_t InstallGameBackend() noexcept;

// IPC-side operations. They never call game code; the engine tick does.
BackendReport BackendStatus() noexcept;
// Queues one selection and waits at most2000ms for a lobby engine tick. A zero type means
// "keep the type currently in provider entry0". A Selected result also locks
// provider entry0 to the selected descriptors: later provider Set (+b0), apply (+78) and
// request (+b8) calls from game code keep their other entries but get entry0
// replaced, so a lobby leader cannot override the server's selection.
BackendReport BackendSelect(const ContentSelection& native,uint32_t wait_ms) noexcept;
// Starts the lobby's match the way a lobby leader's Play does (143578748 requests
// start mode 1): on the next HostPreGame engine tick, the start-mode component's
// authoritative Set (142e1d270) receives 1. The engine's own pregame checks still
// apply (content prepared, map precached, a connected player). Returns OK
// when Set accepted it, Busy outside HostPreGame, Pending if no lobby tick ran.
BackendReport BackendStart(uint32_t wait_ms) noexcept;
// Disables the server's join-time lobby-owner assignment: 1409cdbf0 gives the
// first joining peer ownership unless host-init is configured, and its predicate
// call at 14236f82b is replaced by "configured" (mov al,1). Players then never get
// the lobby leader role. Later joins only; an existing owner is unchanged.
// The server also applies any player's start request (143578748, start mode 1)
// and pause-menu end request (142ec0544, simulation+0xb4c4b0 = 1) whether or not
// that player owns the lobby (R019C). Those arrive through the component tables'
// +0x78 apply (int 143d44a80 -> 142e0d870, byte 143e06a20 -> 142e0dcb8); while
// enabled, applies to those two components are dropped. Server code uses Set
// (+0xb0), as BackendStart does, so server starts and natural ends still work.
// Returns ERROR_SUCCESS, ERROR_INVALID_FUNCTION (bytes differ) or a Win32 error.
// StopGameBackend restores the original bytes.
uint32_t BackendServerOwned(uint32_t mode) noexcept;
// Sets the name in the in-game server list (beacon_name.h): on the next engine
// tick the 48 zero-filled UTF-16 units replace the beacon object's PC name,
// after the object's fields are checked. OK when written and read back, Busy
// before the beacon started, Unsupported if the object differs, Pending if no
// tick ran within wait_ms (at most 2000).
BackendReport BackendSetName(const uint16_t* units,uint32_t wait_ms) noexcept;
// Holds the LAN lobby leader (FN027). Clients treat themselves as leader (lobby
// options, map/mode menus, Play/End Game) only when their XUID equals the qword
// component at simulation+0xb4ba60 (table 143d45040); the server gives it to the
// first joiner (142ebebdc, only while it is unset or 0) and passes it on when that
// player leaves (142e0e348). A nonzero xuid that no player has keeps every player
// a non-leader: every engine tick re-asserts it with the component's authoritative
// Set (142e1d39c) whenever the game's value differs. xuid 0 stops holding it and
// clears the component once, so the next joiner becomes leader again.
// OK when a tick holds (or cleared) it, Busy while no lobby simulation exists
// (it is still applied later), Unsupported if the component differs, Pending
// if no tick ran within wait_ms (at most 2000).
BackendReport BackendSetLeader(uint64_t xuid,uint32_t wait_ms) noexcept;
// Server team rules (team_guard.h TeamPolicyFlag), applied on the engine tick:
// TeamGuardFfa (on by default) keeps every player on its own team in FFA modes;
// TeamBalance puts the non-observers of a team mode on Eagle/Cobra alternately
// once per match before spawn and clears carried-over team requests.
// ERROR_INVALID_PARAMETER for unknown flags.
uint32_t BackendTeamPolicy(uint32_t flags) noexcept;
// Cancels pending work and restores our table slot; module remains pinned so
// a callback already fetched by another thread still has a valid target.
uint32_t StopGameBackend() noexcept;
}
