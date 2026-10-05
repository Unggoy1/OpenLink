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
// Cancels pending work and restores our table slot; module remains pinned so
// a callback already fetched by another thread still has a valid target.
uint32_t StopGameBackend() noexcept;
}
