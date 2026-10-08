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
    LobbyServerOwned=8,  // player start/end-game requests are dropped (BackendServerOwned mode 1)
    LobbyStartSent=16,   // a Start command set start mode in the current lobby
    LobbyNoOwner=32,     // join-time lobby-owner assignment is disabled (BackendServerOwned mode 1)
    LobbyLeaderValid=64, // leader component validated and set; leader is its value
    LobbyLeaderHeld=128  // the server's BackendSetLeader XUID is the current leader
};
// BackendServerOwned modes. NoOwner drops players' start/end-game requests and
// disables the join-time lobby owner; Off restores the stock lobby.
enum ServerOwnedMode : uint32_t { ServerOwnedOff=0,ServerOwnedNoOwner=1 };
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
    // Per session player 0-15 (session + i*0x1470, present in player_mask):
    // requested team +0x2505, assigned +0x2cf5, host/selected +0x2cf6 (142ebfcbc).
    // -1 also when the player is absent.
    int8_t peer_team[16][3];
    // Bot backfill (FN029). Variant bytes are the loaded game options (+0x229
    // botsEnabled, -1 unknown); mode_bots is set when the mode spawns or backfills
    // bots itself (BotModeBackfill/BotModeTeams/BotModeFfa), and backfill then
    // stays out. The rest is the last bot tick (the engine's bot job, which runs
    // only while bots are enabled): counts, BotTickState, the tick thread's role
    // in the engine thread registry (1 = main), and our changes (saturating).
    int8_t bots_enabled;
    uint8_t mode_bots;
    int8_t bot_count,bot_humans;
    uint8_t bot_state;
    int8_t bot_thread_role;
    uint16_t bot_adds,bot_removes,bot_refused;
    uint32_t bot_ticks;
    uint8_t bot_supported; // bot job hooked and native bot functions verified
    int8_t nav_state;       // loaded map bot navigation: -1 unknown, 0 none, 1 present (A074)
    uint8_t nav_from_variant; // 144708629: navigation came from the map variant (Forge)
    int32_t nav_faces;      // navmesh faces, -1 unreadable
    uint16_t bot_difficulties; // bit i: difficulty code i registered by the mode (9,6,7,8 = recruit..spartan)
    // Session players (A082): the player mask session+0x18ac, which the game's
    // own team rules iterate (1404e67a8); player i's record is session + i*0x1470.
    // A player's slot can differ from its connection (peer_mask) slot.
    uint32_t player_mask;
    uint32_t ticks;            // server ticks seen by the hook (saturating), for the stuck-server watchdog
    uint16_t name_fixes;       // times the tick rewrote a changed server name (EditLobbyName), saturating
    uint16_t blocked_restart;  // player Restart Match requests dropped (event 0x58), saturating
    uint16_t rejoin_teams;     // rejoining players put back on their team this match, saturating
    uint8_t restart_guard;     // 1: the Restart Match handler is hooked (server-owned lobby)
    uint8_t end_match;         // 1: BackendEndMatch is available (end-game Set verified)
};
enum BotModeFlag : uint8_t { BotModeBackfill=1,BotModeTeams=2,BotModeFfa=4 };
enum BotTickState : uint8_t {
    BotStateNone=0,     // no bot tick yet
    BotStateOff=1,      // policy has backfill off for this match
    BotStateModeBots=2, // the mode manages its own bots
    BotStateWaiting=3,  // the engine's bot gate is closed (joins or bot changes in progress)
    BotStateFilled=4,   // at the wanted bot count
    BotStateChanged=5,  // added or removed a bot this tick
    BotStateRefused=6,  // the engine did not create the bot
    BotStateNoNavmesh=7 // the map has no bot navigation: no bots added, ours removed
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
// A player's Restart Match (pause menu, client simulation event 0x58, A080c/O099)
// reaches the server as the event's apply function 142ef7394 (descriptor table
// 143d09ba8 +0x78, next to its 1-byte payload reader 142ef8b5c); the server's own
// restart calls 1429f1a4c directly. While enabled, that slot holds a hook that
// drops the request. The hook is optional: if the slot or function bytes differ,
// the rest still works and LobbyProbe::restart_guard stays 0.
// Returns ERROR_SUCCESS, ERROR_INVALID_FUNCTION (bytes differ) or a Win32 error.
// StopGameBackend restores the original bytes.
uint32_t BackendServerOwned(uint32_t mode) noexcept;
// Ends the running match the way a lobby leader's pause-menu End Game does
// (simulation+0xb4c4b0 = 1), through the byte component's authoritative Set
// (142e1d548) on the next HostInGame engine tick. OK when Set accepted it and
// the value reads back, Busy outside HostInGame, Unsupported if the component
// differs, Pending if no tick ran within wait_ms (at most 2000).
BackendReport BackendEndMatch(uint32_t wait_ms) noexcept;
// Sets the name in the in-game server list (beacon_name.h): on the next engine
// tick the 48 zero-filled UTF-16 units replace the beacon object's PC name,
// after the object's fields are checked. OK when written and read back, Busy
// before the beacon started, Unsupported if the object differs, Pending if no
// tick ran within wait_ms (at most 2000). Once set, every tick puts the name
// back if it changed: the server applies any player's EditLobbyName (LAN
// message 0x2e) to the beacon name (FN030, O099).
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
// TeamBalance spreads the non-observers of a team mode evenly over the match's
// teams once per match before spawn and clears carried-over team requests.
// mode is TeamModeEven or TeamModeShuffle; count (0-8) and size (0-32) are the
// playlist entry's team count / team size, 0 when not given (TeamCount). They
// apply to the next match only (cleared at its prep), so send them with every
// selection; flags and mode persist.
// ERROR_INVALID_PARAMETER for unknown flags or out-of-range values.
uint32_t BackendTeamPolicy(uint32_t flags,uint32_t mode,uint32_t count,uint32_t size) noexcept;
// Bot backfill (bot_backfill.h, FN029): with BotBackfill, the engine's bot job
// (it runs each game tick while the match's mode has bots enabled) tops the
// match up to fill_to players with at most max_bots bots of a BotDifficulty,
// and removes one when a player joins, one change at a time and only while the
// engine's own bot gate (142c2b850) allows changes. Bots are created with the
// script native 142a171c8 and removed with 142c2d090, as content scripts do.
// Modes that spawn or backfill bots themselves are left alone. Persists until
// changed; send it with every selection. ERROR_INVALID_PARAMETER for unknown
// flags or out-of-range values.
uint32_t BackendBotPolicy(uint32_t flags,uint32_t fill_to,uint32_t max_bots,uint32_t difficulty) noexcept;
// Cancels pending work and restores our table slot; module remains pinned so
// a callback already fetched by another thread still has a valid target.
uint32_t StopGameBackend() noexcept;
}
