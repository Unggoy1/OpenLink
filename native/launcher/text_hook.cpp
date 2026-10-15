// String-list lookup detours for OpenLink UI text (see text_hook.h, O158).
#define WIN32_LEAN_AND_MEAN
#include <windows.h>
#include "text_hook.h"
#include <atomic>
#include <cstring>
#include <cstdio>
namespace clientjoin {
namespace text {
namespace {
constexpr size_t kEntries=512,kChars=256;
struct Entry { uint32_t id; uint16_t length; wchar_t text[kChars]; };
SRWLOCK lock=SRWLOCK_INIT;
Entry entries[kEntries]; size_t used=0;
std::atomic<bool> installed{false};
std::atomic<uint32_t> hits{0};
SRWLOCK install_lock=SRWLOCK_INIT;

// Lookup A 1405c7204 and B 1405c7038 share the first 15 prologue bytes; the first two
// instructions (7 bytes: mov rax,rsp; mov [rax+10h],rbx) are relocated, both RIP-free.
constexpr uintptr_t kLookupA=0x5c7204,kLookupB=0x5c7038;
constexpr uint8_t kPrologue[15]={0x48,0x8b,0xc4,0x48,0x89,0x58,0x10,0x48,0x89,0x68,0x18,0x48,0x89,0x70,0x20};
constexpr size_t kRelocated=7;
using LookupA=uintptr_t (*)(uint32_t,uint32_t,wchar_t*,int32_t,uint8_t*);
using LookupB=uintptr_t (*)(uint32_t,uint32_t,wchar_t*,uint8_t*);
LookupA original_a=nullptr; LookupB original_b=nullptr;

// Copies our text for sid into out (capacity in wchar_t, including the NUL).
// Overlay: game string IDs replaced only while the overlay is active (OpenLink mode).
std::atomic<bool> overlay_active{false};
Entry overlay[16]; size_t overlay_used=0;
std::atomic<bool (*)(uint32_t,wchar_t*,size_t)> overlay_provider{nullptr}; // Dynamic overlay text (e.g. selected server name)
bool Find(uint32_t sid,wchar_t* out,size_t capacity) noexcept {
    if(!sid || sid==0xffffffffu || !out || capacity<1) return false;
    bool found=false;
    AcquireSRWLockShared(&lock);
    const auto provider=overlay_provider.load(); if(overlay_active.load() && provider && provider(sid,out,capacity)) found=true;
    if(!found && overlay_active.load()) for(size_t i=0;i<overlay_used;++i) if(overlay[i].id==sid) {
        const size_t n=overlay[i].length<capacity-1 ? overlay[i].length : capacity-1;
        std::memcpy(out,overlay[i].text,n*sizeof(wchar_t)); out[n]=0; found=true; break;
    }
    if(!found) for(size_t i=0;i<used;++i) if(entries[i].id==sid) {
        const size_t n=entries[i].length<capacity-1 ? entries[i].length : capacity-1;
        std::memcpy(out,entries[i].text,n*sizeof(wchar_t)); out[n]=0; found=true; break;
    }
    ReleaseSRWLockShared(&lock);
    if(found) hits.fetch_add(1,std::memory_order_relaxed);
    return found;
}
uintptr_t DetourA(uint32_t tag,uint32_t sid,wchar_t* out,int32_t capacity,uint8_t* found) {
    if(capacity>0 && Find(sid,out,size_t(capacity))) { if(found) *found=1; return 1; }
    return original_a(tag,sid,out,capacity,found);
}
uintptr_t DetourB(uint32_t tag,uint32_t sid,wchar_t* out,uint8_t* found) {
    if(Find(sid,out,0x400)) { if(found) *found=1; return 1; }
    return original_b(tag,sid,out,found);
}
uint8_t* NearPage(uintptr_t image) {
    SYSTEM_INFO info; GetSystemInfo(&info);
    const uintptr_t granularity=info.dwAllocationGranularity;
    // Search below and above the image within +-1 GB so rel32 jumps reach.
    for(uintptr_t distance=granularity;distance<0x40000000;distance+=granularity) {
        for(int side=0;side<2;++side) {
            const uintptr_t at=side ? ((image+distance)&~(granularity-1)) : ((image-distance)&~(granularity-1));
            if(void* p=VirtualAlloc(reinterpret_cast<void*>(at),0x1000,MEM_RESERVE|MEM_COMMIT,PAGE_EXECUTE_READWRITE)) return static_cast<uint8_t*>(p);
        }
    }
    return nullptr;
}
void AbsoluteJump(uint8_t* at,const void* target) { at[0]=0xff; at[1]=0x25; std::memset(at+2,0,4); const uintptr_t t=reinterpret_cast<uintptr_t>(target); std::memcpy(at+6,&t,8); }
// Writes "jmp rel32 stub" over the first 5 bytes with one 8-byte compare-exchange.
uint32_t Patch(uintptr_t function,const uint8_t* stub) {
    const int64_t rel=reinterpret_cast<intptr_t>(stub)-intptr_t(function+5);
    if(rel<INT32_MIN || rel>INT32_MAX) return ERROR_INVALID_ADDRESS;
    uint8_t bytes[8]; std::memcpy(bytes,reinterpret_cast<void*>(function),8);
    int64_t expected; std::memcpy(&expected,bytes,8);
    bytes[0]=0xe9; const int32_t r=int32_t(rel); std::memcpy(bytes+1,&r,4);
    int64_t replacement; std::memcpy(&replacement,bytes,8);
    DWORD old=0; if(!VirtualProtect(reinterpret_cast<void*>(function),8,PAGE_EXECUTE_READWRITE,&old)) return GetLastError();
    const int64_t seen=InterlockedCompareExchange64(reinterpret_cast<volatile LONG64*>(function),replacement,expected);
    DWORD ignored=0; VirtualProtect(reinterpret_cast<void*>(function),8,old,&ignored);
    FlushInstructionCache(GetCurrentProcess(),reinterpret_cast<void*>(function),8);
    return seen==expected ? ERROR_SUCCESS : ERROR_INVALID_DATA;
}
}

uint32_t StringId(const char* text,size_t length) noexcept {
    constexpr uint32_t c1=0xcc9e2d51,c2=0x1b873593;
    auto normal=[&](size_t i)->uint8_t { uint8_t ch=uint8_t(text[i]); if(ch>='A'&&ch<='Z') ch=uint8_t(ch+32); else if(ch=='-'||ch==' ') ch='_'; else if(ch=='\n') ch='#'; return ch; };
    auto rotl=[](uint32_t x,int r) { return (x<<r)|(x>>(32-r)); };
    uint32_t h=0; size_t i=0;
    for(;i+4<=length;i+=4) {
        uint32_t k=uint32_t(normal(i))|uint32_t(normal(i+1))<<8|uint32_t(normal(i+2))<<16|uint32_t(normal(i+3))<<24;
        k*=c1; k=rotl(k,15); k*=c2; h^=k; h=rotl(h,13); h=h*5+0xe6546b64;
    }
    uint32_t k=0;
    switch(length&3) { case 3: k^=uint32_t(normal(i+2))<<16; [[fallthrough]]; case 2: k^=uint32_t(normal(i+1))<<8; [[fallthrough]];
        case 1: k^=uint32_t(normal(i)); k*=c1; k=rotl(k,15); k*=c2; h^=k; break; default: break; }
    h^=uint32_t(length); h^=h>>16; h*=0x85ebca6b; h^=h>>13; h*=0xc2b2ae35; h^=h>>16; return h;
}
uint32_t Set(const char* key,const char* utf8) noexcept {
    if(!key || !utf8) return 0;
    const size_t key_length=std::strlen(key); if(!key_length || key_length>200) return 0;
    const uint32_t id=StringId(key,key_length); if(!id || id==0xffffffffu) return 0;
    Entry e={}; e.id=id;
    const int n=MultiByteToWideChar(CP_UTF8,MB_ERR_INVALID_CHARS,utf8,-1,e.text,int(kChars));
    if(n<1) return 0;
    e.length=uint16_t(n-1);
    for(size_t i=0;i<e.length;++i) if(e.text[i]>=0xd800 && e.text[i]<=0xdfff) e.text[i]=L'?'; // BMP only
    AcquireSRWLockExclusive(&lock);
    size_t slot=used;
    for(size_t i=0;i<used;++i) if(entries[i].id==id) { slot=i; break; }
    uint32_t result=0;
    if(slot<kEntries) { entries[slot]=e; if(slot==used) ++used; result=id; }
    ReleaseSRWLockExclusive(&lock); return result;
}
uint32_t Install(uintptr_t image) noexcept {
    AcquireSRWLockExclusive(&install_lock);
    uint32_t e=ERROR_SUCCESS;
    if(!installed.load()) {
        if(std::memcmp(reinterpret_cast<void*>(image+kLookupA),kPrologue,15) || std::memcmp(reinterpret_cast<void*>(image+kLookupB),kPrologue,15)) e=ERROR_BAD_EXE_FORMAT;
        uint8_t* page=e ? nullptr : NearPage(image);
        if(!e && !page) e=ERROR_NOT_ENOUGH_MEMORY;
        if(!e) {
            // Layout: +0 jmp DetourA, +0x20 jmp DetourB, +0x40 trampoline A, +0x60 trampoline B.
            AbsoluteJump(page,reinterpret_cast<const void*>(&DetourA));
            AbsoluteJump(page+0x20,reinterpret_cast<const void*>(&DetourB));
            std::memcpy(page+0x40,kPrologue,kRelocated); AbsoluteJump(page+0x40+kRelocated,reinterpret_cast<const void*>(image+kLookupA+kRelocated));
            std::memcpy(page+0x60,kPrologue,kRelocated); AbsoluteJump(page+0x60+kRelocated,reinterpret_cast<const void*>(image+kLookupB+kRelocated));
            original_a=reinterpret_cast<LookupA>(page+0x40); original_b=reinterpret_cast<LookupB>(page+0x60);
            FlushInstructionCache(GetCurrentProcess(),page,0x80);
            e=Patch(image+kLookupA,page);
            if(!e) { e=Patch(image+kLookupB,page+0x20); }
            if(!e) installed.store(true);
            // A failed second patch leaves A hooked; it stays valid (trampoline is live).
            else if(std::memcmp(reinterpret_cast<void*>(image+kLookupA),kPrologue,5)) installed.store(true);
        }
    }
    ReleaseSRWLockExclusive(&install_lock); return e;
}
bool Installed() noexcept { return installed.load(); }
uint32_t Hits() noexcept { return hits.load(); }
}
}
namespace clientjoin {
namespace text {
bool SetOverlay(uint32_t sid,const char* utf8) noexcept {
    if(!sid || sid==0xffffffffu || !utf8) return false;
    Entry e={}; e.id=sid;
    const int n=MultiByteToWideChar(CP_UTF8,MB_ERR_INVALID_CHARS,utf8,-1,e.text,int(kChars)); if(n<1) return false;
    e.length=uint16_t(n-1);
    AcquireSRWLockExclusive(&lock);
    size_t slot=overlay_used; for(size_t i=0;i<overlay_used;++i) if(overlay[i].id==sid) { slot=i; break; }
    const bool ok=slot<16; if(ok) { overlay[slot]=e; if(slot==overlay_used) ++overlay_used; }
    ReleaseSRWLockExclusive(&lock); return ok;
}
void SetOverlayActive(bool on) noexcept { overlay_active.store(on); }
}
}
namespace clientjoin {
namespace text {
void SetOverlayProvider(bool (*provider)(uint32_t,wchar_t*,size_t)) noexcept { overlay_provider=provider; }
}
}
