#define WIN32_LEAN_AND_MEAN
#include "tick_slot.h"
namespace hostctl {
DWORD RestoreTickProtection(void* volatile* slot,SlotProtection& state,const SlotFunctions& functions) noexcept {
    if(!slot || !functions.Protect) return ERROR_INVALID_PARAMETER;
    if(!state.pending) return ERROR_SUCCESS;
    DWORD ignored=0;
    if(!functions.Protect(const_cast<void**>(slot),sizeof(void*),state.original,&ignored)) return GetLastError();
    state.pending=false; return ERROR_SUCCESS;
}
DWORD ReplaceTickSlot(void* volatile* slot,void* expected,void* replacement,
    SlotProtection& state,const SlotFunctions& functions) noexcept {
    if(!slot || !expected || !replacement || !functions.Protect || !functions.Exchange) return ERROR_INVALID_PARAMETER;
    DWORD old=0;
    if(!functions.Protect(const_cast<void**>(slot),sizeof(void*),PAGE_READWRITE,&old)) return GetLastError();
    if(!state.pending) state.original=old;
    state.pending=true;
    const auto previous=functions.Exchange(slot,replacement,expected);
    const DWORD result=RestoreTickProtection(slot,state,functions);
    if(result!=ERROR_SUCCESS) {
        // Restoration failed, so the page is still writable. Undo only our own
        // exchange, preserving a pointer another component may have installed.
        if(previous==expected) functions.Exchange(slot,expected,replacement);
        RestoreTickProtection(slot,state,functions); // retain pending if retry fails
        return result;
    }
    return previous==expected ? ERROR_SUCCESS : ERROR_INVALID_DATA;
}
}
