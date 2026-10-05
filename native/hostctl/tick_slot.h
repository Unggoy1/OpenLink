#pragma once
#include <windows.h>
namespace hostctl {
struct SlotProtection { DWORD original=0; bool pending=false; };
struct SlotFunctions {
    BOOL (WINAPI *Protect)(void*,SIZE_T,DWORD,DWORD*);
    void* (*Exchange)(void* volatile*,void*,void*);
};
// Internal exact-slot exchange after module validation. No address from IPC.
// Shared protection state must be serialized by the installation lock.
DWORD ReplaceTickSlot(void* volatile* slot,void* expected,void* replacement,
    SlotProtection& state,const SlotFunctions& functions) noexcept;
DWORD RestoreTickProtection(void* volatile* slot,SlotProtection& state,const SlotFunctions& functions) noexcept;
}
