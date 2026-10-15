// In-game browser support (see browser.h). HKS C API addresses/layout: O158/FN058.
#define WIN32_LEAN_AND_MEAN
#include <windows.h>
#include "browser.h"
#include "relay.h"
#include "text_hook.h"
#include "cbrowser.h"
#include "overlay.h"
#include "vote.h"
#include <atomic>
#include <cstdio>
#include <cstring>
namespace clientjoin {
namespace browser {
namespace {
struct Row { char id[40]; char title_key[64]; char desc_key[64]; char name[96]; char thumb[96]; char mode[96]; char match[96]; char phase[16]; char description[484]; int32_t players; bool joinable,needs_update; };
SRWLOCK lock=SRWLOCK_INIT;
Config config={};
std::atomic<bool> configured{false},active{true},join_pending{false};
std::atomic<uintptr_t> registered_state{0}; // lua_State the OpenLink table was registered in
Row rows[kMaxRows]; size_t row_count=0; bool fetched=false;
std::atomic<uint32_t> fetches{0},joins{0},last_join_code{0},fetch_error{0};
uint32_t revision=0; // bumped (under lock) whenever the row snapshot changes
char pending[40]={},joined[40]={}; // joined: the last listing a join was queued for
HANDLE wake=nullptr;
uintptr_t image=0;
SRWLOCK log_lock=SRWLOCK_INIT;
wchar_t log_path[MAX_PATH]={};

// ---- HKS C API (B002, O158) ----
using CFunction=int (*)(uintptr_t);
using PushLString=void (*)(uintptr_t,const char*,size_t);
using ToLString=const char* (*)(uintptr_t,const uint8_t*,size_t*);
using CreateTable=void (*)(uintptr_t,int,int);
using SetField=void (*)(uintptr_t,int,const char*);
using PushCClosure=void (*)(uintptr_t,CFunction,int,const char*,int);
template<class T> T Api(uintptr_t rva) { return reinterpret_cast<T>(image+rva); }
constexpr int kGlobals=-10002;
uint8_t*& Top(uintptr_t L) { return *reinterpret_cast<uint8_t**>(L+0x48); }
uint8_t* Base(uintptr_t L) { return *reinterpret_cast<uint8_t**>(L+0x50); }
uint8_t* Limit(uintptr_t L) { return *reinterpret_cast<uint8_t**>(L+0x58); }
int Args(uintptr_t L) { return int((Top(L)-Base(L))>>4); }
const uint8_t* Arg(uintptr_t L,int i) { return i>=1 && i<=Args(L) ? Base(L)+size_t(i-1)*16 : nullptr; }
uint32_t Type(const uint8_t* tv) { return tv ? *reinterpret_cast<const uint32_t*>(tv) : 0; }
bool Room(uintptr_t L,int n) { return Top(L)+size_t(n)*16<=Limit(L); }
void PushRaw(uintptr_t L,uint32_t type,uint64_t value) {
    uint8_t* t=Top(L); std::memset(t,0,16); std::memcpy(t,&type,4); std::memcpy(t+8,&value,8); Top(L)=t+16;
}
void PushNil(uintptr_t L) { PushRaw(L,0,0); }
void PushBool(uintptr_t L,bool v) { PushRaw(L,1,v ? 1 : 0); }
void PushNumber(uintptr_t L,float v) { uint32_t bits; std::memcpy(&bits,&v,4); PushRaw(L,3,bits); }
void PushString(uintptr_t L,const char* s) { Api<PushLString>(0x6f9f64)(L,s,std::strlen(s)); }
bool ArgString(uintptr_t L,int i,char* out,size_t capacity) {
    const uint8_t* tv=Arg(L,i); if(Type(tv)!=4) return false;
    size_t length=0; const char* s=Api<ToLString>(0x7b9b44)(L,tv,&length);
    if(!s || length>=capacity) return false; std::memcpy(out,s,length); out[length]=0; return true;
}
bool ArgNumber(uintptr_t L,int i,float& v) { const uint8_t* tv=Arg(L,i); if(Type(tv)!=3) return false; std::memcpy(&v,tv+8,4); return true; }
int Fail(uintptr_t L,const char* message) { if(!Room(L,2)) return 0; PushNil(L); PushString(L,message); return 2; }

// ---- natives ----
int NativeLog(uintptr_t L) { char text[512]; if(ArgString(L,1,text,sizeof(text))) Log(text); return 0; }
int NativeCount(uintptr_t L) {
    if(!Room(L,1)) return 0;
    AcquireSRWLockShared(&lock); const float n=fetched ? float(row_count) : -1.0f; ReleaseSRWLockShared(&lock);
    PushNumber(L,n); return 1;
}
int NativeRow(uintptr_t L) {
    float index=0; if(!ArgNumber(L,1,index)) return Fail(L,"row index required");
    if(!Room(L,4)) return 0;
    Row r={}; bool ok=false;
    AcquireSRWLockShared(&lock);
    const int i=index>=1.0f && index<=float(kMaxRows) ? int(index) : 0;
    if(i>=1 && size_t(i)<=row_count) { r=rows[i-1]; ok=true; }
    ReleaseSRWLockShared(&lock);
    if(!ok) return Fail(L,"no such row");
    PushString(L,r.id); PushString(L,r.title_key); PushString(L,r.desc_key); PushBool(L,r.joinable); return 4;
}
int NativeJoin(uintptr_t L) {
    char id[40]; if(!ArgString(L,1,id,sizeof(id))) return Fail(L,"listing id required");
    const char* refusal=RequestJoin(id);
    if(refusal) return Fail(L,refusal);
    if(!Room(L,1)) return 0; PushBool(L,true); return 1;
}
// OpenLink.SetBrowserMode(bool) -> bool (false when the browser hooks are unavailable).
int NativeSetBrowserMode(uintptr_t L) {
    const uint8_t* tv=Arg(L,1); const bool on=Type(tv)==1 ? *reinterpret_cast<const int32_t*>(tv+8)!=0 : Type(tv)!=0;
    const bool ok=cbrowser::SetMode(on);
    if(!Room(L,1)) return 0; PushBool(L,ok); return 1;
}
// OpenLink.BrowserMode() -> bool (the Customs Browser is in OpenLink mode).
int NativeBrowserMode(uintptr_t L) { if(!Room(L,1)) return 0; PushBool(L,cbrowser::Mode()); return 1; }
// OpenLink.SetLobbyVote(bool): the server lobby screen is showing, so the vote panel takes the
// place of its greyed-out host rows (overlay.cpp).
// Optional numbers after the flag: the rows' rectangle x0,y0,x1,y1 and the start button's bottom,
// in UI units (from the widgets' own geometry), so the panel can follow the real layout.
int NativeSetLobbyVote(uintptr_t L) {
    const uint8_t* tv=Arg(L,1); const bool on=Type(tv)==1 && *reinterpret_cast<const int32_t*>(tv+8)!=0;
    float r[5]={};
    for(int i=0;i<5;++i) if(!ArgNumber(L,i+2,r[i])) r[i]=0;
    overlay::SetLobbyBlock(on,r[0],r[1],r[2],r[3],r[4]);
    return 0;
}
// OpenLink.OnServer() -> bool (connected to an OpenLink server through our relay).
int NativeOnServer(uintptr_t L) { if(!Room(L,1)) return 0; PushBool(L,relay::Live()); return 1; }
// OpenLink.RawId(template, hex) -> a game ID value with the given hash (e.g. a dialog ID). Script
// numbers are 32-bit floats; `template` is any stringid(...) result, whose value type (ui64, O195)
// is reused. nil if the template is a number, string or nil.
int NativeRawId(uintptr_t L) {
    const uint8_t* tv=Arg(L,1); const uint32_t type=Type(tv);
    char hex[20]; if(!Room(L,1)) return 0;
    if(!tv || type==0 || type==3 || type==4 || !ArgString(L,2,hex,sizeof(hex))) { PushNil(L); return 1; }
    uint64_t value=0; size_t i=0;
    for(;hex[i];++i) {
        const char c=hex[i]; const int d=c>='0'&&c<='9' ? c-'0' : c>='a'&&c<='f' ? c-'a'+10 : c>='A'&&c<='F' ? c-'A'+10 : -1;
        if(d<0 || i>=16) { PushNil(L); return 1; }
        value=value<<4|uint64_t(d);
    }
    if(!i) { PushNil(L); return 1; }
    PushRaw(L,type,value); return 1;
}
// OpenLink.LeaveNotice([on]): true (default) before the script opens the "Leave server?" dialog: the
// generic dialog reads our text (cbrowser) and the vote panel hides at once; false when cancelled.
int NativeLeaveNotice(uintptr_t L) {
    const uint8_t* tv=Arg(L,1); const bool on=!tv || Type(tv)!=1 || *reinterpret_cast<const int32_t*>(tv+8)!=0;
    if(on) cbrowser::LeaveNotice();
    overlay::Hold(on);
    return 0;
}
// OpenLink.LeaveServer(): the player left the server's lobby (the game's own LAN disconnect ran):
// ends our relay session shortly after (so the game's leave reaches the server) and cancels the
// post-match redirect.
void (*leave_hook)()=nullptr;
int NativeLeaveServer(uintptr_t) {
    vote::Drop(); overlay::Hold(false); // no panel or lobby map from this server any more
    relay::StopAfter(1500);
    if(leave_hook) leave_hook();
    Log("left the OpenLink server (relay closes in 1.5 s)");
    return 0;
}
int NativeRefresh(uintptr_t L) { if(wake) SetEvent(wake); if(!Room(L,1)) return 0; PushBool(L,configured.load()); return 1; }
int NativeStatus(uintptr_t L) {
    if(!Room(L,4)) return 0;
    PushNumber(L,float(fetches.load())); PushNumber(L,float(joins.load())); PushNumber(L,float(last_join_code.load())); PushNumber(L,float(fetch_error.load()));
    return 4;
}

// ---- directory fetch ----
void Describe(const relay::Row& r,char* out,size_t capacity) {
    const int players=r.players<0 ? 0 : r.players;
    const char* where=r.match[0] ? r.match : (r.phase[0] ? r.phase : "OpenLink server");
    const bool phase_suffix=r.match[0] && r.phase[0]; // with no match name the phase is already the first part
    sprintf_s(out,capacity,"%s \xC2\xB7 %d player%s%s%s",where,players,players==1 ? "" : "s",phase_suffix ? " \xC2\xB7 " : "",phase_suffix ? r.phase : "");
}
DWORD WINAPI Fetch(void*) {
    // Text keys follow the listing id, so a row's text and its join target always agree.
    for(;;) {
        relay::Row list[kMaxRows]; size_t count=0;
        const uint32_t e=relay::ListServers(config.directory,list,kMaxRows,count);
        fetch_error.store(e);
        if(!e) {

            Row next[kMaxRows]={};
            for(size_t i=0;i<count;++i) {
                // Simulator listings (status "simulated") are shown but never joinable.
                Row& r=next[i]; strcpy_s(r.id,list[i].id); r.needs_update=list[i].app_protocol>relay::kAppProtocol;
                r.joinable=list[i].joinable && std::strcmp(list[i].status,"simulated")!=0 && !r.needs_update;
                strcpy_s(r.name,list[i].name[0] ? list[i].name : "OpenLink server"); strcpy_s(r.thumb,list[i].thumb); strcpy_s(r.mode,list[i].mode); strcpy_s(r.match,list[i].match); strcpy_s(r.phase,list[i].phase); strcpy_s(r.description,list[i].description); r.players=list[i].players<0 ? 0 : list[i].players;
                sprintf_s(r.title_key,"openlink/srv/%s/t",r.id); sprintf_s(r.desc_key,"openlink/srv/%s/d",r.id);
                char desc[256]; Describe(list[i],desc,sizeof(desc));
                text::Set(r.title_key,list[i].name[0] ? list[i].name : "OpenLink server"); text::Set(r.desc_key,desc);
            }
            AcquireSRWLockExclusive(&lock);
            const bool changed=!fetched || row_count!=count || std::memcmp(rows,next,sizeof(rows));
            std::memcpy(rows,next,sizeof(rows)); row_count=count; fetched=true; if(changed) ++revision;
            ReleaseSRWLockExclusive(&lock);
        }
        fetches.fetch_add(1);
        WaitForSingleObject(wake,5000);
    }
}
}

const char* RequestJoin(const char* id) noexcept {
    if(!id || !id[0] || std::strlen(id)>=40) return "listing id required";
    if(!active.load()) return "OpenLink is not active in this game session";
    bool known=false;
    bool update=false;
    AcquireSRWLockShared(&lock); for(size_t i=0;i<row_count;++i) if(!std::strcmp(rows[i].id,id)) { known=rows[i].joinable; update=rows[i].needs_update; break; } ReleaseSRWLockShared(&lock);
    if(update) return kNeedsUpdate;
    if(!known) return "server is not listed or not joinable";
    bool expected=false;
    if(!join_pending.compare_exchange_strong(expected,true)) return "a join is already in progress";
    AcquireSRWLockExclusive(&lock); strcpy_s(pending,id); strcpy_s(joined,id); ReleaseSRWLockExclusive(&lock);
    joins.fetch_add(1); char line[96]; sprintf_s(line,"join requested %s",id); Log(line);
    return nullptr;
}
size_t CopyRows(BrowserRow* out,size_t capacity,uint32_t& rev) noexcept {
    AcquireSRWLockShared(&lock);
    const size_t n=row_count<capacity ? row_count : capacity;
    for(size_t i=0;i<n;++i) {
        BrowserRow& b=out[i]; b={}; strcpy_s(b.id,rows[i].id); strcpy_s(b.name,rows[i].name); strcpy_s(b.thumb,rows[i].thumb); strcpy_s(b.mode,rows[i].mode); strcpy_s(b.match,rows[i].match); strcpy_s(b.phase,rows[i].phase); strcpy_s(b.description,rows[i].description);
        b.players=rows[i].players; b.joinable=rows[i].joinable;
    }
    rev=revision; ReleaseSRWLockShared(&lock); return n;
}
void Log(const char* text) noexcept {
    AcquireSRWLockExclusive(&log_lock);
    if(!log_path[0]) {
        HMODULE self=nullptr;
        if(GetModuleHandleExW(GET_MODULE_HANDLE_EX_FLAG_FROM_ADDRESS|GET_MODULE_HANDLE_EX_FLAG_UNCHANGED_REFCOUNT,reinterpret_cast<LPCWSTR>(&Log),&self)) {
            const DWORD n=GetModuleFileNameW(self,log_path,MAX_PATH);
            wchar_t* slash=n && n<MAX_PATH ? wcsrchr(log_path,L'\\') : nullptr;
            if(slash && size_t(slash-log_path)+24<MAX_PATH) wcscpy_s(slash+1,MAX_PATH-size_t(slash+1-log_path),L"openlink-launcher.log"); else log_path[0]=0;
        }
    }
    if(log_path[0]) {
        HANDLE f=CreateFileW(log_path,FILE_APPEND_DATA,FILE_SHARE_READ,nullptr,OPEN_ALWAYS,0,nullptr);
        if(f!=INVALID_HANDLE_VALUE) {
            SYSTEMTIME t; GetLocalTime(&t); char line[640];
            const int n=sprintf_s(line,"%02u:%02u:%02u.%03u %s\r\n",t.wHour,t.wMinute,t.wSecond,t.wMilliseconds,text);
            DWORD written=0; if(n>0) WriteFile(f,line,DWORD(n),&written,nullptr); CloseHandle(f);
        }
    }
    ReleaseSRWLockExclusive(&log_lock);
}
uint32_t Configure(const Config& c) noexcept {
    relay::Config check={}; std::memcpy(check.directory,c.directory,sizeof(check.directory)); strcpy_s(check.listing,"probe"); std::memcpy(check.lan_ip,c.lan_ip,sizeof(check.lan_ip));
    if(c.directory[sizeof(c.directory)-1] || c.lan_ip[sizeof(c.lan_ip)-1] || !relay::ValidConfig(check)) return ERROR_INVALID_PARAMETER;
    bool expected=false;
    if(!configured.compare_exchange_strong(expected,true)) return ERROR_ALREADY_EXISTS;
    config=c; wake=CreateEventW(nullptr,FALSE,FALSE,nullptr);
    HANDLE thread=wake ? CreateThread(nullptr,0,Fetch,nullptr,0,nullptr) : nullptr;
    if(!thread) { configured.store(false); return GetLastError(); }
    CloseHandle(thread); Log("browser configured"); return ERROR_SUCCESS;
}
bool RegisterNatives(uintptr_t module_image,uintptr_t L) noexcept {
    if(registered_state.load()==L) return true;
    image=module_image;
    if(!Room(L,4)) return false;
    struct { const char* name; CFunction fn; } natives[]={{"Log",NativeLog},{"Count",NativeCount},{"Row",NativeRow},{"Join",NativeJoin},{"Refresh",NativeRefresh},{"Status",NativeStatus},{"SetBrowserMode",NativeSetBrowserMode},{"BrowserMode",NativeBrowserMode},
        {"SetLobbyVote",NativeSetLobbyVote},{"OnServer",NativeOnServer},{"LeaveNotice",NativeLeaveNotice},{"RawId",NativeRawId},{"LeaveServer",NativeLeaveServer}};
    Api<CreateTable>(0x7c7e7c)(L,0,int(sizeof(natives)/sizeof(natives[0])));
    for(const auto& n:natives) { Api<PushCClosure>(0xa3f47c)(L,n.fn,0,n.name,0); Api<SetField>(0x6f980c)(L,-2,n.name); }
    Api<SetField>(0x6f980c)(L,kGlobals,"OpenLink");
    registered_state.store(L); Log("natives registered"); return true;
}
bool Registered() noexcept { return registered_state.load()!=0; }
bool RegisteredIn(uintptr_t L) noexcept { return registered_state.load()==L; }
bool TakeJoin(char (&listing)[40]) noexcept {
    if(!join_pending.load()) return false;
    AcquireSRWLockShared(&lock); bool have=pending[0]!=0; if(have) strcpy_s(listing,pending); ReleaseSRWLockShared(&lock);
    if(have) { AcquireSRWLockExclusive(&lock); pending[0]=0; ReleaseSRWLockExclusive(&lock); }
    return have;
}
void JoinResult(uint32_t code) noexcept {
    last_join_code.store(code); char line[64]; sprintf_s(line,"join queued code=%u",code); Log(line);
    join_pending.store(false); // the backend decides whether another join may start
}
void SetActive(bool value) noexcept { active.store(value); }
const Config& Current() noexcept { return config; }
uint32_t Fetches() noexcept { return fetches.load(); }
uint32_t FetchError() noexcept { return fetch_error.load(); }
bool JoinedThumb(char (&thumb)[96]) noexcept {
    thumb[0]=0;
    AcquireSRWLockShared(&lock);
    if(joined[0]) for(size_t i=0;i<row_count;++i) if(!std::strcmp(rows[i].id,joined)) { strcpy_s(thumb,rows[i].thumb); break; }
    ReleaseSRWLockShared(&lock);
    return thumb[0]!=0;
}
void SetLeaveHook(void (*hook)()) noexcept { leave_hook=hook; }
bool Configured() noexcept { return configured.load(); }
bool Fetched() noexcept { AcquireSRWLockShared(&lock); const bool f=fetched; ReleaseSRWLockShared(&lock); return f; }
uint32_t RowCount() noexcept { AcquireSRWLockShared(&lock); const uint32_t n=uint32_t(row_count); ReleaseSRWLockShared(&lock); return n; }
uint32_t Joins() noexcept { return joins.load(); }
}
}
