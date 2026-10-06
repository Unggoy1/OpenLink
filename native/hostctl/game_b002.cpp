#define WIN32_LEAN_AND_MEAN
#include <windows.h>
#include <bcrypt.h>
#include "game_b002.h"
#include "engine_mailbox.h"
#include "tick_slot.h"
#include "image_validation.h"
#include "simulation_state.h"
#include "beacon_name.h"
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
        {kServerTick,{0x40,0x53,0x48,0x83,0xec,0x20,0x48,0x8b,0xd9,0xe8,0xca,0x06,0x00,0x00,0x48,0x8b}},
        {0x2e1d270,{0x40,0x53,0x48,0x83,0xec,0x20,0x4c,0x8b,0xca,0x48,0x8b,0xd9,0x45,0x33,0xc0,0xe8}},
        {0x2e0d870,{0x40,0x53,0x48,0x83,0xec,0x20,0x8b,0x81,0xc8,0x00,0x00,0x00,0x48,0x8b,0xd9,0x3b}},
        {0x2e0dcb8,{0x40,0x53,0x48,0x83,0xec,0x20,0x44,0x0f,0xb6,0x81,0xc8,0x00,0x00,0x00,0x48,0x8b}},
        // qword Set 142e1d39c past its shared prologue: mov rax,[rbx+c8]; cmp rax,[r9]; jnz; test [rbx+c0],1
        {0x2e1d3b4,{0x48,0x8b,0x83,0xc8,0x00,0x00,0x00,0x49,0x3b,0x01,0x75,0x09,0xf6,0x83,0xc0,0x00}}
    };
    for(const auto& target:targets) {
        uint8_t observed[16]={}; MEMORY_BASIC_INFORMATION region={};
        if(!VirtualQuery(reinterpret_cast<void*>(base+target.rva),&region,sizeof(region)) || region.AllocationBase!=reinterpret_cast<void*>(base) ||
            !(region.Protect&(PAGE_EXECUTE|PAGE_EXECUTE_READ|PAGE_EXECUTE_READWRITE|PAGE_EXECUTE_WRITECOPY)) ||
            !CopyAddress(base+target.rva,observed,sizeof(observed)) || std::memcmp(observed,target.bytes,sizeof(observed))) return false;
    }
    uintptr_t tick=0,getter=0,setter=0,requested=0,request=0,apply=0,int_getter=0,int_set=0,int_apply=0,byte_apply=0,
        qword_getter=0,qword_set=0;
    return Read(base+0x3d45040+0xa0,qword_getter) && qword_getter==base+0x4e5cec &&
        Read(base+0x3d45040+0xb0,qword_set) && qword_set==base+0x2e1d39c &&
        Read(base+0x3d44a80+0xa0,int_getter) && int_getter==base+0x4e5cec &&
        Read(base+0x3d44a80+0xb0,int_set) && int_set==base+0x2e1d270 &&
        Read(base+0x3d44a80+0x78,int_apply) && int_apply==base+0x2e0d870 &&
        Read(base+0x3e06a20+0x78,byte_apply) && byte_apply==base+0x2e0dcb8 &&
        Read(base+kServerTable+0x18,tick) && tick==base+kServerTick &&
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
// Lobby observation and the start command. Both run on the engine tick only.
constexpr uintptr_t kPregameHandler=0x4dc51e8,kPregameTable=0x3706df0,kSessionKind=0x4c253b0;
constexpr uintptr_t kIntTable=0x3d44a80,kIntSet=0x2e1d270,kStartMode=0x541f0;
using IntSet=uint8_t (*)(void*,const int32_t*);
SRWLOCK probe_lock=SRWLOCK_INIT;
LobbyProbe probe={};
std::atomic<uint64_t> start_request{0},start_completed{0},start_sequence{0};
std::atomic<uint16_t> start_code{CodePending};
std::atomic<bool> start_sent{false},server_owned{false},owner_patched{false};

// Player request filter (server-owned lobby). Requests from clients reach the
// server through the component tables' +0x78 apply; the server's own writes use
// Set (+0xb0). Applies to the start-mode and end-game components are dropped.
constexpr uintptr_t kIntApply=0x2e0d870,kByteTable=0x3e06a20,kByteApply=0x2e0dcb8,kEndGame=0xb4c4b0;
std::atomic<uintptr_t> current_simulation{0};
std::atomic<uint32_t> blocked_start{0},blocked_end{0};
std::atomic<bool> filter_active{false};
using IntApply=uint8_t (*)(void*,const int32_t*);
using ByteApply=uint8_t (*)(void*,const uint8_t*);
bool FilterRequest(void* component,uintptr_t offset) noexcept {
    const uintptr_t simulation=current_simulation.load(std::memory_order_acquire);
    return filter_active.load(std::memory_order_acquire) && simulation &&
        reinterpret_cast<uintptr_t>(component)==simulation+offset;
}
uint8_t HookIntApply(void* component,const int32_t* value) {
    if(FilterRequest(component,kStartMode)) { blocked_start.fetch_add(1); return 1; }
    return reinterpret_cast<IntApply>(image.load(std::memory_order_acquire)+kIntApply)(component,value);
}
uint8_t HookByteApply(void* component,const uint8_t* value) {
    if(FilterRequest(component,kEndGame)) { blocked_end.fetch_add(1); return 1; }
    return reinterpret_cast<ByteApply>(image.load(std::memory_order_acquire)+kByteApply)(component,value);
}

// LAN lobby leader (BackendSetLeader, FN027): qword component, valid bit +0xc0,
// value +0xc8, written on the engine tick through its authoritative Set.
constexpr uintptr_t kLeader=0xb4ba60,kQwordTable=0x3d45040,kQwordSet=0x2e1d39c;
using QwordSet=uint8_t (*)(void*,const uint64_t*);
std::atomic<uint64_t> leader_xuid{0},leader_request{0},leader_completed{0},leader_sequence{0};
std::atomic<uint16_t> leader_code{CodePending};
std::atomic<bool> leader_clear{false};
std::atomic<uint32_t> leader_sets{0};
// Returns the validated component address, or 0.
uintptr_t LeaderComponent(uintptr_t base,uintptr_t simulation) noexcept {
    uintptr_t table=0,set=0;
    if(!simulation || !Read(simulation+kLeader,table) || table!=base+kQwordTable || !Read(table+0xb0,set) || set!=base+kQwordSet) return 0;
    return simulation+kLeader;
}
bool ReadLeader(uintptr_t component,uint64_t& value) noexcept {
    uint8_t available=0; value=0;
    return component && Read(component+0xc0,available) && (available&1) && Read(component+0xc8,value);
}

LobbyProbe ReadLobby(uintptr_t base,uintptr_t session,uintptr_t simulation) noexcept {
    LobbyProbe p={}; p.owner=p.host_peer=p.start_mode=-1;
    if(session && Read(session+0xa0,p.peers) && Read(session+0xa4,p.peer_mask) && Read(session+0x6c,p.owner) &&
        Read(session+0x70,p.host_peer) && Read(session+0x18a8,p.players)) {
        p.flags|=LobbyValid;
        for(unsigned i=0;i<32;++i) {
            int32_t state=0;
            if((p.peer_mask>>i)&1 && Read(session+0xb0+uintptr_t(i)*0xc0,state) && state==8) ++p.connected;
        }
    }
    uintptr_t table=0; uint8_t available=0;
    if(simulation && Read(simulation+kStartMode,table) && table==base+kIntTable &&
        Read(simulation+kStartMode+0xc0,available) && (available&1) && Read(simulation+kStartMode+0xc8,p.start_mode))
        p.flags|=LobbyStartMode;
    uintptr_t handler_table=0,handler_context=0;
    const uintptr_t handler=base+kPregameHandler;
    if(Read(handler,handler_table) && handler_table==base+kPregameTable && Read(handler+0x38,handler_context) &&
        handler_context==base+kContext && Read(handler+0x58,p.content_prepared) && Read(handler+0x68,p.prep_started) &&
        Read(handler+0x69,p.prep_done) && Read(handler+0x74,p.loading) && Read(handler+0x76,p.start))
        p.flags|=LobbyHandler;
    Read(base+kContext+0x280,p.allowed); Read(base+kContext+0xec,p.users_required); Read(base+kContext+0xe8,p.game_type);
    uintptr_t kind=0; if(!Read(base+kSessionKind,kind) || !kind || !Read(kind+8,p.session_kind)) p.session_kind=-1;
    uintptr_t end_table=0; uint8_t end_value=0; p.end_game=-1;
    if(simulation && Read(simulation+kEndGame,end_table) && end_table>=base && end_table-base<0x10000000) {
        p.end_game_table=int32_t(end_table-base);
        if(Read(simulation+kEndGame+0xc8,end_value)) p.end_game=end_value;
    }
    p.blocked_start=uint8_t(blocked_start.load()>255 ? 255 : blocked_start.load());
    p.blocked_end=uint8_t(blocked_end.load()>255 ? 255 : blocked_end.load());
    if(server_owned.load(std::memory_order_acquire)) p.flags|=LobbyServerOwned;
    if(owner_patched.load(std::memory_order_acquire)) p.flags|=LobbyNoOwner;
    if(start_sent.load(std::memory_order_acquire)) p.flags|=LobbyStartSent;
    if(ReadLeader(LeaderComponent(base,simulation),p.leader)) {
        p.flags|=LobbyLeaderValid;
        const uint64_t held=leader_xuid.load(std::memory_order_acquire);
        if(held && p.leader==held) p.flags|=LobbyLeaderHeld;
    }
    p.leader_sets=leader_sets.load();
    return p;
}
// Keeps the server's leader XUID in the leader component (engine tick only) and
// answers a pending BackendSetLeader. Set is called only when the value differs.
void TickLeader(uintptr_t base,uintptr_t simulation) noexcept {
    const uint64_t ticket=leader_request.exchange(0,std::memory_order_acq_rel);
    const uint64_t desired=leader_xuid.load(std::memory_order_acquire);
    const bool clear=!desired && leader_clear.load(std::memory_order_acquire);
    uint16_t code=CodeOK;
    if(desired || clear) {
        code=CodeBusy;
        if(simulation) {
            const uintptr_t component=LeaderComponent(base,simulation);
            code=CodeUnsupported;
            if(component) {
                uint64_t current=0; const bool valid=ReadLeader(component,current);
                code=CodeFailed;
                if(valid && current==desired) code=CodeOK;
                else if(reinterpret_cast<QwordSet>(base+kQwordSet)(reinterpret_cast<void*>(component),&desired) &&
                    ReadLeader(component,current) && current==desired) {
                    code=CodeOK;
                    if(desired) { const uint32_t n=leader_sets.load(); if(n!=UINT32_MAX) leader_sets.store(n+1); }
                }
                if(clear && code==CodeOK) leader_clear.store(false,std::memory_order_release);
            }
        }
    }
    if(ticket) {
        leader_code.store(code,std::memory_order_release);
        leader_completed.store(ticket,std::memory_order_release);
    }
}
// Executes a queued Start on the engine thread: HostPreGame only, validated
// component table, native authority-checked Set, then readback.
void TickStart(uintptr_t base,int32_t state,uint32_t gates,uintptr_t simulation) noexcept {
    const uint64_t ticket=start_request.exchange(0,std::memory_order_acq_rel);
    if(!ticket) return;
    uint16_t code=CodeBusy;
    const uint32_t required=GateRole|GateDispatch;
    if(state==HostPreGame && (gates&required)==required && simulation) {
        code=CodeFailed;
        const uintptr_t component=simulation+kStartMode; uintptr_t table=0,set=0; uint8_t available=0;
        if(Read(component,table) && table==base+kIntTable && Read(table+0xb0,set) && set==base+kIntSet &&
            Read(component+0xc0,available) && (available&1)) {
            const int32_t value=1; int32_t observed=0;
            if(reinterpret_cast<IntSet>(set)(reinterpret_cast<void*>(component),&value) &&
                Read(component+0xc8,observed) && observed==value) {
                code=CodeOK; start_sent.store(true,std::memory_order_release);
            }
        }
    }
    start_code.store(code,std::memory_order_release);
    start_completed.store(ticket,std::memory_order_release);
}

// Server name in the in-game list (BackendSetName). The beacon tick 142e9954c
// runs in the same networking update (1405145e0 -> 1422c2d0c) as the server
// tick that calls HookTick, so this write never races a beacon serialization.
constexpr uintptr_t kBeacon=0x4dbfef0;
SRWLOCK name_lock=SRWLOCK_INIT;
uint16_t pending_name[kBeaconNameUnits]={};
std::atomic<uint64_t> name_request{0},name_completed{0},name_sequence{0};
std::atomic<uint16_t> name_code{CodePending};
bool WriteAddress(uintptr_t address,const void* source,size_t size) noexcept {
    MEMORY_BASIC_INFORMATION region={};
    if(!VirtualQuery(reinterpret_cast<void*>(address),&region,sizeof(region)) || region.State!=MEM_COMMIT ||
        !(region.Protect&(PAGE_READWRITE|PAGE_WRITECOPY)) || (region.Protect&PAGE_GUARD) ||
        reinterpret_cast<uintptr_t>(region.BaseAddress)+region.RegionSize<address+size) return false;
    __try { std::memcpy(reinterpret_cast<void*>(address),source,size); return true; }
    __except(EXCEPTION_EXECUTE_HANDLER) { return false; }
}
void TickName(uintptr_t base) noexcept {
    const uint64_t ticket=name_request.exchange(0,std::memory_order_acq_rel);
    if(!ticket) return;
    uint16_t code=CodeUnsupported;
    uint8_t snapshot[kBeaconSnapshot]={};
    MEMORY_BASIC_INFORMATION region={};
    if(VirtualQuery(reinterpret_cast<void*>(base+kBeacon),&region,sizeof(region)) && region.AllocationBase==reinterpret_cast<void*>(base) &&
        CopyAddress(base+kBeacon,snapshot,sizeof(snapshot))) {
        const auto check=CheckBeacon(snapshot);
        if(check==BeaconCheck::NotStarted) code=CodeBusy;
        if(check==BeaconCheck::Ok) {
            uint16_t units[kBeaconNameUnits],observed[kBeaconNameUnits]={};
            AcquireSRWLockShared(&name_lock); std::memcpy(units,pending_name,sizeof(units)); ReleaseSRWLockShared(&name_lock);
            code=CodeFailed;
            if(WriteAddress(base+kBeacon+kBeaconName,units,sizeof(units)) &&
                CopyAddress(base+kBeacon+kBeaconName,observed,sizeof(observed)) && !std::memcmp(units,observed,sizeof(units)))
                code=CodeOK;
        }
    }
    name_code.store(code,std::memory_order_release);
    name_completed.store(ticket,std::memory_order_release);
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
    uintptr_t table=0,current_server=0,session=0,simulation=0;
    if(IsLan(base) && Read(base+kManager+0x70,current_server) && current_server==reinterpret_cast<uintptr_t>(server) &&
        Read(reinterpret_cast<uintptr_t>(server),table) && table==base+kServerTable) {
        frame.gates|=GateRole|GateDispatch;
        uint8_t active=0;
        if(Read(base+0x4dc3180,active) && active) Read(base+kContext,frame.state);
        int32_t valid=0;
        if(Read(base+kContext+0x78,session) && session && session<=UINTPTR_MAX-0x5a080 &&
            Read(session+0x5a078,valid) && SimulationAvailable(valid) && Read(session+0x59f78,simulation) &&
            simulation && simulation<=UINTPTR_MAX-0xb4b510) {
            const auto provider=simulation+0xb4afc8;
            uintptr_t provider_table=0;
            if(Read(provider,provider_table) && provider_table==frame.provider_table) frame.provider=reinterpret_cast<void*>(provider);
        } else simulation=0;
        current_simulation.store(simulation,std::memory_order_release);
        if(frame.state==HostInGame || frame.state==HostEndGame) start_sent.store(false,std::memory_order_release);
        TickStart(base,frame.state,frame.gates,simulation);
        TickName(base);
        TickLeader(base,simulation);
        auto lobby=ReadLobby(base,session,simulation);
        // A Start whose players all left before the engine began the match would
        // otherwise start instantly for the next joiner; withdraw it (start mode 0).
        if(frame.state==HostPreGame && start_sent.load(std::memory_order_acquire) && simulation &&
            (lobby.flags&(LobbyValid|LobbyStartMode))==(LobbyValid|LobbyStartMode) && lobby.connected==0 && lobby.start_mode==1 && !lobby.start) {
            const uintptr_t component=simulation+kStartMode; uintptr_t component_table=0,set=0;
            if(Read(component,component_table) && component_table==base+kIntTable && Read(component_table+0xb0,set) && set==base+kIntSet) {
                const int32_t none=0;
                if(reinterpret_cast<IntSet>(set)(reinterpret_cast<void*>(component),&none)) {
                    start_sent.store(false,std::memory_order_release);
                    lobby=ReadLobby(base,session,simulation);
                }
            }
        }
        AcquireSRWLockExclusive(&probe_lock); probe=lobby; ReleaseSRWLockExclusive(&probe_lock);
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

// Lobby-owner patch. The aligned qword at 14236f828 holds the tail of
// "lea rcx,[g_HostInitDesc]" and the whole "call cand_HostInitConfigured" in
// 1409cdbf0's cold block; replacing the call with "mov al,1; nop x3" takes the
// game's own host-init branch (no owner assigned). One atomic 8-byte exchange.
constexpr uintptr_t kOwnerSite=0x236f828;
constexpr uint64_t kOwnerNative=0xfe63699ce802a53aull,kOwnerPatched=0x90909001b002a53aull;
DWORD SwapOwnerSite(uintptr_t base,uint64_t expected,uint64_t replacement) noexcept {
    uint8_t before[4]={},after[6]={};
    static const uint8_t lea[4]={0x48,0x8d,0x0d,0x25},test[6]={0x84,0xc0,0x0f,0x85,0x38,0xe4};
    if(!CopyAddress(base+kOwnerSite-4,before,sizeof(before)) || std::memcmp(before,lea,sizeof(lea)) ||
        !CopyAddress(base+kOwnerSite+8,after,sizeof(after)) || std::memcmp(after,test,sizeof(test))) return ERROR_INVALID_FUNCTION;
    const auto site=reinterpret_cast<volatile LONG64*>(base+kOwnerSite);
    DWORD old=0;
    if(!VirtualProtect(const_cast<LONG64*>(site),8,PAGE_EXECUTE_READWRITE,&old)) return GetLastError();
    const auto seen=uint64_t(InterlockedCompareExchange64(site,LONG64(replacement),LONG64(expected)));
    DWORD ignored=0; VirtualProtect(const_cast<LONG64*>(site),8,old,&ignored);
    FlushInstructionCache(GetCurrentProcess(),const_cast<LONG64*>(site),8);
    return seen==expected || seen==replacement ? ERROR_SUCCESS : ERROR_INVALID_FUNCTION;
}
// Swaps the int/byte table +0x78 applies for the request filter hooks, or back.
// A slot that holds neither expected value is left untouched.
SlotProtection int_apply_protection,byte_apply_protection;
DWORD SwapApplySlot(uintptr_t base,uintptr_t table,uintptr_t native,void* hook,bool install,SlotProtection& state) noexcept {
    const auto slot=reinterpret_cast<void* volatile*>(base+table+0x78);
    uintptr_t observed=0;
    if(!Read(base+table+0x78,observed)) return ERROR_READ_FAULT;
    void* const from=install ? reinterpret_cast<void*>(base+native) : hook;
    void* const to=install ? hook : reinterpret_cast<void*>(base+native);
    if(observed==reinterpret_cast<uintptr_t>(to)) return ERROR_SUCCESS;
    return ReplaceTickSlot(slot,from,to,state,slot_functions);
}
DWORD SetRequestFilter(uintptr_t base,bool enable) noexcept {
    if(!enable) filter_active.store(false,std::memory_order_release);
    DWORD result=SwapApplySlot(base,kIntTable,kIntApply,reinterpret_cast<void*>(&HookIntApply),enable,int_apply_protection);
    if(result==ERROR_SUCCESS || !enable)
        result=SwapApplySlot(base,kByteTable,kByteApply,reinterpret_cast<void*>(&HookByteApply),enable,byte_apply_protection)==ERROR_SUCCESS ? result : ERROR_INVALID_DATA;
    if(enable && result!=ERROR_SUCCESS)
        SwapApplySlot(base,kIntTable,kIntApply,reinterpret_cast<void*>(&HookIntApply),false,int_apply_protection);
    if(enable && result==ERROR_SUCCESS) filter_active.store(true,std::memory_order_release);
    return result;
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
        if(owner_patched.exchange(false)) {
            const auto owner_site=SwapOwnerSite(base,kOwnerPatched,kOwnerNative);
            if(result==ERROR_SUCCESS) result=owner_site;
        }
        if(server_owned.exchange(false)) {
            const auto filter=SetRequestFilter(base,false);
            if(result==ERROR_SUCCESS) result=filter;
        }
    }
    start_request.store(0); name_request.store(0); leader_request.store(0); leader_xuid.store(0); leader_clear.store(false);
    AcquireSRWLockExclusive(&selection_lock); lock_active=false; ReleaseSRWLockExclusive(&selection_lock);
    ReleaseSRWLockExclusive(&installation); return result;
}
namespace {
BackendReport WithLockGates(BackendReport report) noexcept {
    ContentSelection entry;
    if(LockedEntry(entry)) report.gates|=GateLocked;
    if(intercepted.load(std::memory_order_acquire)) report.gates|=GateIntercepted;
    AcquireSRWLockShared(&probe_lock); report.lobby=probe; ReleaseSRWLockShared(&probe_lock);
    return report;
}
}
BackendReport BackendStart(uint32_t wait_ms) noexcept {
    auto report=commands.Status();
    if(!running.load(std::memory_order_acquire)) { report.code=CodePending; return WithLockGates(report); }
    if(wait_ms>2000) wait_ms=2000;
    const uint64_t ticket=++start_sequence;
    uint64_t idle=0;
    if(!start_request.compare_exchange_strong(idle,ticket)) { report.code=CodeBusy; return WithLockGates(report); }
    const ULONGLONG deadline=GetTickCount64()+wait_ms;
    while(start_completed.load(std::memory_order_acquire)!=ticket && GetTickCount64()<deadline) Sleep(5);
    uint64_t queued=ticket;
    // Withdraw an undispatched command; one the tick already took completes shortly.
    if(start_completed.load(std::memory_order_acquire)!=ticket && !start_request.compare_exchange_strong(queued,0))
        for(unsigned i=0;i<100 && start_completed.load(std::memory_order_acquire)!=ticket;++i) Sleep(5);
    report=commands.Status();
    report.code=start_completed.load(std::memory_order_acquire)==ticket ? start_code.load(std::memory_order_acquire) : uint16_t(CodePending);
    return WithLockGates(report);
}
BackendReport BackendSetName(const uint16_t* units,uint32_t wait_ms) noexcept {
    auto report=commands.Status();
    if(!running.load(std::memory_order_acquire)) { report.code=CodePending; return WithLockGates(report); }
    if(wait_ms>2000) wait_ms=2000;
    const uint64_t ticket=++name_sequence;
    AcquireSRWLockExclusive(&name_lock);
    uint64_t idle=0;
    const bool queued=name_request.load(std::memory_order_acquire)==0;
    if(queued) std::memcpy(pending_name,units,sizeof(pending_name));
    ReleaseSRWLockExclusive(&name_lock);
    if(!queued || !name_request.compare_exchange_strong(idle,ticket)) { report.code=CodeBusy; return WithLockGates(report); }
    const ULONGLONG deadline=GetTickCount64()+wait_ms;
    while(name_completed.load(std::memory_order_acquire)!=ticket && GetTickCount64()<deadline) Sleep(5);
    uint64_t waiting=ticket;
    // Withdraw an undispatched command; one the tick already took completes shortly.
    if(name_completed.load(std::memory_order_acquire)!=ticket && !name_request.compare_exchange_strong(waiting,0))
        for(unsigned i=0;i<100 && name_completed.load(std::memory_order_acquire)!=ticket;++i) Sleep(5);
    report=commands.Status();
    report.code=name_completed.load(std::memory_order_acquire)==ticket ? name_code.load(std::memory_order_acquire) : uint16_t(CodePending);
    return WithLockGates(report);
}
BackendReport BackendSetLeader(uint64_t xuid,uint32_t wait_ms) noexcept {
    auto report=commands.Status();
    if(!running.load(std::memory_order_acquire)) { report.code=CodePending; return WithLockGates(report); }
    if(wait_ms>2000) wait_ms=2000;
    // The held value takes effect on the next tick whether or not this call waits for it.
    leader_clear.store(xuid==0,std::memory_order_release);
    leader_xuid.store(xuid,std::memory_order_release);
    const uint64_t ticket=++leader_sequence;
    uint64_t idle=0;
    if(!leader_request.compare_exchange_strong(idle,ticket)) { report.code=CodeBusy; return WithLockGates(report); }
    const ULONGLONG deadline=GetTickCount64()+wait_ms;
    while(leader_completed.load(std::memory_order_acquire)!=ticket && GetTickCount64()<deadline) Sleep(5);
    uint64_t waiting=ticket;
    if(leader_completed.load(std::memory_order_acquire)!=ticket && !leader_request.compare_exchange_strong(waiting,0))
        for(unsigned i=0;i<100 && leader_completed.load(std::memory_order_acquire)!=ticket;++i) Sleep(5);
    report=commands.Status();
    report.code=leader_completed.load(std::memory_order_acquire)==ticket ? leader_code.load(std::memory_order_acquire) : uint16_t(CodePending);
    return WithLockGates(report);
}
uint32_t BackendServerOwned(uint32_t mode) noexcept {
    if(mode>ServerOwnedFilterOnly) return ERROR_INVALID_PARAMETER;
    AcquireSRWLockExclusive(&installation);
    const auto base=image.load(); DWORD result=ERROR_NOT_READY;
    if(running.load() && base) {
        const bool filter=mode!=ServerOwnedOff,no_owner=mode==ServerOwnedNoOwner;
        result=SetRequestFilter(base,filter);
        if(result==ERROR_SUCCESS) {
            result=no_owner ? SwapOwnerSite(base,kOwnerNative,kOwnerPatched) : SwapOwnerSite(base,kOwnerPatched,kOwnerNative);
            if(result!=ERROR_SUCCESS && filter && !server_owned.load()) SetRequestFilter(base,false);
        }
        if(result==ERROR_SUCCESS) {
            server_owned.store(filter,std::memory_order_release);
            owner_patched.store(no_owner,std::memory_order_release);
        }
    }
    ReleaseSRWLockExclusive(&installation); return result;
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
