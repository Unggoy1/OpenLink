#define WIN32_LEAN_AND_MEAN
#include <windows.h>
#include <bcrypt.h>
#include "game_b002.h"
#include "engine_mailbox.h"
#include "tick_slot.h"
#include "image_validation.h"
#include "simulation_state.h"
#include "beacon_name.h"
#include "team_guard.h"
#include "bot_backfill.h"
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

// Team diagnostics (LobbyProbe team fields): plain reads, no game calls.
constexpr uintptr_t kLobbyVariant=0x542c0,kVariantTeams=0x10bc,kGameGlobals=0x5121d28,kGameIndex=0x45c5838;
std::atomic<uint32_t> team_fixes{0}; // TickTeamGuard corrections
int8_t VariantTeams(uintptr_t variant) noexcept {
    uint8_t teams=0;
    return variant && Read(variant+kVariantTeams,teams) ? int8_t(teams!=0) : int8_t(-1);
}
void ReadTeams(uintptr_t base,uintptr_t session,uintptr_t simulation,LobbyProbe& p) noexcept {
    std::memset(p.peer_team,0xff,sizeof(p.peer_team));
    p.lobby_variant_teams=p.game_variant_teams=-1; p.game_state=-1;
    uint8_t available=0,present=0;
    if(simulation && Read(simulation+kLobbyVariant+0xc0,available) && (available&1) &&
        Read(simulation+kLobbyVariant+0xc8,present) && present)
        p.lobby_variant_teams=VariantTeams(simulation+kLobbyVariant+0xd0);
    uintptr_t globals=0; uint16_t index=0;
    if(Read(base+kGameGlobals,globals) && globals && Read(base+kGameIndex,index)) {
        const uintptr_t game=globals+uintptr_t(index)*0x1134f0;
        if(Read(game,p.game_state)) p.game_variant_teams=VariantTeams(game+0x28);
    }
    Read(base+0x4dcf9ac,p.last_teams_enabled); Read(base+0x4dcf9b0,p.last_team_count); Read(base+0x4dc340c,p.forced_team_count);
    p.team_fixes=uint8_t(team_fixes.load()>255 ? 255 : team_fixes.load());
    if(!session || !(p.flags&LobbyValid)) return;
    for(unsigned i=0;i<16;++i) {
        if(!((p.peer_mask>>i)&1)) continue;
        const uintptr_t peer=session+uintptr_t(i)*0x1470;
        Read(peer+0x2505,p.peer_team[i][0]); Read(peer+0x2cf5,p.peer_team[i][1]); Read(peer+0x2cf6,p.peer_team[i][2]);
    }
}

// FFA team guard. When the match's game variant (simulation+0x542c0) has teams
// off, the game's own rule (142ebfcbc with teamsEnabled false) puts each peer on
// its own team: assigned (+0x2cf5) = peer index, or 31 if the peer requested the
// observer team (+0x2505). This re-applies that rule on the engine tick if a peer
// differs, with the same change notification (session +0x68 and +0x54d14), so no
// two players can share a team in FFA. Normally a no-op: the game already did it.
//
// Team balance (TeamBalance policy): once per match, when match prep has put a
// team mode into that variant (HostPreGame with a start requested, or
// HostStarting), the non-observer peers are spread over TeamCount teams with
// AssignTeams (team_guard.h; even or shuffle, team count or size from the
// playlist entry), written to assigned +0x2cf5 and selected +0x2cf6, and their
// carried-over requests (+0x2505) are cleared so the game's own pass (142ebfcbc)
// keeps the result. The next lobby clears that variant (absent until prep),
// which re-arms the balance.
bool WriteAddress(uintptr_t address,const void* source,size_t size) noexcept;
std::atomic<uint32_t> team_policy{TeamGuardFfa},team_mode{TeamModeEven},team_count{0},team_size{0};
std::atomic<uint32_t> match_teams{0}; // teams the balance used this match (bot backfill), 0 none
bool balanced=false; // engine thread only
void BalanceTeams(uintptr_t session,uint32_t mask,bool& changed) noexcept {
    unsigned peers[kMaxTeamSize],n=0; int8_t current[kMaxTeamSize],want[kMaxTeamSize],requests[kMaxTeamSize];
    for(unsigned i=0;i<32 && n<kMaxTeamSize;++i) {
        if(!((mask>>i)&1)) continue;
        const uintptr_t peer=session+uintptr_t(i)*0x1470;
        int8_t requested=0,assigned=0,selected=0;
        if(!Read(peer+0x2505,requested) || !Read(peer+0x2cf5,assigned) || !Read(peer+0x2cf6,selected) ||
            IsObserver(requested,assigned,selected)) continue;
        peers[n]=i; current[n]=assigned; requests[n]=requested; ++n;
    }
    if(!n) return;
    LARGE_INTEGER seed={}; QueryPerformanceCounter(&seed);
    const unsigned teams=TeamCount(n,team_count.load(),team_size.load());
    match_teams.store(teams);
    AssignTeams(n,current,teams,team_mode.load(),uint64_t(seed.QuadPart)|1,want);
    for(unsigned k=0;k<n;++k) {
        const uintptr_t peer=session+uintptr_t(peers[k])*0x1470; const int8_t none=-1;
        int8_t selected=0; Read(peer+0x2cf6,selected);
        if(want[k]==current[k] && selected==want[k] && requests[k]==none) continue;
        if(WriteAddress(peer+0x2cf5,&want[k],1) && WriteAddress(peer+0x2cf6,&want[k],1) && WriteAddress(peer+0x2505,&none,1)) changed=true;
    }
}
void NotifyTeams(uintptr_t session) noexcept {
    int32_t a=0,b=0;
    if(Read(session+0x68,a) && Read(session+0x54d14,b)) { ++a; ++b; WriteAddress(session+0x68,&a,4); WriteAddress(session+0x54d14,&b,4); }
    const uint32_t n=team_fixes.load(); if(n!=UINT32_MAX) team_fixes.store(n+1);
}
void TickTeams(uintptr_t session,uintptr_t simulation,int32_t state,bool start_requested) noexcept {
    uint8_t available=0,present=0,teams=0; uint32_t mask=0;
    if(!session || !simulation || !Read(simulation+kLobbyVariant+0xc0,available) || !(available&1) ||
        !Read(simulation+kLobbyVariant+0xc8,present) || !present) { balanced=false; match_teams.store(0); return; }
    if(!Read(simulation+kLobbyVariant+0xd0+kVariantTeams,teams) || !Read(session+0xa4,mask)) return;
    const uint32_t policy=team_policy.load(std::memory_order_acquire);
    bool changed=false;
    if(!teams && (policy&TeamGuardFfa)) {
        for(unsigned i=0;i<32;++i) {
            if(!((mask>>i)&1)) continue;
            const uintptr_t peer=session+uintptr_t(i)*0x1470;
            int8_t requested=0,assigned=0,want=0;
            if(!Read(peer+0x2505,requested) || !Read(peer+0x2cf5,assigned) || !FfaTeamFix(i,requested,assigned,want)) continue;
            if(WriteAddress(peer+0x2cf5,&want,1)) changed=true;
        }
    }
    const bool before_spawn=(state==HostPreGame && start_requested) || state==HostStarting;
    if(!balanced && before_spawn) {
        balanced=true;
        if(teams && (policy&TeamBalance)) BalanceTeams(session,mask,changed);
        // The entry's team count/size apply to this match only: if the next
        // entry's rules never arrive, that match falls back to two teams.
        team_count.store(0); team_size.store(0);
    }
    if(changed) NotifyTeams(session);
}

// Bot backfill (FN029, bot_backfill.h). The engine schedules its per-tick bot
// update as a std::function job (140bd55d0, only while the match's game options
// have bots enabled, +0x229); the job's call slot is vtable 1436ac278 +0x10 =
// 140d9f53c, which runs 1407017d8 (bot AI, removals, the mode's own backfill).
// HookBotJob runs that, then TickBots, in the same place and thread as the
// engine's own backfill (142c30320). TickBots makes at most one change per tick
// and only while the engine's gate 142c2b850 allows bot changes (running game,
// every machine joined, 100 ms since the last change, no bot mid-change). Bots
// are created with the script native 142a171c8(team, difficulty), as content
// scripts do, and removed with 142c2d090(bot handle).
constexpr uintptr_t kBotJobTable=0x36ac278,kBotJob=0xd9f53c,kBotAdd=0x2a171c8,kBotRemove=0x2c2d090,kBotGate=0x2c2b850;
constexpr uintptr_t kParticipantTable=0x367f9b0,kParticipantInit=0x4a2e98,kParticipantNext=0x497a5c,kFreeTeam=0x2c292fc;
constexpr uintptr_t kThreadRegistry=0x4948f40; // 64 x {tid +0, role +4} stride 0x60 (14051f988: role 1 = main)
using BotJob=void (*)(void*);
using BotAdd=void (*)(uint32_t,uint16_t);
using BotRemove=uint64_t (*)(uint32_t,uint64_t,uint64_t,uint64_t);
using BotGate=uint8_t (*)();
using FreeTeam=int32_t (*)();
using ParticipantInit=void (*)(void*);
using ParticipantNext=uint8_t (*)(void*);
SlotProtection bot_job_protection;
std::atomic<bool> bots_supported{false};
std::atomic<uint32_t> bot_flags{0},bot_fill{0},bot_max{kMaxBots},bot_difficulty{BotMarine};
std::atomic<uint32_t> bot_ticks{0},bot_adds{0},bot_removes{0},bot_refused{0};
std::atomic<int32_t> bot_count{-1},bot_humans{-1},bot_thread_role{-1};
std::atomic<uint8_t> bot_state{BotStateNone};
bool ValidateBots(uintptr_t base) noexcept {
    struct Fingerprint { uintptr_t rva; uint8_t bytes[16]; };
    static const Fingerprint targets[]={
        {kBotJob,{0x48,0x83,0xec,0x28,0xe8,0x43,0x9c,0x72,0x01,0x48,0x8b,0x0d,0xd4,0x07,0x11,0x04}},
        {kBotAdd,{0x48,0x89,0x5c,0x24,0x08,0x57,0x48,0x83,0xec,0x20,0x8b,0xf9,0x0f,0xb7,0xda,0x48}},
        {kBotRemove,{0x48,0x89,0x5c,0x24,0x10,0x48,0x89,0x6c,0x24,0x18,0x48,0x89,0x74,0x24,0x20,0x41}},
        {kBotGate,{0x48,0x89,0x5c,0x24,0x18,0x48,0x89,0x74,0x24,0x20,0x55,0x48,0x8b,0xec,0x48,0x83}},
        {kParticipantInit,{0x40,0x53,0x48,0x83,0xec,0x20,0x48,0x8b,0xd9,0xff,0x15,0x21,0x21,0xa1,0x04,0x48}},
        {kParticipantNext,{0x48,0x89,0x5c,0x24,0x08,0x48,0x89,0x6c,0x24,0x10,0x48,0x89,0x74,0x24,0x18,0x57}},
        {kFreeTeam,{0x48,0x89,0x5c,0x24,0x08,0x57,0x48,0x83,0xec,0x50,0x48,0x8d,0x05,0xa3,0x66,0xa5}}
    };
    for(const auto& target:targets) {
        uint8_t observed[16]={};
        if(!CopyAddress(base+target.rva,observed,sizeof(observed)) || std::memcmp(observed,target.bytes,sizeof(observed))) return false;
    }
    uintptr_t job=0,next=0;
    return Read(base+kBotJobTable+0x10,job) && job==base+kBotJob && Read(base+kParticipantTable,next) && next==base+kParticipantNext;
}
// The loaded game options (also read by ReadTeams), or 0.
uintptr_t LoadedVariant(uintptr_t base) noexcept {
    uintptr_t globals=0; uint16_t index=0;
    if(!Read(base+kGameGlobals,globals) || !globals || !Read(base+kGameIndex,index)) return 0;
    return globals+uintptr_t(index)*0x1134f0+0x28;
}
// BotModeFlag bits for the mode's own bots: backfill (+0x232), per-team (+0x22a..+0x231) or FFA (+0x233) counts.
uint8_t ModeBots(uintptr_t variant) noexcept {
    uint8_t settings[12]={}; uint8_t flags=0;
    if(!variant || !CopyAddress(variant+0x229,settings,sizeof(settings))) return 0;
    if(settings[9]) flags|=BotModeBackfill;
    for(unsigned t=1;t<=8;++t) if(settings[t]) flags|=BotModeTeams;
    if(settings[10]) flags|=BotModeFfa;
    return flags;
}
int32_t ThreadRole() noexcept {
    const DWORD self=GetCurrentThreadId();
    const uintptr_t base=image.load(std::memory_order_acquire);
    for(unsigned i=0;i<64;++i) {
        DWORD tid=0; int32_t role=0;
        if(!Read(base+kThreadRegistry+uintptr_t(i)*0x60,tid)) return -1;
        if(tid==self) return Read(base+kThreadRegistry+uintptr_t(i)*0x60+4,role) ? role : -1;
    }
    return -1;
}
void Saturate(std::atomic<uint32_t>& counter) noexcept { const uint32_t n=counter.load(); if(n!=UINT32_MAX) counter.store(n+1); }
// The match's participants through the engine's iterator (vtable 14367f9b0,
// 1404a2e98/140497a5c, current participant at +0x28; leaving players and bots
// queued for removal are skipped, as for 142c292fc and Lua Bot_RemoveAll).
unsigned ReadParticipants(uintptr_t base,BotParticipant* out,unsigned capacity) noexcept {
    alignas(16) uintptr_t iterator[16]={};
    iterator[0]=base+kParticipantTable;
    reinterpret_cast<ParticipantInit>(base+kParticipantInit)(iterator);
    unsigned n=0;
    for(unsigned guard=0;guard<64 && reinterpret_cast<ParticipantNext>(base+kParticipantNext)(iterator);++guard) {
        const uintptr_t participant=iterator[5];
        int32_t machine=0,handle=-1; uint8_t team=0;
        if(n>=capacity || !participant || !Read(participant+0x538,machine) || !Read(participant+0x53c,handle) || !Read(participant+0x285,team)) continue;
        out[n].bot=handle!=-1; out[n].team=int8_t(team^0x9e); out[n].handle=uint32_t(handle);
        if(!out[n].bot && machine==-1) continue; // neither player nor bot
        ++n;
    }
    return n;
}
// Bot navigation of the loaded map (A074): the server's HavokAIManager
// (*144976770, created at map load) holds the hkaiWorld at +0x454460, whose
// streaming collection (+0x130) has n (+0x40) 0x110-byte entries (+0x38) with
// the navmesh instance at +0; instance +0x20 -> data, face count at data +0x20.
// 343 maps load it from the scenario's pathfinding tag, Forge maps from the map
// variant's baked "Navigation" content (then byte 144708629 is set). Plain
// reads only. Returns the face total, or -1 when a pointer is missing.
constexpr uintptr_t kAiManager=0x4976770,kNavFromVariant=0x4708629;
int32_t NavmeshFaces(uintptr_t base) noexcept {
    uintptr_t manager=0,world=0,collection=0,entries=0; int32_t n=0;
    if(!Read(base+kAiManager,manager) || !manager || !Read(manager+0x454460,world) || !world ||
        !Read(world+0x130,collection) || !collection || !Read(collection+0x40,n) || n<0 || n>1024 ||
        (n && (!Read(collection+0x38,entries) || !entries))) return -1;
    int64_t faces=0;
    for(int32_t i=0;i<n;++i) {
        uintptr_t instance=0,data=0; int32_t count=0;
        if(!Read(entries+uintptr_t(i)*0x110,instance)) return -1;
        if(!instance) continue;
        if(!Read(instance+0x20,data) || !data || !Read(data+0x20,count) || count<0) return -1;
        faces+=count;
    }
    return faces>INT32_MAX ? INT32_MAX : int32_t(faces);
}
std::atomic<int32_t> nav_faces{-1};
std::atomic<int8_t> nav_state{-1}; // -1 unknown, 0 no navmesh (two zero samples >= 1 s apart), 1 navmesh
ULONGLONG nav_zero_since=0,bot_last_tick=0; // bot job thread only
// Updates nav_state; a new match (no bot tick for 2 s) starts over.
void TickNavmesh(uintptr_t base) noexcept {
    const ULONGLONG now=GetTickCount64();
    if(now-bot_last_tick>2000) { nav_state.store(-1); nav_zero_since=0; }
    bot_last_tick=now;
    const int32_t faces=NavmeshFaces(base); nav_faces.store(faces);
    if(faces>0) { nav_state.store(1); nav_zero_since=0; }
    else if(faces<0) nav_zero_since=0;
    else if(nav_state.load()!=1) {
        if(!nav_zero_since) nav_zero_since=now;
        else if(now-nav_zero_since>=1000) nav_state.store(0);
    }
}
// Registered bot difficulties (A075): bot manager *144eafd20 +0x8bac holds ten
// int32 tag handles indexed by difficulty code, -1 when unset; bot init clears
// them each map load and only the Lua native Bot_SetDifficultyTagRef (142a17fc8)
// fills them, so a mode without bot scripts has none. Bit i = code i set.
constexpr uintptr_t kBotManager=0x4eafd20;
uint16_t BotDifficultyMask(uintptr_t base) noexcept {
    uintptr_t manager=0; int32_t handles[10]={};
    if(!Read(base+kBotManager,manager) || !manager || !CopyAddress(manager+0x8bac,handles,sizeof(handles))) return 0;
    uint16_t mask=0;
    for(unsigned i=0;i<10;++i) if(handles[i]!=-1) mask|=uint16_t(1u<<i);
    return mask;
}
void TickBots(uintptr_t base) noexcept {
    Saturate(bot_ticks);
    if(bot_thread_role.load()<0) bot_thread_role.store(ThreadRole()); // the job thread does not change
    TickNavmesh(base);
    const uintptr_t variant=LoadedVariant(base);
    BotParticipant participants[32]; const unsigned n=ReadParticipants(base,participants,32);
    int32_t bots=0; for(unsigned i=0;i<n;++i) if(participants[i].bot) ++bots;
    bot_count.store(bots); bot_humans.store(int32_t(n)-bots);
    if(ModeBots(variant)) { bot_state.store(BotStateModeBots); return; }
    if(!(bot_flags.load(std::memory_order_acquire)&BotBackfill)) { bot_state.store(BotStateOff); return; }
    if(!reinterpret_cast<BotGate>(base+kBotGate)()) { bot_state.store(BotStateWaiting); return; }
    // No navigation: bots would only stand around. Remove any we added, add none.
    if(nav_state.load()==0) {
        bot_state.store(BotStateNoNavmesh);
        for(unsigned i=0;i<n;++i) if(participants[i].bot) {
            reinterpret_cast<BotRemove>(base+kBotRemove)(participants[i].handle,0,0,0);
            Saturate(bot_removes); break;
        }
        return;
    }
    uint8_t teams_enabled=0;
    const unsigned teams=variant && Read(variant+kVariantTeams,teams_enabled) && teams_enabled ? (match_teams.load() ? match_teams.load() : 2u) : 0u;
    const BotAction action=DecideBots(participants,n,bot_fill.load(),bot_max.load(),teams);
    if(action.kind==BotAction::None) { bot_state.store(BotStateFilled); return; }
    if(action.kind==BotAction::Remove) {
        reinterpret_cast<BotRemove>(base+kBotRemove)(action.handle,0,0,0);
        Saturate(bot_removes); bot_state.store(BotStateChanged); return;
    }
    // The engine creates a bot only for a difficulty whose tag the mode's scripts
    // registered (Bot_SetDifficultyTagRef); use the configured one if it is, else
    // the nearest registered one, else add nothing.
    const uint16_t difficulty=BotDifficultyFallback(BotDifficultyCode(bot_difficulty.load()),BotDifficultyMask(base));
    if(!difficulty) { Saturate(bot_refused); bot_state.store(BotStateRefused); return; }
    int32_t team=action.team;
    if(team<0) team=reinterpret_cast<FreeTeam>(base+kFreeTeam)();
    if(team<0) { Saturate(bot_refused); bot_state.store(BotStateRefused); return; }
    reinterpret_cast<BotAdd>(base+kBotAdd)(uint32_t(team),difficulty);
    BotParticipant after[32]; const unsigned m=ReadParticipants(base,after,32);
    int32_t now=0; for(unsigned i=0;i<m;++i) if(after[i].bot) ++now;
    if(now>bots) { Saturate(bot_adds); bot_state.store(BotStateChanged); bot_count.store(now); }
    else { Saturate(bot_refused); bot_state.store(BotStateRefused); }
}
void HookBotJob(void* job) {
    const uintptr_t base=image.load(std::memory_order_acquire);
    reinterpret_cast<BotJob>(base+kBotJob)(job); // the engine's bot update, exactly once
    if(running.load(std::memory_order_acquire) && bots_supported.load(std::memory_order_acquire)) TickBots(base);
}
uint16_t SaturatedU16(const std::atomic<uint32_t>& counter) noexcept { const uint32_t n=counter.load(); return uint16_t(n>65535 ? 65535 : n); }
void ReadBots(uintptr_t base,LobbyProbe& p) noexcept {
    const uintptr_t variant=LoadedVariant(base); uint8_t enabled=0;
    p.bots_enabled=variant && Read(variant+0x229,enabled) ? int8_t(enabled!=0) : int8_t(-1);
    p.mode_bots=ModeBots(variant);
    p.bot_count=int8_t(bot_count.load()); p.bot_humans=int8_t(bot_humans.load());
    p.bot_state=bot_state.load(); p.bot_thread_role=int8_t(bot_thread_role.load());
    p.bot_adds=SaturatedU16(bot_adds); p.bot_removes=SaturatedU16(bot_removes); p.bot_refused=SaturatedU16(bot_refused);
    p.bot_ticks=bot_ticks.load(); p.bot_supported=bots_supported.load() ? 1 : 0;
    p.nav_state=nav_state.load(); p.nav_faces=nav_faces.load(); Read(base+kNavFromVariant,p.nav_from_variant);
    p.bot_difficulties=BotDifficultyMask(base);
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
    ReadTeams(base,session,simulation,p);
    ReadBots(base,p);
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
        {
            uintptr_t mode_table=0; uint8_t mode_available=0; int32_t start_mode=0;
            const bool start_requested=simulation && Read(simulation+kStartMode,mode_table) && mode_table==base+kIntTable &&
                Read(simulation+kStartMode+0xc0,mode_available) && (mode_available&1) && Read(simulation+kStartMode+0xc8,start_mode) && start_mode==1;
            TickTeams(session,simulation,frame.state,start_requested);
        }
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
DWORD SwapBotJob(uintptr_t base,void* expected,void* replacement) noexcept {
    const auto slot=reinterpret_cast<void* volatile*>(base+kBotJobTable+0x10);
    return ReplaceTickSlot(slot,expected,replacement,bot_job_protection,slot_functions);
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
                // Bot backfill is optional: without verified bot functions the rest still runs.
                if(result==ERROR_SUCCESS && ValidateBots(base)) {
                    bots_supported.store(true,std::memory_order_release);
                    if(SwapBotJob(base,reinterpret_cast<void*>(base+kBotJob),reinterpret_cast<void*>(&HookBotJob))!=ERROR_SUCCESS) {
                        bots_supported.store(false,std::memory_order_release);
                        RestoreTickProtection(reinterpret_cast<void* volatile*>(base+kBotJobTable+0x10),bot_job_protection,slot_functions);
                    }
                }
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
        bots_supported.store(false,std::memory_order_release);
        uintptr_t job=0; DWORD bot_job=ERROR_SUCCESS;
        if(!Read(base+kBotJobTable+0x10,job)) bot_job=ERROR_READ_FAULT;
        else if(job==reinterpret_cast<uintptr_t>(&HookBotJob)) bot_job=SwapBotJob(base,reinterpret_cast<void*>(&HookBotJob),reinterpret_cast<void*>(base+kBotJob));
        const auto bot_cleanup=RestoreTickProtection(reinterpret_cast<void* volatile*>(base+kBotJobTable+0x10),bot_job_protection,slot_functions);
        if(result==ERROR_SUCCESS) result=bot_job==ERROR_SUCCESS ? bot_cleanup : bot_job;
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
uint32_t BackendTeamPolicy(uint32_t flags,uint32_t mode,uint32_t count,uint32_t size) noexcept {
    if((flags&~uint32_t(TeamGuardFfa|TeamBalance)) || mode>TeamModeShuffle || count>kMaxTeams || size>kMaxTeamSize)
        return ERROR_INVALID_PARAMETER;
    team_mode.store(mode); team_count.store(count); team_size.store(size);
    team_policy.store(flags,std::memory_order_release);
    return ERROR_SUCCESS;
}
uint32_t BackendBotPolicy(uint32_t flags,uint32_t fill_to,uint32_t max_bots,uint32_t difficulty) noexcept {
    if((flags&~uint32_t(BotBackfill)) || fill_to>kMaxBotFill || max_bots>kMaxBots || difficulty>BotSpartan ||
        ((flags&BotBackfill) && (fill_to<2 || !max_bots)))
        return ERROR_INVALID_PARAMETER;
    bot_fill.store(fill_to); bot_max.store(max_bots); bot_difficulty.store(difficulty);
    bot_flags.store(flags,std::memory_order_release);
    return ERROR_SUCCESS;
}
uint32_t BackendServerOwned(uint32_t mode) noexcept {
    if(mode>ServerOwnedNoOwner) return ERROR_INVALID_PARAMETER;
    AcquireSRWLockExclusive(&installation);
    const auto base=image.load(); DWORD result=ERROR_NOT_READY;
    if(running.load() && base) {
        const bool owned=mode==ServerOwnedNoOwner;
        result=SetRequestFilter(base,owned);
        if(result==ERROR_SUCCESS) {
            result=owned ? SwapOwnerSite(base,kOwnerNative,kOwnerPatched) : SwapOwnerSite(base,kOwnerPatched,kOwnerNative);
            if(result!=ERROR_SUCCESS && owned && !server_owned.load()) SetRequestFilter(base,false);
        }
        if(result==ERROR_SUCCESS) {
            server_owned.store(owned,std::memory_order_release);
            owner_patched.store(owned,std::memory_order_release);
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
