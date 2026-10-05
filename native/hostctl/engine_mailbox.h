#pragma once
#include <windows.h>
#include "game_b002.h"

namespace hostctl {
// Internal engine observation; never supplied over IPC. Binder validates build,
// role, receiver/provider lifetime and callback ownership before constructing it.
struct EngineFrame {
    uint32_t gates;
    int32_t state;
    uint32_t owner_thread;
    void* provider;
    uintptr_t provider_table;
    ProviderFunctions functions;
};
class CommandMailbox {
public:
    CommandMailbox() noexcept;
    void Enable() noexcept;
    void Stop() noexcept;
    uint64_t Submit(const ContentSelection& selection,uint16_t* rejection=nullptr) noexcept;
    bool Cancel(uint64_t ticket) noexcept;
    BackendReport Wait(uint64_t ticket,uint32_t milliseconds) noexcept;
    BackendReport Status() noexcept;
    void Tick(const EngineFrame& frame) noexcept;
private:
    void Complete(uint16_t code) noexcept; // exclusive lock held
    SRWLOCK lock_=SRWLOCK_INIT;
    CONDITION_VARIABLE changed_=CONDITION_VARIABLE_INIT;
    bool enabled_=false,pending_=false,applying_=false,uncollected_=false,waiter_departed_=false;
    uint64_t sequence_=0,ticket_=0,completed_ticket_=0;
    ContentSelection requested_={};
    BackendReport status_={},completed_={};
    int32_t last_state_=StateUnknown;
};
}
