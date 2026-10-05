#define WIN32_LEAN_AND_MEAN
#include <windows.h>
#include "tick_slot.h"
#include <cstdio>
namespace {
int failures=0,protect_calls=0; unsigned fail_mask=0;
void check(bool ok,const char* text) { if(!ok) { ++failures; std::fprintf(stderr,"FAIL %s\n",text); } }
BOOL WINAPI protect(void* address,SIZE_T size,DWORD flags,DWORD* old) {
    ++protect_calls; if(fail_mask&(1u<<unsigned(protect_calls))) { SetLastError(ERROR_ACCESS_DENIED); return FALSE; }
    return VirtualProtect(address,size,flags,old);
}
void* exchange(void* volatile* slot,void* replacement,void* expected) { return InterlockedCompareExchangePointer(slot,replacement,expected); }
}
int main() {
    auto slot=static_cast<void**>(VirtualAlloc(nullptr,4096,MEM_COMMIT|MEM_RESERVE,PAGE_READWRITE)); if(!slot) return 9;
    void* original=reinterpret_cast<void*>(123); void* hook=reinterpret_cast<void*>(456);
    *slot=original; DWORD old=0; VirtualProtect(slot,4096,PAGE_READONLY,&old);
    hostctl::SlotFunctions functions={protect,exchange};
    hostctl::SlotProtection state={};
    check(hostctl::ReplaceTickSlot(slot,original,hook,state,functions)==ERROR_SUCCESS && *slot==hook && !state.pending,"normal exchange/protection restore");
    check(hostctl::ReplaceTickSlot(slot,hook,original,state,functions)==ERROR_SUCCESS && *slot==original,"normal stop restore");
    fail_mask=1u<<1; protect_calls=0;
    check(hostctl::ReplaceTickSlot(slot,original,hook,state,functions)==ERROR_ACCESS_DENIED && *slot==original,"initial protect failure unchanged");
    fail_mask=(1u<<2)|(1u<<3); protect_calls=0;
    check(hostctl::ReplaceTickSlot(slot,original,hook,state,functions)==ERROR_ACCESS_DENIED && *slot==original && state.pending,"failed restore rolls pointer back and retains cleanup");
    fail_mask=0; check(hostctl::RestoreTickProtection(slot,state,functions)==ERROR_SUCCESS && !state.pending,"later cleanup retries original page protection");
    check(hostctl::ReplaceTickSlot(slot,hook,original,state,functions)==ERROR_INVALID_DATA && *slot==original,"foreign slot is never overwritten");
    MEMORY_BASIC_INFORMATION region={}; VirtualQuery(slot,&region,sizeof(region)); check(region.Protect==PAGE_READONLY,"original protection retained");
    VirtualFree(slot,0,MEM_RELEASE); std::printf("slot tests failures=%d\n",failures); return failures ? 1 : 0;
}
