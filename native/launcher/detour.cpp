// Inline detours (see detour.h). Same technique as the proven text hook (O159).
#define WIN32_LEAN_AND_MEAN
#include <windows.h>
#include "detour.h"
#include <cstring>
namespace clientjoin {
namespace detour {
namespace {
SRWLOCK lock=SRWLOCK_INIT;
uint8_t* page=nullptr; size_t used=0;
constexpr size_t kPage=0x1000,kSlot=0x40;
uint8_t* NearPage(uintptr_t image) {
    SYSTEM_INFO info; GetSystemInfo(&info);
    const uintptr_t granularity=info.dwAllocationGranularity;
    for(uintptr_t distance=granularity*16;distance<0x40000000;distance+=granularity) {
        for(int side=0;side<2;++side) {
            const uintptr_t at=side ? ((image+distance)&~(granularity-1)) : ((image-distance)&~(granularity-1));
            if(void* p=VirtualAlloc(reinterpret_cast<void*>(at),kPage,MEM_RESERVE|MEM_COMMIT,PAGE_EXECUTE_READWRITE)) return static_cast<uint8_t*>(p);
        }
    }
    return nullptr;
}
void AbsoluteJump(uint8_t* at,const void* target) { at[0]=0xff; at[1]=0x25; std::memset(at+2,0,4); const uintptr_t t=reinterpret_cast<uintptr_t>(target); std::memcpy(at+6,&t,8); }
}
bool Replace(uintptr_t image,uintptr_t rva,const uint8_t* expected,size_t expected_length,const void* handler,uint32_t* error) noexcept {
    // Install with relocate 0: the slot's trampoline half is never built or used.
    return Install(image,rva,expected,expected_length,0,handler,error)!=nullptr;
}
void* Install(uintptr_t image,uintptr_t rva,const uint8_t* expected,size_t expected_length,size_t relocate,const void* handler,uint32_t* error) noexcept {
    auto fail=[&](uint32_t e)->void* { if(error) *error=e; return nullptr; };
    const bool replace=relocate==0; // Replace(): jump to handler only, no trampoline
    if(!image || !expected || expected_length<relocate || expected_length<5 || (!replace && (relocate<5 || relocate>16)) || !handler) return fail(ERROR_INVALID_PARAMETER);
    const uintptr_t function=image+rva;
    if(std::memcmp(reinterpret_cast<const void*>(function),expected,expected_length)) return fail(ERROR_BAD_EXE_FORMAT);
    AcquireSRWLockExclusive(&lock);
    if(!page) page=NearPage(image);
    if(!page || used+kSlot>kPage) { ReleaseSRWLockExclusive(&lock); return fail(ERROR_NOT_ENOUGH_MEMORY); }
    uint8_t* slot=page+used; used+=kSlot;
    // Slot+0: jmp handler (14 bytes); slot+0x10: relocated prologue + jmp function+relocate.
    AbsoluteJump(slot,handler);
    if(!replace) {
        std::memcpy(slot+0x10,expected,relocate);
        AbsoluteJump(slot+0x10+relocate,reinterpret_cast<const void*>(function+relocate));
    }
    FlushInstructionCache(GetCurrentProcess(),slot,kSlot);
    const int64_t rel=reinterpret_cast<intptr_t>(slot)-intptr_t(function+5);
    uint32_t e=ERROR_SUCCESS;
    if(rel<INT32_MIN || rel>INT32_MAX) e=ERROR_INVALID_ADDRESS;
    else {
        uint8_t bytes[8]; std::memcpy(bytes,reinterpret_cast<void*>(function),8);
        int64_t before; std::memcpy(&before,bytes,8);
        bytes[0]=0xe9; const int32_t r=int32_t(rel); std::memcpy(bytes+1,&r,4);
        int64_t after; std::memcpy(&after,bytes,8);
        DWORD old=0;
        if(!VirtualProtect(reinterpret_cast<void*>(function),8,PAGE_EXECUTE_READWRITE,&old)) e=GetLastError();
        else {
            const int64_t seen=InterlockedCompareExchange64(reinterpret_cast<volatile LONG64*>(function),after,before);
            DWORD ignored=0; VirtualProtect(reinterpret_cast<void*>(function),8,old,&ignored);
            FlushInstructionCache(GetCurrentProcess(),reinterpret_cast<void*>(function),8);
            if(seen!=before) e=ERROR_INVALID_DATA;
        }
    }
    ReleaseSRWLockExclusive(&lock);
    if(e) return fail(e);
    if(error) *error=ERROR_SUCCESS;
    return slot+0x10;
}
}
}
