#define WIN32_LEAN_AND_MEAN
#include <windows.h>
#include <bcrypt.h>
#include "game_b002.h"
#include "engine_mailbox.h"
#include "tick_slot.h"
#include "image_validation.h"
#include "simulation_state.h"
#include <atomic>
#include <cstring>

namespace hostctl {
namespace {
using TickFunction=void (*)(void*);
constexpr uintptr_t kServerTable=0x3e06b28,kServerTick=0x2df8f14,kProviderTable=0x3d44d80;
constexpr uintptr_t kContext=0x4dc3188,kServerFlag=0x4eadd40,kLanFlag=0x4eadd4d;
constexpr uintptr_t kManager=0x4f51240;
SRWLOCK installation=SRWLOCK_INIT;
bool installed_once=false; // transport/DLL lifetime is also single-start
SlotProtection slot_protection;
std::atomic<bool> running{false};
std::atomic<uintptr_t> image{0};
std::atomic<TickFunction> original_tick{nullptr};
std::atomic<DWORD> owner{0};
CommandMailbox commands;

// Bounded reads of this process's owned loaded module/native objects, only used
// after opt-in. SEH protects diagnostics from unreadable addresses; it cannot
// establish native object lifetime or recover faults inside game callbacks.
bool CopyAddress(uintptr_t address,void* destination,size_t size) noexcept {
    if(!address || !size || size>UINTPTR_MAX-address) return false;
    uintptr_t at=address; const uintptr_t end=address+size;
    while(at<end) {
        MEMORY_BASIC_INFORMATION region={};
        if(!VirtualQuery(reinterpret_cast<void*>(at),&region,sizeof(region)) || region.State!=MEM_COMMIT ||
            (region.Protect&(PAGE_GUARD|PAGE_NOACCESS)) || !region.RegionSize) return false;
        const uintptr_t start=reinterpret_cast<uintptr_t>(region.BaseAddress);
        if(region.RegionSize>UINTPTR_MAX-start || start+region.RegionSize<=at) return false;
        at=start+region.RegionSize;
    }
    __try { std::memcpy(destination,reinterpret_cast<const void*>(address),size); return true; }
    __except(EXCEPTION_EXECUTE_HANDLER) { return false; }
}
template<class T> bool Read(uintptr_t address,T& value) noexcept { return CopyAddress(address,&value,sizeof(value)); }
bool HashMatches(const wchar_t* path) noexcept {
    static const uint8_t wanted[32]={0x5d,0xa5,0x18,0xec,0x21,0xf5,0xab,0x7b,0xae,0xb0,0x21,0xae,0xed,0x2b,0x98,0x92,
        0xee,0xb6,0x53,0x20,0xdc,0x17,0xd2,0x68,0x2f,0x8f,0x55,0x27,0x1c,0x20,0x3d,0xa8};
    HANDLE file=CreateFileW(path,GENERIC_READ,FILE_SHARE_READ|FILE_SHARE_DELETE,nullptr,OPEN_EXISTING,FILE_ATTRIBUTE_NORMAL,nullptr);
    if(file==INVALID_HANDLE_VALUE) return false;
    BCRYPT_ALG_HANDLE algorithm=nullptr; BCRYPT_HASH_HANDLE hash=nullptr;
    PUCHAR object=nullptr; DWORD length=0,returned=0; bool result=false;
    do {
        if(BCryptOpenAlgorithmProvider(&algorithm,BCRYPT_SHA256_ALGORITHM,nullptr,0)<0) break;
        if(BCryptGetProperty(algorithm,BCRYPT_OBJECT_LENGTH,reinterpret_cast<PUCHAR>(&length),sizeof(length),&returned,0)<0 || !length || length>65536) break;
        object=static_cast<PUCHAR>(HeapAlloc(GetProcessHeap(),0,length)); if(!object) break;
        if(BCryptCreateHash(algorithm,&hash,object,length,nullptr,0,0)<0) break;
        uint8_t buffer[65536]; DWORD count=0; bool ok=true;
        for(;;) {
            if(!ReadFile(file,buffer,sizeof(buffer),&count,nullptr)) { ok=false; break; }
            if(!count) break;
            if(BCryptHashData(hash,buffer,count,0)<0) { ok=false; break; }
        }
        uint8_t digest[32]={};
        if(ok && BCryptFinishHash(hash,digest,sizeof(digest),0)>=0) result=std::memcmp(digest,wanted,sizeof(digest))==0;
    } while(false);
    if(hash) BCryptDestroyHash(hash); if(object) HeapFree(GetProcessHeap(),0,object);
    if(algorithm) BCryptCloseAlgorithmProvider(algorithm,0); CloseHandle(file); return result;
}
bool ValidateImage(uintptr_t base) noexcept {
    wchar_t path[32768]; const DWORD n=GetModuleFileNameW(nullptr,path,32768);
    if(!n || n>=32768 || !HashMatches(path)) return false;
    IMAGE_DOS_HEADER dos={}; IMAGE_NT_HEADERS64 nt={};
    if(!Read(base,dos) || dos.e_magic!=IMAGE_DOS_SIGNATURE || dos.e_lfanew<0 || dos.e_lfanew>0x100000 ||
        !Read(base+uintptr_t(dos.e_lfanew),nt) || !ValidateLoadedHeader(base,dos,nt,kManager+0x78)) return false;
    struct Fingerprint { uintptr_t rva; uint8_t bytes[16]; };
    static const Fingerprint targets[]={
        {0x964fb8,{0x48,0x83,0xec,0x28,0x48,0x8b,0x49,0x18,0xe8,0x7b,0x23,0xb8,0xff,0x32,0xd2,0x84}},
        {0x4e5cec,{0x8a,0x81,0xc0,0x00,0x00,0x00,0x48,0x81,0xc1,0xc8,0x00,0x00,0x00,0x24,0x01,0xf6}},
        {0x2e0d234,{0x48,0x89,0x5c,0x24,0x08,0x57,0x48,0x83,0xec,0x20,0x48,0x8b,0xf9,0x33,0xdb,0xe8}},
        {0x2e1d478,{0x48,0x89,0x5c,0x24,0x08,0x48,0x89,0x6c,0x24,0x10,0x48,0x89,0x74,0x24,0x18,0x57}},
        {0x2e1cb9c,{0x48,0x89,0x5c,0x24,0x08,0x48,0x89,0x6c,0x24,0x10,0x48,0x89,0x74,0x24,0x18,0x57}},
        {0x2e0dc08,{0x48,0x89,0x5c,0x24,0x08,0x48,0x89,0x74,0x24,0x10,0x57,0x48,0x83,0xec,0x20,0x48}},
        {kServerTick,{0x40,0x53,0x48,0x83,0xec,0x20,0x48,0x8b,0xd9,0xe8,0xca,0x06,0x00,0x00,0x48,0x8b}}
    };
    for(const auto& target:targets) {
        uint8_t observed[16]={}; MEMORY_BASIC_INFORMATION region={};
        if(!VirtualQuery(reinterpret_cast<void*>(base+target.rva),&region,sizeof(region)) || region.AllocationBase!=reinterpret_cast<void*>(base) ||
            !(region.Protect&(PAGE_EXECUTE|PAGE_EXECUTE_READ|PAGE_EXECUTE_READWRITE|PAGE_EXECUTE_WRITECOPY)) ||
            !CopyAddress(base+target.rva,observed,sizeof(observed)) || std::memcmp(observed,target.bytes,sizeof(observed))) return false;
    }
    uintptr_t tick=0,getter=0,setter=0,requested=0,request=0,apply=0;
    return Read(base+kServerTable+0x18,tick) && tick==base+kServerTick &&
        Read(base+kProviderTable+0xa0,getter) && getter==base+0x4e5cec &&
        Read(base+kProviderTable+0xa8,requested) && requested==base+0x2e0d234 &&
        Read(base+kProviderTable+0xb0,setter) && setter==base+0x2e1d478 &&
        Read(base+kProviderTable+0xb8,request) && request==base+0x2e1cb9c &&
        Read(base+kProviderTable+0x78,apply) && apply==base+0x2e0dc08;
}
bool IsLan(uintptr_t base) noexcept {
    uint8_t server=0,lan=0,thunder=0;
    return Read(base+kServerFlag,server) && Read(base+kLanFlag,lan) && Read(base+0x4eadd49,thunder) && server && lan && !thunder;
}
void HookTick(void* server) {
    const auto original=original_tick.load(std::memory_order_acquire);
    if(original) original(server); // exact native call, once; native exceptions keep native semantics
    if(!running.load(std::memory_order_acquire)) return;
    const uintptr_t base=image.load(std::memory_order_acquire);
    DWORD expected=0; const DWORD actual=GetCurrentThreadId(); owner.compare_exchange_strong(expected,actual);
    EngineFrame frame={GateBuild,StateUnknown,owner.load(),nullptr,base+kProviderTable,
        {reinterpret_cast<decltype(ProviderFunctions::Authority)>(base+0x964fb8),
         reinterpret_cast<decltype(ProviderFunctions::Current)>(base+0x4e5cec),
         reinterpret_cast<decltype(ProviderFunctions::Set)>(base+0x2e1d478),
         reinterpret_cast<decltype(ProviderFunctions::Requested)>(base+0x2e0d234)}};
    uintptr_t table=0,current_server=0;
    if(IsLan(base) && Read(base+kManager+0x70,current_server) && current_server==reinterpret_cast<uintptr_t>(server) &&
        Read(reinterpret_cast<uintptr_t>(server),table) && table==base+kServerTable) {
        frame.gates|=GateRole|GateDispatch;
        uint8_t active=0;
        if(Read(base+0x4dc3180,active) && active) Read(base+kContext,frame.state);
        uintptr_t session=0,simulation=0; int32_t valid=0;
        if(Read(base+kContext+0x78,session) && session && session<=UINTPTR_MAX-0x5a080 &&
            Read(session+0x5a078,valid) && SimulationAvailable(valid) && Read(session+0x59f78,simulation) &&
            simulation && simulation<=UINTPTR_MAX-0xb4b510) {
            const auto provider=simulation+0xb4afc8;
            uintptr_t provider_table=0;
            if(Read(provider,provider_table) && provider_table==frame.provider_table) frame.provider=reinterpret_cast<void*>(provider);
        }
    }
    commands.Tick(frame);
}
void* ExchangeSlot(void* volatile* slot,void* replacement,void* expected) { return InterlockedCompareExchangePointer(slot,replacement,expected); }
const SlotFunctions slot_functions={VirtualProtect,ExchangeSlot};
DWORD SwapTick(uintptr_t base,void* expected,void* replacement) noexcept {
    const auto slot=reinterpret_cast<void* volatile*>(base+kServerTable+0x18);
    return ReplaceTickSlot(slot,expected,replacement,slot_protection,slot_functions);
}

// Server-owned selection lock. Game code reaches the provider's authoritative
// Set (142e1d478), request dispatcher (142e1cb9c) and unconditional apply (142e0dc08)
// only through table slots +b0/+b8/+78 (A048/A049 RefsTo: no direct calls). hostctl calls Set directly, so
// these hooks see only other writers, such as a lobby leader's selection.
using ProviderWrite=uint8_t (*)(void*,const ContentArray*);
constexpr uintptr_t kProviderSet=0x2e1d478,kProviderRequest=0x2e1cb9c,kProviderApply=0x2e0dc08;
SlotProtection set_protection,request_protection,apply_protection;
SRWLOCK selection_lock=SRWLOCK_INIT;
bool lock_active=false;
ContentSelection locked_entry={};
std::atomic<bool> intercepted{false};
bool LockedEntry(ContentSelection& entry) noexcept {
    AcquireSRWLockShared(&selection_lock);
    const bool active=lock_active; if(active) entry=locked_entry;
    ReleaseSRWLockShared(&selection_lock); return active;
}
uint8_t RewriteEntry(uintptr_t target,void* provider,const ContentArray* contents) {
    const auto original=reinterpret_cast<ProviderWrite>(image.load(std::memory_order_acquire)+target);
    ContentSelection entry;
    if(!running.load(std::memory_order_acquire) || !contents || !LockedEntry(entry)) return original(provider,contents);
    alignas(16) ContentArray copy;
    std::memcpy(&copy,contents,sizeof(copy));
    if(std::memcmp(&copy.entries[0],&entry,sizeof(entry))!=0) {
        copy.entries[0]=entry; intercepted.store(true,std::memory_order_release);
    }
    return original(provider,&copy);
}
uint8_t HookProviderSet(void* provider,const ContentArray* contents) { return RewriteEntry(kProviderSet,provider,contents); }
uint8_t HookProviderRequest(void* provider,const ContentArray* contents) { return RewriteEntry(kProviderRequest,provider,contents); }
// Slot +78 (142e0dc08, also base table 143692f60+78) copies into Current with no
// authority check; R015 showed a fresh leader overwrote Current without +b0/+b8.
uint8_t HookProviderApply(void* provider,const ContentArray* contents) { return RewriteEntry(kProviderApply,provider,contents); }
DWORD SwapProviderSlot(uintptr_t base,uintptr_t offset,void* expected,void* replacement,SlotProtection& state) noexcept {
    const auto slot=reinterpret_cast<void* volatile*>(base+kProviderTable+offset);
    return ReplaceTickSlot(slot,expected,replacement,state,slot_functions);
}
// Restores one provider slot if it still holds our hook; preserves any other value.
DWORD RestoreProviderSlot(uintptr_t base,uintptr_t offset,uintptr_t native,void* hook,SlotProtection& state) noexcept {
    uintptr_t observed=0; DWORD result=ERROR_SUCCESS;
    if(!Read(base+kProviderTable+offset,observed)) result=ERROR_READ_FAULT;
    else if(observed==reinterpret_cast<uintptr_t>(hook)) result=SwapProviderSlot(base,offset,hook,reinterpret_cast<void*>(base+native),state);
    const auto cleanup=RestoreTickProtection(reinterpret_cast<void* volatile*>(base+kProviderTable+offset),state,slot_functions);
    return result==ERROR_SUCCESS ? cleanup : result;
}
}
uint32_t InstallGameBackend() noexcept {
    AcquireSRWLockExclusive(&installation);
    if(running.load()) { ReleaseSRWLockExclusive(&installation); return ERROR_SUCCESS; }
    if(installed_once) { ReleaseSRWLockExclusive(&installation); return ERROR_ALREADY_EXISTS; }
    const auto base=reinterpret_cast<uintptr_t>(GetModuleHandleW(nullptr));
    DWORD result=ERROR_NOT_SUPPORTED;
    if(base && ValidateImage(base)) {
        result=ERROR_INVALID_FUNCTION;
        if(IsLan(base)) {
            HMODULE own=nullptr;
            if(GetModuleHandleExW(GET_MODULE_HANDLE_EX_FLAG_FROM_ADDRESS|GET_MODULE_HANDLE_EX_FLAG_PIN,
                reinterpret_cast<LPCWSTR>(&InstallGameBackend),&own)) {
                image.store(base); original_tick.store(reinterpret_cast<TickFunction>(base+kServerTick)); owner.store(0);
                installed_once=true; commands.Enable(); running.store(true,std::memory_order_release);
                // Provider write hooks pass through unchanged until a selection locks entry0.
                result=SwapProviderSlot(base,0xb0,reinterpret_cast<void*>(base+kProviderSet),reinterpret_cast<void*>(&HookProviderSet),set_protection);
                if(result==ERROR_SUCCESS) result=SwapProviderSlot(base,0xb8,reinterpret_cast<void*>(base+kProviderRequest),reinterpret_cast<void*>(&HookProviderRequest),request_protection);
                if(result==ERROR_SUCCESS) result=SwapProviderSlot(base,0x78,reinterpret_cast<void*>(base+kProviderApply),reinterpret_cast<void*>(&HookProviderApply),apply_protection);
                if(result==ERROR_SUCCESS) result=SwapTick(base,reinterpret_cast<void*>(base+kServerTick),reinterpret_cast<void*>(&HookTick));
                if(result!=ERROR_SUCCESS) {
                    running.store(false); commands.Stop();
                    RestoreProviderSlot(base,0x78,kProviderApply,reinterpret_cast<void*>(&HookProviderApply),apply_protection);
                    RestoreProviderSlot(base,0xb8,kProviderRequest,reinterpret_cast<void*>(&HookProviderRequest),request_protection);
                    RestoreProviderSlot(base,0xb0,kProviderSet,reinterpret_cast<void*>(&HookProviderSet),set_protection);
                }
            } else result=GetLastError();
        }
    }
    ReleaseSRWLockExclusive(&installation); return result;
}
uint32_t StopGameBackend() noexcept {
    AcquireSRWLockExclusive(&installation); running.store(false); commands.Stop();
    const auto base=image.load(); DWORD result=ERROR_SUCCESS;
    uintptr_t observed=0;
    // Check actual ownership even if an installation failure made the callback
    // passive. Late fetched callbacks are safe to enter because the DLL is pinned.
    if(base) {
        if(!Read(base+kServerTable+0x18,observed)) result=ERROR_READ_FAULT;
        else if(observed==reinterpret_cast<uintptr_t>(&HookTick)) result=SwapTick(base,reinterpret_cast<void*>(&HookTick),reinterpret_cast<void*>(base+kServerTick));
        const auto cleanup=RestoreTickProtection(reinterpret_cast<void* volatile*>(base+kServerTable+0x18),slot_protection,slot_functions);
        if(result==ERROR_SUCCESS) result=cleanup;
        const auto apply=RestoreProviderSlot(base,0x78,kProviderApply,reinterpret_cast<void*>(&HookProviderApply),apply_protection);
        const auto request=RestoreProviderSlot(base,0xb8,kProviderRequest,reinterpret_cast<void*>(&HookProviderRequest),request_protection);
        const auto set=RestoreProviderSlot(base,0xb0,kProviderSet,reinterpret_cast<void*>(&HookProviderSet),set_protection);
        if(result==ERROR_SUCCESS) result=apply;
        if(result==ERROR_SUCCESS) result=request;
        if(result==ERROR_SUCCESS) result=set;
    }
    AcquireSRWLockExclusive(&selection_lock); lock_active=false; ReleaseSRWLockExclusive(&selection_lock);
    ReleaseSRWLockExclusive(&installation); return result;
}
namespace {
BackendReport WithLockGates(BackendReport report) noexcept {
    ContentSelection entry;
    if(LockedEntry(entry)) report.gates|=GateLocked;
    if(intercepted.load(std::memory_order_acquire)) report.gates|=GateIntercepted;
    return report;
}
}
BackendReport BackendStatus() noexcept { return WithLockGates(commands.Status()); }
BackendReport BackendSelect(const ContentSelection& desired,uint32_t wait_ms) noexcept {
    uint16_t rejection=CodePending; const auto ticket=commands.Submit(desired,&rejection);
    if(!ticket) { auto report=commands.Status(); report.code=rejection; report.gates&=~uint32_t(GateSelection); return WithLockGates(report); }
    auto report=commands.Wait(ticket,wait_ms);
    if(report.code==CodeSelected) {
        // observed is the exact entry0 written and read back, with native types.
        AcquireSRWLockExclusive(&selection_lock); locked_entry=report.observed; lock_active=true; ReleaseSRWLockExclusive(&selection_lock);
    }
    return WithLockGates(report);
}
}
