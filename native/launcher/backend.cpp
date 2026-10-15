#define WIN32_LEAN_AND_MEAN
#include "backend.h"
#include "boundary.h"
#include "../hostctl/tick_slot.h"
#include "../hostctl/image_validation.h"
#include <atomic>
#include <cstdio>
#include <cstring>
#include "b002_fingerprints.h"
#include "relay.h"
#include "text_hook.h"
#include "browser.h"
#include "browser_lua.h"
#include "cbrowser.h"
#include "version.h"
#include "vote.h"
#include "overlay.h"
#include "mapheader.h"
namespace clientjoin {
namespace {
constexpr uintptr_t kFrontSlot=0x3706f18,kPreSlot=0x3706fc8,kFront=0xcaf72c,kPre=0x6a772c;
using TickFunction=void (*)(void*);
SRWLOCK installation=SRWLOCK_INIT,mailbox=SRWLOCK_INIT;
hostctl::SlotProtection front_protection,pre_protection;
uintptr_t image=0;
bool front_installed=false,pre_installed=false,started_once=false;
std::atomic<bool> running{false};
std::atomic<DWORD> engine_thread{0};
JoinMachine machine;
Report report;
thread_local bool in_hook=false;
struct TickContext { void* self; bool front; };
bool Copy(uintptr_t address,void* destination,size_t size) noexcept {
    if(!address || !size || size>UINTPTR_MAX-address) return false;
    uintptr_t at=address,end=address+size;
    while(at<end) {
        MEMORY_BASIC_INFORMATION region={};
        if(!VirtualQuery(reinterpret_cast<void*>(at),&region,sizeof(region)) || region.State!=MEM_COMMIT ||
            (region.Protect&(PAGE_GUARD|PAGE_NOACCESS)) || !region.RegionSize) return false;
        uintptr_t start=reinterpret_cast<uintptr_t>(region.BaseAddress);
        if(region.RegionSize>UINTPTR_MAX-start || start+region.RegionSize<=at) return false;
        at=start+region.RegionSize;
    }
    __try { std::memcpy(destination,reinterpret_cast<void*>(address),size); return true; }
    __except(EXCEPTION_EXECUTE_HANDLER) { return false; }
}
template<class T> bool Read(uintptr_t address,T& value) noexcept { return Copy(address,&value,sizeof(value)); }
bool Fingerprints(uintptr_t base) noexcept {
    for(const auto& f:kFingerprints) {
        uint8_t bytes[16]={}; MEMORY_BASIC_INFORMATION region={};
        if(!VirtualQuery(reinterpret_cast<void*>(base+f.rva),&region,sizeof(region)) || region.AllocationBase!=reinterpret_cast<void*>(base) ||
            !(region.Protect&(PAGE_EXECUTE|PAGE_EXECUTE_READ|PAGE_EXECUTE_READWRITE|PAGE_EXECUTE_WRITECOPY)) ||
            !Copy(base+f.rva,bytes,16) || std::memcmp(bytes,f.bytes,16)) return false;
    }
    return true;
}
bool Validate(uintptr_t base) noexcept {
    wchar_t path[32768]; DWORD n=GetModuleFileNameW(nullptr,path,32768);
    IMAGE_DOS_HEADER dos={}; IMAGE_NT_HEADERS64 nt={}; uintptr_t front=0,pre=0; uint8_t server=1;
    return n && n<32768 && ValidateFile(path) && Read(base,dos) && dos.e_lfanew>=0 && dos.e_lfanew<=0x100000 &&
        Read(base+uintptr_t(dos.e_lfanew),nt) && hostctl::ValidateLoadedHeader(base,dos,nt,0x4f51240+0x78) &&
        Fingerprints(base) && Read(base+kFrontSlot,front) && front==base+kFront && Read(base+kPreSlot,pre) && pre==base+kPre &&
        Read(base+0x4eadd40,server) && !server;
}
template<class T> T Function(uintptr_t rva) { return reinterpret_cast<T>(image+rva); }
bool Boundary() noexcept {
    const DWORD e=CheckNoEac(GetCurrentProcessId()); uint8_t server=1;
    if(e || !Read(image+0x4eadd40,server) || server) { report.error=e ? e : ERROR_INVALID_FUNCTION; return false; }
    return true;
}
bool Switch(void*) {
    if(!Boundary()) return false;
    void* manager=Function<void* (*)()>(0x4f9770)();
    if(reinterpret_cast<uintptr_t>(manager)!=image+0x4f51240) { report.error=ERROR_INVALID_ADDRESS; return false; }
    alignas(16) uint8_t result[16]={};
    Function<void* (*)(void*,void*,uint32_t,uint32_t,uint8_t)>(0x2df649c)(manager,result,1,2,0);
    Function<void (*)(void*)>(0x45e9ac)(result);
    return true;
}
bool Arm(void* context,const Request& r) {
    const auto& tick=*static_cast<TickContext*>(context);
    if(!Boundary() || Function<uint32_t (*)(void*)>(0x519cfc)(reinterpret_cast<void*>(image+0x4f51240))!=1) return false;
    uintptr_t session=0; uint32_t session_mode=0xffffffff;
    if(!Read(image+0x4dc3200,session) || !session || !Read(session+0x5a078,session_mode) || session_mode!=(tick.front ? 0u : 5u)) {
        report.error=ERROR_INVALID_STATE; return false;
    }
    if(!tick.front) {
        int32_t state=-1,target=0;
        if(!Read(reinterpret_cast<uintptr_t>(tick.self)+0x60,state) || !Read(reinterpret_cast<uintptr_t>(tick.self)+0x50,target) || state!=0 || target!=-1) {
            report.error=ERROR_INVALID_STATE; return false;
        }
    }
    uint32_t mode=0xffffffff;
    if(!Read(image+0x4dc4cd0,mode) || (!(r.flags&2) && mode!=0)) { report.error=ERROR_INVALID_STATE; return false; }
    if(r.flags&2) Function<void (*)(const char*,const char*)>(0x2e9360c)(r.map,r.variant);
    Function<void (*)(const char*)>(0x2e97ebc)(r.host);
    // The existing pre-game loop already has a client session. Bypass belongs
    // only to the separate mode0 front-end start path, not this observed route.
    if(tick.front) Function<void (*)(bool)>(0x2e93544)(true);
    uint8_t armed=0,bypass=0;
    if(!Read(image+0x4dc4cd4,armed) || !Read(image+0x4dc4cc8,bypass) || !armed || (tick.front ? !bypass : bypass!=0)) { report.error=ERROR_INVALID_DATA; return false; }
    report.gates|=WasArmed; return true;
}
bool Start(void*) {
    if(!Boundary() || Function<uint32_t (*)(void*)>(0x519cfc)(reinterpret_cast<void*>(image+0x4f51240))!=1) return false;
    Function<void (*)(uint32_t)>(0xcaf830)(2); return true;
}
uint32_t Kind(void*) { return Function<uint32_t (*)(void*)>(0x519cfc)(reinterpret_cast<void*>(image+0x4f51240)); }
bool CanInitLobby(void* context) {
    const auto& tick=*static_cast<TickContext*>(context);
    if(tick.front || !Boundary()) { if(!report.error) report.error=ERROR_INVALID_STATE; return false; }
    uintptr_t engine_session=0; uint32_t engine_mode=0xffffffff; int32_t state=-1,target=0;
    if(!Read(image+0x4dc3200,engine_session) || !engine_session || !Read(engine_session+0x5a078,engine_mode) || engine_mode!=5 ||
        !Read(reinterpret_cast<uintptr_t>(tick.self)+0x60,state) || !Read(reinterpret_cast<uintptr_t>(tick.self)+0x50,target) || state!=0 || target!=-1) {
        report.error=ERROR_INVALID_STATE; return false;
    }
    uintptr_t table=0,getter=0;
    if(!Read(image+0x4f51240,table) || table!=image+0x36ee2e8 || !Read(table,getter) || getter!=image+0x51931c) {
        report.error=ERROR_INVALID_DATA; return false;
    }
    alignas(16) uintptr_t session[2]={};
    Function<void* (*)(void*,void*)>(0x51931c)(reinterpret_cast<void*>(image+0x4f51240),session);
    const bool exists=session[0]!=0;
    Function<void (*)(void*)>(0x45e9ac)(session);
    if(exists) { report.error=ERROR_ALREADY_EXISTS; return false; }
    return true;
}
bool BeginLanLobby(void* context) {
    if(!CanInitLobby(context) || Kind(nullptr)!=1) { if(!report.error) report.error=ERROR_INVALID_STATE; return false; }
    // Normal Create Match call shape: CustomGame=0, automatic=3, activate=1,
    // final flag=0 (14357e45d..). Here only network mode changes to explicit LAN1.
    // This queues the game's own lobby lifecycle; no ready/state bit is forced.
    Function<void (*)(int32_t,int32_t,uint8_t,uint8_t)>(0x2e95818)(0,1,1,0);
    report.gates|=LobbyInitRequested; return true;
}
// Browse vector 144dbfe30/38 (stride 0x60; +0 sixteen-byte address key, IPv4 first,
// rest zero). Stock picker callback 1420db830 passes an index to 1420da720, which runs
// 1420db2d0 on the registered thread or queues it there (O145). Read-only checks only
// choose the index; the selection itself is the stock call.
constexpr uintptr_t kBrowseBegin=0x4dbfe30,kBrowseEnd=0x4dbfe38,kSelectReady=0x4dc3180,kManager=0x4f51240,kLanService=0x3c90700;
constexpr uintptr_t kBrowseStride=0x60,kBrowseLimit=64;
uint8_t select_ip[4]={};
bool selection_sent=false;
bool ParseIPv4(const char* text,uint8_t (&out)[4]) noexcept {
    unsigned part=0,value=0,digits=0;
    for(const char* p=text;;++p) {
        if(*p>='0' && *p<='9') { value=value*10+unsigned(*p-'0'); if(++digits>3 || value>255) return false; continue; }
        if(!digits || part>3) return false;
        out[part++]=uint8_t(value); value=0; digits=0;
        if(!*p) return part==4;
        if(*p!='.') return false;
    }
}
bool KeyMatches(const uint8_t (&key)[16]) noexcept {
    if(std::memcmp(key,select_ip,4)) return false;
    for(unsigned i=4;i<16;++i) if(key[i]) return false;
    return true;
}
// Read-only: the LAN service's current target key (+0x984, active byte +0x980) names
// the selected address (O149 layout). Admission still needs real server evidence.
bool TargetNames() noexcept {
    uintptr_t wrapper=0,service=0,table=0; uint8_t active=0,key[16]={};
    return Read(image+kManager+0x20,wrapper) && wrapper && Read(wrapper,service) && service && Read(service,table) && table==image+kLanService &&
        Read(service+0x980,active) && active==1 && Copy(service+0x984,key,16) && KeyMatches(key);
}
int Select(void*,const Request& r) {
    if(selection_sent || !Boundary() || Kind(nullptr)!=1 || !ParseIPv4(r.host,select_ip)) { if(!report.error) report.error=ERROR_INVALID_STATE; return SelectRefused; }
    uintptr_t begin=0,end=0;
    if(!Read(image+kBrowseBegin,begin) || !Read(image+kBrowseEnd,end)) { report.error=ERROR_INVALID_DATA; return SelectRefused; }
    if(begin==end) return SelectWait;
    if(!begin || end<begin || (end-begin)%kBrowseStride || (end-begin)/kBrowseStride>kBrowseLimit) { report.error=ERROR_INVALID_DATA; return SelectRefused; }
    int32_t index=-1;
    for(uintptr_t i=0;i<(end-begin)/kBrowseStride;++i) {
        uint8_t key[16]={};
        if(!Copy(begin+i*kBrowseStride,key,16)) { report.error=ERROR_INVALID_DATA; return SelectRefused; }
        if(KeyMatches(key)) { index=int32_t(i); break; }
    }
    if(index<0) return SelectWait;
    report.gates|=BrowseMatch;
    uint8_t ready=0;
    if(!Read(image+kSelectReady,ready) || !ready) return SelectWait;
    // A target already naming this host would make the later confirmation ambiguous.
    if(TargetNames()) report.gates|=TargetPreset;
    // Same call shape as 1420db830: task result (16 bytes used; padded like the stock
    // frame's spare slot), pointer to the dword index, then release the task's control
    // block with 1404f965c.
    alignas(16) uintptr_t task[4]={};
    Function<void* (*)(void*,int32_t*)>(0x20da720)(task,&index);
    if(task[1]) Function<void (*)(uintptr_t)>(0x4f965c)(task[1]);
    selection_sent=true; report.gates|=SelectionDispatched;
    return SelectDispatched;
}
void SampleTarget() noexcept { if(TargetNames()) report.gates|=TargetSelected; }
// The front-end script manager (pointer global 0x14486ff58; lua_State* +0x28,
// CRITICAL_SECTION +0x180, L+0x98 -> manager) and its run-chunk helper 1414a70a0
// (manager, text, length, chunk name) -> 1 ok / 0 error, error logged and popped (O155).
constexpr uintptr_t kLuaManager=0x486ff58,kRunChunk=0x14a70a0;
// Persistent mode. The engine tick waits for the front-end UI
// scripts, installs the embedded browser Lua, then health-checks it every 5 s and
// reinstalls it (with natives) if the UI Lua state was recreated.
std::atomic<bool> persistent{false};
enum UiStage { UiWaiting,UiInstall,UiRunning };
UiStage ui_stage=UiWaiting; ULONGLONG ui_next=0; uint32_t ui_installs=0,ui_failures=0;
// Runs one chunk under the manager lock (never blocking). 1 ok, 0 Lua error; ERROR_BUSY/ERROR_INVALID_DATA otherwise.
DWORD RunUiChunk(const char* text,size_t length,uint32_t& ok) noexcept {
    uintptr_t manager=0,L=0,back=0;
    if(!Read(image+kLuaManager,manager) || !manager || !Read(manager+0x28,L) || !L || !Read(L+0x98,back) || back!=manager) return ERROR_INVALID_DATA;
    auto* lock=reinterpret_cast<CRITICAL_SECTION*>(manager+0x180);
    if(static_cast<DWORD>(reinterpret_cast<uintptr_t>(lock->OwningThread))==GetCurrentThreadId()) return ERROR_BUSY;
    if(!TryEnterCriticalSection(lock)) return ERROR_BUSY;
    if(!browser::RegisteredIn(L)) browser::RegisterNatives(image,L);
    const auto run=Function<uint64_t (*)(uintptr_t,const char*,size_t,const char*)>(kRunChunk);
    ok=(run(manager,text,length,"=openlink")&0xff)==1 ? 1u : 0u;
    LeaveCriticalSection(lock);
    return ERROR_SUCCESS;
}
// The anti-cheat boundary is checked before anything is installed; afterwards
// it is re-checked every 30 s on its own thread (module walks stay off
// the engine tick). If it ever fails, listing and joining stop for good.
std::atomic<DWORD> boundary_failure{0};
DWORD WINAPI WatchBoundary(void*) {
    for(;;) {
        Sleep(30000);
        const DWORD e=CheckNoEac(GetCurrentProcessId());
        if(e) { boundary_failure.store(e); browser::SetActive(false); cbrowser::Disable(); browser::Log("protection boundary check failed; OpenLink inactive"); return 0; }
    }
}
// After an OpenLink match the game returns to the LAN Create Match screen
// (CustomGameScreen), which stays loading until Back (O162/O174, FN064). A queued join arms
// this; the next front-end UI rebuild (seen only after leaving the match, O161/O162) starts a
// 60 s watch on the engine tick: our browser replaces Create Match (FN065); the
// fallback is 3 stuck samples 1 s apart -> stock Back, then our browser from the landing.
std::atomic<bool> post_armed{false};
bool post_watch=false; int post_step=0; uint32_t post_stuck=0,post_redirects=0; uint32_t post_down=0; ULONGLONG post_until=0,post_next=0,post_cooldown=0,post_down_at=0;
void ArmPostMatch() noexcept { post_armed.store(true); post_watch=false; } // a new join ends any watch
// The player left the server from its lobby (Back -> "Leave server?"): no redirect for that leave.
std::atomic<bool> post_cancel{false};
void CancelPostMatch() { post_armed.store(false); post_cancel.store(true); }
void StartPostMatchWatch(ULONGLONG now) {
    // A watch still pending (its window spent in a match: no front-end ticks there) restarts too.
    if(!post_armed.exchange(false) && !post_watch) return;
    post_watch=true; post_step=0; post_stuck=0; post_redirects=0; post_cooldown=0; post_until=now+60000; post_next=now+1000;
    browser::Log("returned from an OpenLink match; watching for the stuck lobby");
}
void TickPostMatch(ULONGLONG now) {
    if(post_cancel.exchange(false)) post_watch=false;
    if(!post_watch || ui_stage!=UiRunning || now<post_next) return;
    post_next=now+1000;
    // Packets from the server still arriving = the player stayed (a match ended normally and
    // they are in the server lobby). Track the last change so a quiet watch can re-arm for the next return.
    if(const uint32_t down=relay::Snapshot().down; down!=post_down) { post_down=down; post_down_at=now; }
    if(now>post_until) {
        post_watch=false;
        browser::Log(post_step ? "post-match: browser not reopened (landing not found)" : "post-match watch ended (lobby not stuck)");
        if(!post_step && !post_redirects && now-post_down_at<5000) { post_armed.store(true); browser::Log("post-match: still connected to the OpenLink server; watching the next return"); }
        return;
    }
    static const char kStuck[]="assert(OpenLinkUI and OpenLinkUI.lobbyStuck and OpenLinkUI.lobbyStuck())";
    static const char kBack[]="assert(OpenLinkUI.leaveStuck())";
    static const char kOpen[]="assert(OpenLinkUI and OpenLinkUI.openAfterBack and OpenLinkUI.openAfterBack())";
    uint32_t ok=0;
    // First choice, replace the LAN Create Match screen with our browser at once
    // (at most twice, 4 s apart, in case the lobby router brings Create Match back).
    if(post_step==0 && post_redirects<2) {
        if(now<post_cooldown) return;
        static const char kPresent[]="assert(OpenLinkUI and OpenLinkUI.lobbyPresent and OpenLinkUI.lobbyPresent())";
        static const char kRoot[]="assert(OpenLinkUI.openBrowserRoot())";
        const DWORD p=RunUiChunk(kPresent,sizeof(kPresent)-1,ok);
        if(p==ERROR_BUSY || p || !ok) return;
        const DWORD r=RunUiChunk(kRoot,sizeof(kRoot)-1,ok);
        if(r==ERROR_BUSY) return;
        if(!r && ok) { ++post_redirects; post_cooldown=now+4000; browser::Log("post-match: OpenLink Servers opened in place of Create Match"); return; }
        post_redirects=2; browser::Log("post-match: browser redirect failed; falling back to Back + reopen");
        return;
    }
    if(post_step==0) {
        if(now<post_cooldown) return;
        const DWORD e=RunUiChunk(kStuck,sizeof(kStuck)-1,ok);
        if(e==ERROR_BUSY) return;
        if(e || !ok) { post_stuck=0; return; }
        if(++post_stuck<3) return;
        const DWORD b=RunUiChunk(kBack,sizeof(kBack)-1,ok);
        browser::Log(!b && ok ? "post-match: stuck LAN lobby, pressed Back" : "post-match: Back failed");
        if(b || !ok) { post_watch=false; return; }
        post_step=1; post_until=now+15000;
        return;
    }
    const DWORD e=RunUiChunk(kOpen,sizeof(kOpen)-1,ok);
    if(!e && ok) { post_watch=false; browser::Log("post-match: OpenLink Servers reopened"); }
}
void TickPersistentUi() noexcept {
    const ULONGLONG now=GetTickCount64();
    if(const DWORD e=boundary_failure.load()) {
        if(report.gates&NoEac) { report.gates&=~uint32_t(NoEac); report.error=e; machine.Cancel(); }
        return;
    }
    // A refused browser join opens the stock OK dialog (its text comes from cbrowser).
    if(ui_stage==UiRunning && cbrowser::TakeNotice()) {
        static const char kNotice[]="if OpenLinkUI and OpenLinkUI.notify then OpenLinkUI.notify() end";
        uint32_t shown=0; const DWORD e=RunUiChunk(kNotice,sizeof(kNotice)-1,shown);
        if(e==ERROR_BUSY) cbrowser::RequeueNotice();
        else browser::Log(!e && shown ? "join refusal dialog requested" : "join refusal dialog failed");
    }
    TickPostMatch(now);
    // Lobby vote panel: the script reports whether the lobby is showing and where its host rows are.
    static ULONGLONG vote_next=0; static uint32_t vote_busy=0;
    if(ui_stage==UiRunning && now>=vote_next) {
        static const char kVoteTick[]="if OpenLinkUI and OpenLinkUI.voteTick then OpenLinkUI.voteTick() end";
        uint32_t ran=0;
        if(RunUiChunk(kVoteTick,sizeof(kVoteTick)-1,ran)!=ERROR_BUSY) {
            if(vote_busy>=20) { char note[80]; sprintf_s(note,"vote tick ran after %u busy tries",vote_busy); browser::Log(note); }
            vote_next=now+500; vote_busy=0;
        } else ++vote_busy;
    }    if(now<ui_next) return;
    static const char kReady[]="assert(type(AddListItemsToCUI)=='function' and type(stringid)=='function')";
    static const char kHealthy[]="assert(OpenLinkUI ~= nil and type(OpenLink)=='table' and AddListItemsToCUI ~= OpenLinkUI.original); if OpenLinkUI.wrapDispose then pcall(OpenLinkUI.wrapDispose) end";
    uint32_t ok=0; DWORD e=ERROR_SUCCESS;
    switch(ui_stage) {
    case UiWaiting:
        e=RunUiChunk(kReady,sizeof(kReady)-1,ok);
        if(e==ERROR_BUSY) return;
        if(!e && ok) { ui_stage=UiInstall; ui_next=now; browser::Log("front-end UI scripts ready"); return; }
        ui_next=now+2000; return;
    case UiInstall:
        e=RunUiChunk(kBrowserLua,sizeof(kBrowserLua)-1,ok);
        if(e==ERROR_BUSY) return;
        if(!e && ok) { ui_stage=UiRunning; ++ui_installs; ui_next=now+5000; browser::Log("browser installed"); return; }
        ++ui_failures; browser::Log("browser install failed; retrying"); ui_stage=UiWaiting; ui_next=now+5000; return;
    case UiRunning:
        e=RunUiChunk(kHealthy,sizeof(kHealthy)-1,ok);
        if(e==ERROR_BUSY) return;
        if(!e && !ok) { browser::Log("browser missing from the UI state; reinstalling"); StartPostMatchWatch(now); ui_stage=UiWaiting; ui_next=now; return; }
        ui_next=now+5000; return;
    }
}
void Sample(void* self,bool front) noexcept {
    uint8_t server=1,bypass=0,armed=0,passed=0; uint32_t mode=0xffffffff,init=0xffffffff; uintptr_t session=0;
    Read(image+0x4eadd40,server); Read(image+0x4dc4cc8,bypass); Read(image+0x4dc4cd4,armed); Read(image+0x4dc4cd0,init);
    if(Read(image+0x4dc3200,session) && session) Read(session+0x5a078,mode);
    report.mode=int32_t(mode); report.init_mode=int32_t(init); report.bypass=bypass; report.armed=armed;
    report.kind=server ? -1 : int32_t(Function<uint32_t (*)(void*)>(0x519cfc)(reinterpret_cast<void*>(image+0x4f51240)));
    report.gates&=~uint32_t(Client|Lan|ClientSession);
    if(!server) report.gates|=Client;
    if(report.kind==1) report.gates|=Lan;
    if(report.mode==5) report.gates|=ClientSession;
    if(front && Read(reinterpret_cast<uintptr_t>(self)+0x60,passed)) {
        report.passed=passed; if(passed && (report.gates&WasArmed)) report.gates|=GatePassed;
    }
    if(!front) {
        int32_t state=-1,target=-1;
        if(Read(reinterpret_cast<uintptr_t>(self)+0x60,state)) report.pre_state=state;
        if(Read(reinterpret_cast<uintptr_t>(self)+0x50,target)) report.pre_target=target;
    }
    if(selection_sent) SampleTarget();
    report.gates|=Ticking;
}
void EngineTick(void* self,bool front) {
    const auto original=Function<TickFunction>(front ? kFront : kPre);
    if(in_hook || !running.load(std::memory_order_acquire)) { original(self); return; }
    DWORD expected=0; engine_thread.compare_exchange_strong(expected,GetCurrentThreadId());
    if(engine_thread.load()!=GetCurrentThreadId()) { original(self); return; }
    in_hook=true;
    // A join requested by OpenLink.Join from the UI. Queued here, outside the
    // mailbox lock, because the native runs while our own Lua chunks may hold it.
    {
        char listing[40];
        if(browser::TakeJoin(listing)) {
            RelayJoin j={}; const auto& c=browser::Current();
            std::memcpy(j.directory,c.directory,sizeof(j.directory)); strcpy_s(j.listing,listing); std::memcpy(j.lan_ip,c.lan_ip,sizeof(j.lan_ip));
            const Code queued=QueueOpenLink(j); browser::JoinResult(uint32_t(queued));
            if(queued==Pending) { ArmPostMatch(); vote::Accept(); overlay::Hold(false); } // a new server's votes count again
        }
    }
    AcquireSRWLockExclusive(&mailbox);
    if(running.load()) {
        if(front) ++report.front_ticks; else { ++report.pre_ticks; report.gates|=PreGame; }
        Sample(self,front);
        const Frame frame={(report.gates&Client)!=0,(report.gates&NoEac)!=0,front,report.mode,uint32_t(report.kind),report.pre_state,report.pre_target};
        TickContext context={self,front};
        machine.Tick(frame,GetTickCount64(),{&context,Switch,Arm,Start,Kind,BeginLanLobby,CanInitLobby,Select});
        report.phase=machine.phase; report.code=machine.code;
        if(machine.fallback_used) report.gates|=Fallback;
        if(persistent.load() && (report.gates&NoEac)) { TickPersistentUi(); cbrowser::Tick(); }
    }
    ReleaseSRWLockExclusive(&mailbox);
    original(self);
    AcquireSRWLockExclusive(&mailbox);
    if(running.load()) Sample(self,front);
    ReleaseSRWLockExclusive(&mailbox);
    in_hook=false;
}
void FrontHook(void* self) { EngineTick(self,true); }
void PreHook(void* self) { EngineTick(self,false); }
void* Exchange(void* volatile* slot,void* value,void* expected) { return InterlockedCompareExchangePointer(slot,value,expected); }
const hostctl::SlotFunctions functions={VirtualProtect,Exchange};
DWORD Swap(uintptr_t slot,uintptr_t target,void* hook,bool install,hostctl::SlotProtection& protection) noexcept {
    void* original=reinterpret_cast<void*>(image+target);
    return hostctl::ReplaceTickSlot(reinterpret_cast<void* volatile*>(image+slot),install ? original : hook,install ? hook : original,protection,functions);
}
DWORD Restore() noexcept {
    DWORD first=ERROR_SUCCESS;
    if(pre_installed) { DWORD e=Swap(kPreSlot,kPre,reinterpret_cast<void*>(&PreHook),false,pre_protection); if(!e) pre_installed=false; else first=e; }
    if(front_installed) { DWORD e=Swap(kFrontSlot,kFront,reinterpret_cast<void*>(&FrontHook),false,front_protection); if(!e) front_installed=false; else if(!first) first=e; }
    if(image) {
        DWORD e=hostctl::RestoreTickProtection(reinterpret_cast<void* volatile*>(image+kPreSlot),pre_protection,functions); if(e && !first) first=e;
        e=hostctl::RestoreTickProtection(reinterpret_cast<void* volatile*>(image+kFrontSlot),front_protection,functions); if(e && !first) first=e;
    }
    return first;
}
}
DWORD Install() noexcept {
    AcquireSRWLockExclusive(&installation);
    DWORD e=started_once ? ERROR_ALREADY_EXISTS : CheckNoEac(GetCurrentProcessId());
    uintptr_t base=reinterpret_cast<uintptr_t>(GetModuleHandleW(nullptr));
    if(!e && !Validate(base)) e=ERROR_BAD_EXE_FORMAT;
    if(!e) {
        started_once=true; image=base;
        AcquireSRWLockExclusive(&mailbox); report.gates=Build|Client|NoEac; ReleaseSRWLockExclusive(&mailbox);
        e=Swap(kFrontSlot,kFront,reinterpret_cast<void*>(&FrontHook),true,front_protection); front_installed=e==0;
        if(!e) { e=Swap(kPreSlot,kPre,reinterpret_cast<void*>(&PreHook),true,pre_protection); pre_installed=e==0; }
        if(e) { const DWORD cleanup=Restore(); if(cleanup) e=cleanup; }
        else {
            AcquireSRWLockExclusive(&mailbox); report.gates|=Hook; ReleaseSRWLockExclusive(&mailbox);
            running.store(true,std::memory_order_release);
        }
    }
    AcquireSRWLockExclusive(&mailbox); report.error=e; if(e) report.code=Unsupported; ReleaseSRWLockExclusive(&mailbox);
    ReleaseSRWLockExclusive(&installation); return e;
}
DWORD StartPersistent(const char* directory,const char* lan_ip) noexcept {
    const DWORD e=Install();
    if(e) return e;
    // The UI text table needs a running engine tick (the tick hooks just went in). The launcher
    // waits 30 s for this call, so give the game up to 25 s to tick once.
    for(int i=0;i<250;++i) {
        AcquireSRWLockExclusive(&mailbox); const bool ticking=(report.gates&Ticking)!=0; ReleaseSRWLockExclusive(&mailbox);
        if(ticking) break;
        Sleep(100);
    }
    const Code configured=ConfigureBrowser(directory,lan_ip);
    if(configured!=Ok) { browser::Log("browser config refused"); return ERROR_INVALID_PARAMETER; }
    static const char kTexts[]=
        "openlink/item/title\tOpenLink Servers\n"
        "openlink/item/desc\tBrowse and join community dedicated servers.\n"
        "openlink/ui/refresh\tRefresh server list\n"
        "openlink/ui/refresh_desc\tFetch the latest servers from the OpenLink directory.\n"
        "openlink/ui/empty\tNo OpenLink servers found\n"
        "openlink/ui/loading\tLoading servers...\n"
        "openlink/ui/empty_desc\tSelect Refresh to check the directory again.\n";
    if(SetTexts(kTexts,sizeof(kTexts)-1)!=Ok) { browser::Log("UI text setup failed"); return ERROR_INVALID_DATA; }
    // Customs Browser labels while it is in OpenLink mode (IDs from a string trace, O165).
    text::SetOverlay(0x773922d8u,"OpenLink Servers");          // grid header "Customs Browser"
    text::SetOverlay(0x9be64b79u,"OpenLink Servers");          // details header "Search Results"
    text::SetOverlay(0xdc6e123fu,"No OpenLink servers found"); // "No Custom Games Found"
    // The details page's custom-game XP notes do not apply to OpenLink servers (O167 trace).
    text::SetOverlay(0x99d276b4u,"");                           // "{n}/{m} Daily Custom XP Cap"
    text::SetOverlay(0xf511f511u,"");                           // same, alternate form
    text::SetOverlay(0x5a42898au,"");                           // "Match is eligible for XP"
    text::SetOverlay(0x4ae5c2ecu,"");                           // same, alternate form
    // The filters panel is hidden in OpenLink mode; its "Filters" title is a separate widget.
    text::SetOverlay(0x85d07d99u,"");                           // "Filters"
    HANDLE watcher=CreateThread(nullptr,0,WatchBoundary,nullptr,0,nullptr);
    if(!watcher) { browser::Log("boundary watcher failed to start"); return GetLastError(); }
    CloseHandle(watcher);
    // Customs Browser OpenLink mode; without it the simple list page is the fallback.
    overlay::SetLog(&browser::Log);
    browser::SetLeaveHook(&CancelPostMatch); // leaving from the lobby is not a post-match return
    // The lobby's map header shows the map the server will really play (optional).
    if(const DWORD header=mapheader::Install(image,&browser::Log)) { char note[96]; sprintf_s(note,"lobby map header hook unavailable (%lu)",header); browser::Log(note); }
    vote::Install(); // playlist votes from the relay; the overlay starts with the first ballot
    if(const DWORD hooks=cbrowser::Install(image)) { char note[96]; sprintf_s(note,"customs browser hooks unavailable (%lu); using the list page",hooks); browser::Log(note); }
    persistent.store(true); browser::SetActive(true);
    char line[512]; sprintf_s(line,"OpenLink Launcher %s started (directory %s, %s)",OPENLINK_VERSION,directory,lan_ip[0] ? lan_ip : "loopback relay"); browser::Log(line);
    return ERROR_SUCCESS;
}
namespace {
// Caller holds the mailbox lock.
bool IdleForJoin() noexcept {
    return running.load() && !report.armed && !report.bypass &&
        ((report.mode==0 && report.front_ticks) || (report.mode==5 && report.pre_ticks && report.pre_state==0 && report.pre_target==-1)) &&
        (report.gates&(Build|Client|Hook|Ticking))==(Build|Client|Hook|Ticking) && Fingerprints(image);
}
std::atomic<bool> relay_join_queued{false};
ULONGLONG relay_join_at=0; uint32_t report_joins=0;
// Caller holds the mailbox lock. Clears everything a finished join left behind.
void ResetJoin() noexcept {
    machine.Reset(); selection_sent=false;
    report.gates&=~uint32_t(WasArmed|GatePassed|LobbyInitRequested|BrowseMatch|SelectionDispatched|TargetSelected|TargetPreset|Fallback);
    report.phase=Idle; report.code=NotReady; report.error=0;
    relay_join_queued.store(false);
    char line[64]; sprintf_s(line,"previous join reset (joins so far %u)",report_joins); browser::Log(line);
}
void RelayReady(const char* local_ip) {
    char note[128]; sprintf_s(note,"relay announces the server on %s%s",local_ip,relay::Snapshot().lan_auto ? " (LAN broadcast: a server runs on this PC)" : ""); browser::Log(note);
    Request r={}; strcpy_s(r.host,local_ip); r.flags=16;
    const Code code=Queue(r);
    if(code!=Pending) {
        AcquireSRWLockExclusive(&mailbox); if(!report.error) report.error=ERROR_INVALID_STATE; report.code=code; ReleaseSRWLockExclusive(&mailbox);
        relay_join_queued.store(false); // a refused selection must not block the next join
        char line[64]; sprintf_s(line,"stock selection not queued (code %u)",unsigned(code)); browser::Log(line);
    }
}
}
Code Queue(const Request& request) noexcept {
    // Only the stock LAN host setup + Server-picker selection (flag 16) is ever used. The join
    // machine's other request kinds (direct auto-connect variants) are refused here.
    if(request.flags!=16) return Invalid;
    const DWORD e=CheckNoEac(GetCurrentProcessId());
    AcquireSRWLockExclusive(&mailbox);
    Code code=NotReady;
    if(e) { report.error=e; report.gates&=~uint32_t(NoEac); code=Failure; }
    else if(IdleForJoin()) {
        report.gates|=NoEac; code=machine.Submit(request,GetTickCount64()); report.code=machine.code; report.phase=machine.phase;
    }
    ReleaseSRWLockExclusive(&mailbox); return code;
}
Code ConfigureBrowser(const char* directory,const char* lan_ip) noexcept {
    browser::Config c={};
    if(!directory || !lan_ip || strcpy_s(c.directory,directory) || strcpy_s(c.lan_ip,lan_ip)) return Invalid;
    const DWORD e=CheckNoEac(GetCurrentProcessId());
    if(e) return Failure;
    const uint32_t result=browser::Configure(c);
    return result==ERROR_SUCCESS || result==ERROR_ALREADY_EXISTS ? Ok : result==ERROR_INVALID_PARAMETER ? Invalid : Failure;
}
Code SetTexts(const char* lines,size_t length) noexcept {
    if(!lines || !length || length>16000) return Invalid;
    const DWORD e=CheckNoEac(GetCurrentProcessId());
    AcquireSRWLockExclusive(&mailbox);
    Code code=NotReady;
    if(e) { report.error=e; report.gates&=~uint32_t(NoEac); code=Failure; }
    else if(running.load() && (report.gates&(Build|Client|Hook|Ticking))==(Build|Client|Hook|Ticking) && Fingerprints(image)) {
        code=Ok; uint32_t added=0;
        for(size_t at=0;at<length && code==Ok;) {
            size_t end=at; while(end<length && lines[end]!='\n') ++end;
            size_t tab=at; while(tab<end && lines[tab]!='\t') ++tab;
            if(end>at && tab<end) {
                char key[256]={},value[1024]={};
                size_t vend=end; if(vend>tab+1 && lines[vend-1]=='\r') --vend;
                if(tab-at>=sizeof(key) || vend-tab-1>=sizeof(value)) code=Invalid;
                else { std::memcpy(key,lines+at,tab-at); std::memcpy(value,lines+tab+1,vend-tab-1); if(text::Set(key,value)) ++added; else code=Invalid; }
            }
            at=end+1;
        }
        report.text_entries+=added;
        if(code==Ok) { const uint32_t hook=text::Install(image); if(hook) { report.text_error=hook; code=Failure; } }
    }
    ReleaseSRWLockExclusive(&mailbox); return code;
}
Code QueueOpenLink(const RelayJoin& join) noexcept {
    relay::Config config={};
    std::memcpy(config.directory,join.directory,sizeof(config.directory)); std::memcpy(config.listing,join.listing,sizeof(config.listing));
    std::memcpy(config.lan_ip,join.lan_ip,sizeof(config.lan_ip));
    if(!relay::ValidConfig(config)) return Invalid;
    const DWORD e=CheckNoEac(GetCurrentProcessId());
    AcquireSRWLockExclusive(&mailbox);
    Code code=NotReady;
    if(e) { report.error=e; report.gates&=~uint32_t(NoEac); code=Failure; }
    else {
        // One join at a time, but any number per game process. A join is in flight
        // while the machine is selecting, or while its relay is still starting (bounded
        // by 45 s, after which a relay that never advertised is treated as abandoned).
        const bool machine_busy=machine.phase==WaitingLan || machine.phase==Armed || machine.phase==Selecting;
        const relay::State relay_state=relay::Snapshot().state;
        const bool relay_starting=relay_join_queued.load() && machine.phase==Idle && relay_state!=relay::Failed && relay_state!=relay::Gone &&
            GetTickCount64()-relay_join_at<45000;
        if(machine_busy || relay_starting) code=Busy;
        else if(IdleForJoin()) {
            // Back at the idle front end: the previous join (if any) is over. Reset it; the
            // relay restart below retires the previous relay session off the engine thread.
            if(machine.phase!=Idle || relay_join_queued.load()) ResetJoin();
            // The selection itself is queued later, from the relay thread, through Queue().
            const DWORD started=relay::Start(config,RelayReady);
            if(started) { report.error=started; code=Failure; }
            else { relay_join_queued.store(true); relay_join_at=GetTickCount64(); ++report_joins; code=Pending; }
        }
    }
    ReleaseSRWLockExclusive(&mailbox); return code;
}
}
