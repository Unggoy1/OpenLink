// Customs Browser OpenLink mode (see cbrowser.h). Addresses and layouts: FN060/O163.
#define WIN32_LEAN_AND_MEAN
#include <windows.h>
#include "cbrowser.h"
#include "browser.h"
#include "detour.h"
#include "text_hook.h"
#include <atomic>
#include <cstdio>
#include <cstring>
#include <cwchar>
namespace clientjoin {
namespace cbrowser {
namespace {
// Lobby record (0x140): wstring name +0x00, wstring handle +0x20, GUID map +0x50 / version
// +0x60, members current +0x118 / max +0x11c. Built with the game's ctor/assign/dtor so its
// allocator owns every string.
constexpr size_t kRecord=0x140,kMaxRecords=browser::kMaxRows;
constexpr uintptr_t kCtor=0x1106c90,kDtor=0xe3b1b4,kAssign=0x4e9a44;
constexpr uintptr_t kCopy=0x20af850,kQuery=0x1054ea8,kJoinHandle=0xd0f738,kJoinFireteam=0x2e92e04;
constexpr char kSentinel[]="openlink:";
struct Vector { uint8_t* first; uint8_t* last; uint8_t* end; };
using Ctor=void* (*)(void*);
using Dtor=void (*)(void*);
using Assign=void* (*)(void*,const wchar_t*,size_t);
using Copy=uintptr_t (*)(uintptr_t,Vector*);
using Query=void (*)();
using JoinHandle=void (*)(const uint8_t*);
using JoinFireteam=void (*)(uint32_t,uint64_t,const uint8_t*);

uintptr_t image=0;
std::atomic<bool> installed{false},mode{false},clear_pending{false},force_push{false},official_since_off{false},disabled{false};
Copy original_copy=nullptr; Query original_query=nullptr; JoinHandle original_join_handle=nullptr; JoinFireteam original_join_fireteam=nullptr;
SRWLOCK records_lock=SRWLOCK_INIT;
uint8_t* records=nullptr; size_t record_count=0; uint32_t pushed_revision=0xffffffffu; // pushed_revision: engine thread only
std::atomic<uint32_t> blocked_queries{0},swapped_results{0},caught_joins{0};

template<class T> T Game(uintptr_t rva) { return reinterpret_cast<T>(image+rva); }
int Hex(char c) { return c>='0'&&c<='9' ? c-'0' : c>='a'&&c<='f' ? c-'a'+10 : c>='A'&&c<='F' ? c-'A'+10 : -1; }
// "8-4-4-4-12" text -> Windows GUID layout (Data1/2/3 little-endian, Data4 bytes in order).
bool ParseGuid(const char* s,size_t n,uint8_t (&out)[16]) {
    if(n!=36 || s[8]!='-' || s[13]!='-' || s[18]!='-' || s[23]!='-') return false;
    uint8_t raw[16]; size_t k=0;
    for(size_t i=0;i<36 && k<16;) { if(s[i]=='-') { ++i; continue; } const int a=Hex(s[i]),b=Hex(s[i+1]); if(a<0||b<0) return false; raw[k++]=uint8_t(a*16+b); i+=2; }
    if(k!=16) return false;
    out[0]=raw[3]; out[1]=raw[2]; out[2]=raw[1]; out[3]=raw[0]; out[4]=raw[5]; out[5]=raw[4]; out[6]=raw[7]; out[7]=raw[6];
    std::memcpy(out+8,raw+8,8); return true;
}
void AssignText(uint8_t* wstring,const char* utf8) {
    wchar_t wide[128]={}; int n=MultiByteToWideChar(CP_UTF8,0,utf8,-1,wide,128); n=n>0 ? n-1 : 0;
    Game<Assign>(kAssign)(wstring,wide,size_t(n));
}
// Builds a new record set from the browser snapshot (engine thread).
// "<asset GUID>/<version GUID>" (listing thumb/mode) into two record GUID fields.
bool ParsePair(const char* text,uint8_t* asset,uint8_t* version) {
    const char* slash=std::strchr(text,'/'); uint8_t a[16],v[16];
    if(!slash || !ParseGuid(text,size_t(slash-text),a) || !ParseGuid(slash+1,std::strlen(slash+1),v)) return false;
    std::memcpy(asset,a,16); std::memcpy(version,v,16); return true;
}
// Server names for OpenLink tiles, keyed by listing id (records_lock).
struct TileName { char id[40]; wchar_t name[96]; size_t length; wchar_t mode[96]; size_t mode_length; wchar_t description[484]; size_t description_length; };
TileName names[kMaxRecords]; size_t name_count=0;
// Builds a new record set from the browser snapshot (engine thread). Listings without
// map art are left out: the tile could not show anything useful (and simulator
// listings carry none).
uint8_t* Build(size_t& count,TileName (&tile_names)[kMaxRecords],size_t& tile_count) {
    browser::BrowserRow rows[kMaxRecords]; uint32_t rev=0;
    const size_t total=browser::CopyRows(rows,kMaxRecords,rev);
    count=0; tile_count=0;
    if(!total) return nullptr;
    auto* set=static_cast<uint8_t*>(HeapAlloc(GetProcessHeap(),HEAP_ZERO_MEMORY,total*kRecord));
    if(!set) return nullptr;
    for(size_t i=0;i<total;++i) {
        // A server between matches (for example while its players vote) has no map yet: it is
        // listed without map art. Listings with neither art nor open slots (a server's
        // reachability check) stay out.
        uint8_t map_asset[16],map_version[16];
        const bool art=ParsePair(rows[i].thumb,map_asset,map_version);
        if(!art && !rows[i].joinable) continue;
        uint8_t* r=set+count*kRecord; Game<Ctor>(kCtor)(r); ++count;
        AssignText(r+0x00,rows[i].name);
        char handle[64]; sprintf_s(handle,"%s%s",kSentinel,rows[i].id); AssignText(r+0x20,handle);
        if(art) { std::memcpy(r+0x50,map_asset,16); std::memcpy(r+0x60,map_version,16); }
        // The playlist's published game variant is the UGC variant (+0x90/+0xa0).
        ParsePair(rows[i].mode,r+0x90,r+0xa0);
        // Only a full listing (16+ players) shows full; other non-joinable listings keep
        // open slots so Join reaches our hook and the player is told why (refusal dialog).
        const int32_t players=rows[i].players,max=players<16 ? 16 : (rows[i].joinable ? players+1 : players);
        std::memcpy(r+0x118,&players,4); std::memcpy(r+0x11c,&max,4);
        TileName& t=tile_names[tile_count++]; t={}; strcpy_s(t.id,rows[i].id);
        const int n=MultiByteToWideChar(CP_UTF8,0,rows[i].name,-1,t.name,96); t.length=n>0 ? size_t(n-1) : 0;
        const char* phase=rows[i].phase;
        const char* label=rows[i].match[0] ? rows[i].match :
            !std::strcmp(phase,"voting") ? "Voting for the next match" : !std::strcmp(phase,"starting") ? "Starting a match" :
            !std::strcmp(phase,"in_game") ? "In a match" : !std::strcmp(phase,"post_game") ? "Match ending" : "In the lobby";
        const int m=MultiByteToWideChar(CP_UTF8,0,label,-1,t.mode,96); t.mode_length=m>0 ? size_t(m-1) : 0;
        const int d=MultiByteToWideChar(CP_UTF8,0,rows[i].description,-1,t.description,484); t.description_length=d>0 ? size_t(d-1) : 0;
    }
    if(!count) { HeapFree(GetProcessHeap(),0,set); return nullptr; }
    return set;
}
// SEH-guarded copy for reading game UI objects.
bool SafeCopy(void* to,const void* from,size_t n) { __try { std::memcpy(to,from,n); return true; } __except(EXCEPTION_EXECUTE_HANDLER) { return false; } }
// Reads an MSVC std::string (narrow) into out; false if unreadable or too long.
bool ReadString(const uint8_t* s,char* out,size_t capacity) {
    uint8_t raw[32]; if(!SafeCopy(raw,s,32)) return false;
    size_t size=0,cap=0; std::memcpy(&size,raw+0x10,8); std::memcpy(&cap,raw+0x18,8);
    if(size>=capacity || cap<size) return false;
    const char* text=cap>=16 ? *reinterpret_cast<const char* const*>(raw) : reinterpret_cast<const char*>(s);
    if(!SafeCopy(out,text,size)) return false; out[size]=0; return true;
}
bool WStringEquals(const uint8_t* s,const wchar_t* text,size_t length) {
    uint8_t raw[32]; if(!SafeCopy(raw,s,32)) return false;
    size_t size=0,cap=0; std::memcpy(&size,raw+0x10,8); std::memcpy(&cap,raw+0x18,8);
    if(size!=length) return false;
    const wchar_t* data=cap>=8 ? *reinterpret_cast<const wchar_t* const*>(raw) : reinterpret_cast<const wchar_t*>(s);
    wchar_t buffer[96]; if(length>96 || !SafeCopy(buffer,data,length*sizeof(wchar_t))) return false;
    return !std::memcmp(buffer,text,length*sizeof(wchar_t));
}
void Destroy(uint8_t* set,size_t count) { for(size_t i=0;i<count;++i) Game<Dtor>(kDtor)(set+i*kRecord); if(set) HeapFree(GetProcessHeap(),0,set); }
bool StartsWithSentinel(const char* s,size_t n,char (&id)[40]) {
    const size_t p=sizeof(kSentinel)-1;
    if(n<=p || n-p>=sizeof(id) || std::memcmp(s,kSentinel,p)) return false;
    std::memcpy(id,s+p,n-p); id[n-p]=0; return true;
}
void Notice(const char* refusal); // defined below
void Join(const char* id) {
    caught_joins.fetch_add(1);
    const char* refusal=browser::RequestJoin(id);
    char line[160]; sprintf_s(line,"browser join %s -> %s",id,refusal ? refusal : "queued"); browser::Log(line);
    if(!refusal) SetMode(false); // the join navigates away; never leave the mode armed behind it
    else Notice(refusal);
}

// ---- hooks ----
void QueryHook() {
    if(mode.load()) {
        // The browser asked for results (screen init or Refresh): answer with ours again.
        blocked_queries.fetch_add(1); force_push.store(true); return;
    }
    original_query();
}
uintptr_t CopyHook(uintptr_t unused,Vector* official) {
    // Lock order everywhere: records_lock, then the game's 0x1451a2500 inside the copy.
    AcquireSRWLockShared(&records_lock);
    uintptr_t result;
    if(mode.load()) {
        // An official result landing while OpenLink mode is on is replaced by our records.
        swapped_results.fetch_add(1);
        Vector ours={records,records+record_count*kRecord,records+record_count*kRecord};
        result=original_copy(unused,&ours);
    } else {
        official_since_off.store(true);
        result=original_copy(unused,official);
    }
    ReleaseSRWLockShared(&records_lock);
    return result;
}
void JoinHandleHook(const uint8_t* handle) {
    // MSVC std::string: inline buffer when capacity < 16, size at +0x10, capacity at +0x18.
    size_t size=0,capacity=0; std::memcpy(&size,handle+0x10,8); std::memcpy(&capacity,handle+0x18,8);
    const char* text=capacity>=16 ? *reinterpret_cast<const char* const*>(handle) : reinterpret_cast<const char*>(handle);
    char id[40];
    if(text && StartsWithSentinel(text,size,id)) { Join(id); return; }
    original_join_handle(handle);
}
void JoinFireteamHook(uint32_t user,uint64_t xuid,const uint8_t* span) {
    const char* text=nullptr; size_t size=0; std::memcpy(&text,span,8); std::memcpy(&size,span+8,8);
    char id[40];
    if(text && StartsWithSentinel(text,size,id)) { Join(id); return; }
    original_join_fireteam(user,xuid,span);
}
// After the stock page builder (0x1406a1a7c(dc)) recreates the tiles, OpenLink tiles show
// the server name in place of the map name. Items: list object at dc+0x1a0 with its item
// pointer array from *(dc+0x1a8) to *(dc+0x1b0); MapName wstring +0x100, SearchHandleId
// std::string +0x2a8 (FN060).
using PageBuilder=void (*)(uint8_t*);
PageBuilder original_page=nullptr;
std::atomic<uint32_t> renamed{0};
void PageBuilderHook(uint8_t* dc) {
    original_page(dc);
    if(!mode.load() || !dc) return;
    uintptr_t first=0,last=0;
    if(!SafeCopy(&first,dc+0x1a8,8) || !SafeCopy(&last,dc+0x1b0,8) || !first || last<first || (last-first)%8 || (last-first)/8>kMaxRecords) return;
    AcquireSRWLockShared(&records_lock);
    for(uintptr_t at=first;at<last;at+=8) {
        uint8_t* item=nullptr; if(!SafeCopy(&item,reinterpret_cast<const void*>(at),8) || !item) continue;
        char handle[64]; char id[40];
        if(!ReadString(item+0x2a8,handle,sizeof(handle)) || !StartsWithSentinel(handle,std::strlen(handle),id)) continue;
        for(size_t i=0;i<name_count;++i) {
            if(std::strcmp(names[i].id,id)) continue;
            // The tile mode label (ModeName +0x120) shows the host's playlist entry name.
            if(names[i].mode_length && !WStringEquals(item+0x120,names[i].mode,names[i].mode_length)) Game<Assign>(kAssign)(item+0x120,names[i].mode,names[i].mode_length);
            if(names[i].length && !WStringEquals(item+0x100,names[i].name,names[i].length)) {
                Game<Assign>(kAssign)(item+0x100,names[i].name,names[i].length); renamed.fetch_add(1);
            }
            break;
        }
    }
    ReleaseSRWLockShared(&records_lock);
}
// The grid Join 0x14346ed48(dc,index,page) resolves index against the global result
// vector and ignores page, so from page 2 on it picks a page-1 record (FN060 Q4). In OpenLink
// mode the tile's own item (same page item array as above) names the server instead.
using JoinAt=void (*)(uint8_t*,int32_t,int32_t);
JoinAt original_join_at=nullptr;
void JoinAtHook(uint8_t* dc,int32_t index,int32_t page) {
    if(!mode.load()) { original_join_at(dc,index,page); return; }
    uintptr_t first=0,last=0; uint8_t* item=nullptr; char handle[64],id[40];
    if(dc && index>=0 && SafeCopy(&first,dc+0x1a8,8) && SafeCopy(&last,dc+0x1b0,8) && first && last>first && !((last-first)%8) &&
       uintptr_t(index)<(last-first)/8 && SafeCopy(&item,reinterpret_cast<const void*>(first+uintptr_t(index)*8),8) && item &&
       ReadString(item+0x2a8,handle,sizeof(handle)) && StartsWithSentinel(handle,std::strlen(handle),id)) { Join(id); return; }
    // Every record is ours while the mode is on, so the stock lookup could only pick the wrong server.
    char line[96]; sprintf_s(line,"grid join refused: no OpenLink tile at index %d page %d",index,page); browser::Log(line);
}
}

namespace {
// The details screen's breadcrumb "Details" (0x38afcfda, O165 trace) shows the selected
// server's name. The details page keeps the selected handle (std::string) at 0x144e10800.
bool CopyText(const wchar_t* text,wchar_t* out,size_t capacity) {
    const size_t length=std::wcslen(text),n=length<capacity-1 ? length : capacity-1;
    std::memcpy(out,text,n*sizeof(wchar_t)); out[n]=0; return true;
}
bool OverlayText(uint32_t sid,wchar_t* out,size_t capacity) {
    if(capacity<2) return false;
    // The empty grid ("No Custom Games Found", 0xdc6e123f) says why it is empty. With a
    // working directory and no servers the static overlay text applies.
    if(sid==0xdc6e123fu) {
        if(!browser::Configured()) return CopyText(L"OpenLink directory not configured",out,capacity);
        if(browser::FetchError()) return CopyText(L"Can't reach the OpenLink directory",out,capacity);
        if(!browser::Fetched()) return CopyText(L"Loading OpenLink servers...",out,capacity);
        return false;
    }
    if(sid!=0x38afcfdau || !image) return false;
    char handle[64],id[40];
    if(!ReadString(reinterpret_cast<const uint8_t*>(image+0x4e10800),handle,sizeof(handle)) || !StartsWithSentinel(handle,std::strlen(handle),id)) return false;
    bool found=false;
    // Never wait here: a text lookup made while Tick holds the records exclusively would deadlock.
    if(!TryAcquireSRWLockShared(&records_lock)) return false;
    for(size_t i=0;i<name_count && !found;++i) if(!std::strcmp(names[i].id,id) && names[i].length) {
        const size_t n=names[i].length<capacity-1 ? names[i].length : capacity-1;
        std::memcpy(out,names[i].name,n*sizeof(wchar_t)); out[n]=0; found=true;
    }
    ReleaseSRWLockShared(&records_lock);
    return found;
}
}
namespace {
// The details tab's FileDescription getter 0x140f05e54 (AssetTabSharedDetailsDC, vtable
// 0x1436bda98) stores a borrowed wchar_t* to the map description (DC+0x178) in its UI value at
// r8+8 (type 3 at r8). For an OpenLink tile with a listing description, that pointer is
// replaced afterwards by one of ours (FN061). The UI may keep the pointer, so ours live in a
// never-freed ring and a slot is reused only for the same text or after 32 other texts.
using Getter=bool (*)(uint8_t*,void*,uint8_t*);
Getter original_description=nullptr;
constexpr size_t kDescriptionSlots=32,kDescriptionChars=484;
wchar_t (*description_ring)[kDescriptionChars]=nullptr; size_t description_next=0;
SRWLOCK description_lock=SRWLOCK_INIT;
std::atomic<uint32_t> described{0};
bool SelectedId(char (&id)[40]) {
    char handle[64];
    return image && ReadString(reinterpret_cast<const uint8_t*>(image+0x4e10800),handle,sizeof(handle)) && StartsWithSentinel(handle,std::strlen(handle),id);
}
bool DescriptionHook(uint8_t* dc,void* key,uint8_t* value) {
    const bool result=original_description(dc,key,value);
    if(!result || !mode.load() || !dc || !value || !description_ring) return result;
    uintptr_t vtable=0; int32_t type=0; char id[40];
    if(!SafeCopy(&vtable,dc,8) || vtable!=image+0x36bda98 || !SafeCopy(&type,dc+0x170,4) || type!=8 || !SelectedId(id)) return result;
    wchar_t text[kDescriptionChars]; size_t length=0;
    if(!TryAcquireSRWLockShared(&records_lock)) return result; // never wait (see OverlayText)
    for(size_t i=0;i<name_count;++i) if(!std::strcmp(names[i].id,id)) {
        length=names[i].description_length<kDescriptionChars-1 ? names[i].description_length : kDescriptionChars-1;
        std::memcpy(text,names[i].description,length*sizeof(wchar_t)); break;
    }
    ReleaseSRWLockShared(&records_lock);
    if(!length) return result; // no listing description: the map description stays
    text[length]=0;
    AcquireSRWLockExclusive(&description_lock);
    wchar_t* slot=nullptr;
    for(size_t i=0;i<kDescriptionSlots && !slot;++i) if(!std::wcscmp(description_ring[i],text)) slot=description_ring[i];
    if(!slot) { slot=description_ring[description_next]; description_next=(description_next+1)%kDescriptionSlots; std::memcpy(slot,text,(length+1)*sizeof(wchar_t)); }
    ReleaseSRWLockExclusive(&description_lock);
    uint16_t kind=0; std::memcpy(&kind,value,2);
    if(kind==3) { std::memcpy(value+8,&slot,8); if(!described.fetch_add(1)) browser::Log("details description replaced"); }
    return result;
}
// The details tab's IsCustomGameXPValid getter 0x140dbd69c (same DC, byte DC+0x226)
// shows the custom-game XP block (cap/eligible text and warning icon) when true (FN062). It
// writes type 0 at r8 and the bool at r8+8; for OpenLink tiles it reports false.
Getter original_xp=nullptr;
bool XpHook(uint8_t* dc,void* key,uint8_t* value) {
    const bool result=original_xp(dc,key,value);
    if(!result || !mode.load() || !dc || !value) return result;
    uintptr_t vtable=0; int32_t type=0; uint16_t kind=1; char id[40];
    if(!SafeCopy(&vtable,dc,8) || vtable!=image+0x36bda98 || !SafeCopy(&type,dc+0x170,4) || type!=8 || !SafeCopy(&kind,value,2) || kind!=0 || !SelectedId(id)) return result;
    value[8]=0;
    return result;
}
// Refused joins open the stock OK dialog "campaign_generic_dialog" from Lua
// (OpenLinkUI.notify); while the notice is fresh its title 0xd8386f08 and body 0x345b4b29
// show our text, at all times (text::SetAlwaysProvider: the lobby's "Leave server?" dialog too).
std::atomic<int> notice_kind{0}; std::atomic<bool> notice_pending{false}; std::atomic<ULONGLONG> notice_until{0};
void Notice(const char* refusal) {
    int kind=3;
    if(!std::strcmp(refusal,browser::kNeedsUpdate)) kind=4;
    else if(!std::strcmp(refusal,"server is not listed or not joinable")) kind=1;
    else if(!std::strcmp(refusal,"a join is already in progress")) kind=2;
    notice_kind.store(kind); notice_until.store(GetTickCount64()+15000); notice_pending.store(true);
}
// The lobby's "Leave server?" confirmation is the front-end's quit dialog (0x266ab009, OKAY + CANCEL,
// loaded in the lobby; O198): its title 0x1b186cbd and question 0x5fbd0a42 read ours while it is up.
bool NoticeText(uint32_t sid,wchar_t* out,size_t capacity) {
    const bool generic=sid==0xd8386f08u || sid==0x345b4b29u, quit=sid==0x1b186cbdu || sid==0x5fbd0a42u;
    if((!generic && !quit) || GetTickCount64()>=notice_until.load()) return false;
    const int kind=notice_kind.load();
    if(quit) return kind==5 && CopyText(sid==0x1b186cbdu ? L"Leave Server?" : L"You will leave this OpenLink server and go back to Custom Games.",out,capacity);
    if(sid==0xd8386f08u) return CopyText(kind==4 ? L"Update OpenLink Launcher" : kind==5 ? L"Leave server?" : L"Can't join this server",out,capacity);
    switch(kind) {
    case 5: return CopyText(L"You will leave this OpenLink server and go back to Custom Games. Press Back to stay.",out,capacity);
    case 1: return CopyText(L"This OpenLink server is full or not accepting players right now. Try again in a moment or pick another server.",out,capacity);
    case 2: return CopyText(L"A join is already in progress. Wait for it to finish.",out,capacity);
    case 4: return CopyText(L"This server needs a newer OpenLink Launcher. Download the latest OpenLink Launcher to join it.",out,capacity);
    default: return CopyText(L"OpenLink could not start this join. Close the browser and try again.",out,capacity);
    }
}
}
uint32_t Install(uintptr_t module_image) noexcept {
    if(installed.load()) return ERROR_SUCCESS;
    image=module_image;
    static const uint8_t kCopyPrologue[]={0x48,0x89,0x5c,0x24,0x18,0x48,0x89,0x4c,0x24,0x08,0x56,0x57,0x41,0x56,0x48,0x81};
    static const uint8_t kQueryPrologue[]={0x48,0x89,0x5c,0x24,0x20,0x55,0x56,0x57,0x41,0x54,0x41,0x55,0x41,0x56,0x41,0x57};
    static const uint8_t kJoinHandlePrologue[]={0x48,0x89,0x5c,0x24,0x08,0x48,0x89,0x7c,0x24,0x18,0x55,0x48,0x8d,0x6c,0x24,0xa9};
    static const uint8_t kJoinFireteamPrologue[]={0x48,0x89,0x5c,0x24,0x08,0x48,0x89,0x6c,0x24,0x10,0x48,0x89,0x74,0x24,0x18,0x57};
    uint32_t e=ERROR_SUCCESS;
    // Join hooks first: once our records can appear, their joins must already be caught.
    original_join_handle=reinterpret_cast<JoinHandle>(detour::Install(image,kJoinHandle,kJoinHandlePrologue,16,5,reinterpret_cast<const void*>(&JoinHandleHook),&e)); if(!original_join_handle) return e;
    original_join_fireteam=reinterpret_cast<JoinFireteam>(detour::Install(image,kJoinFireteam,kJoinFireteamPrologue,16,5,reinterpret_cast<const void*>(&JoinFireteamHook),&e)); if(!original_join_fireteam) return e;
    original_query=reinterpret_cast<Query>(detour::Install(image,kQuery,kQueryPrologue,16,5,reinterpret_cast<const void*>(&QueryHook),&e)); if(!original_query) return e;
    original_copy=reinterpret_cast<Copy>(detour::Install(image,kCopy,kCopyPrologue,16,5,reinterpret_cast<const void*>(&CopyHook),&e)); if(!original_copy) return e;
    // Optional: server names on tiles. Without it the tiles keep the map name.
    static const uint8_t kPagePrologue[]={0x48,0x8b,0xc4,0x55,0x53,0x56,0x57,0x41,0x54,0x41,0x55,0x41,0x56,0x41,0x57,0x48};
    uint32_t page_error=0;
    original_page=reinterpret_cast<PageBuilder>(detour::Install(image,0x6a1a7c,kPagePrologue,16,5,reinterpret_cast<const void*>(&PageBuilderHook),&page_error));
    if(!original_page) { char note[96]; sprintf_s(note,"tile name hook unavailable (%u)",page_error); browser::Log(note); }
    // Optional: page-aware grid Join. Without it a grid Join past page 1 may pick another server.
    static const uint8_t kJoinAtPrologue[]={0x48,0x89,0x5c,0x24,0x08,0x48,0x89,0x7c,0x24,0x10,0x55,0x48,0x8b,0xec,0x48,0x81};
    uint32_t join_at_error=0;
    original_join_at=reinterpret_cast<JoinAt>(detour::Install(image,0x346ed48,kJoinAtPrologue,16,5,reinterpret_cast<const void*>(&JoinAtHook),&join_at_error));
    if(!original_join_at) { char note[96]; sprintf_s(note,"grid join hook unavailable (%u)",join_at_error); browser::Log(note); }
    // Optional: server description on the details page. Without it the map description shows.
    static const uint8_t kDescriptionPrologue[]={0x40,0x53,0x48,0x83,0xec,0x20,0x49,0x8b,0xd8,0xe8,0x16,0x00,0x00,0x00};
    description_ring=static_cast<wchar_t (*)[kDescriptionChars]>(HeapAlloc(GetProcessHeap(),HEAP_ZERO_MEMORY,sizeof(wchar_t)*kDescriptionSlots*kDescriptionChars));
    uint32_t description_error=ERROR_NOT_ENOUGH_MEMORY;
    if(description_ring) original_description=reinterpret_cast<Getter>(detour::Install(image,0xf05e54,kDescriptionPrologue,sizeof(kDescriptionPrologue),6,reinterpret_cast<const void*>(&DescriptionHook),&description_error));
    if(!original_description) { char note[96]; sprintf_s(note,"details description hook unavailable (%u)",description_error); browser::Log(note); }
    // Optional: hide the custom-game XP block on OpenLink details pages.
    static const uint8_t kXpPrologue[]={0x40,0x53,0x48,0x83,0xec,0x20,0x49,0x8b,0xd8,0xe8,0x76,0x00,0x00,0x00};
    uint32_t xp_error=0;
    original_xp=reinterpret_cast<Getter>(detour::Install(image,0xdbd69c,kXpPrologue,sizeof(kXpPrologue),6,reinterpret_cast<const void*>(&XpHook),&xp_error));
    if(!original_xp) { char note[96]; sprintf_s(note,"details XP hook unavailable (%u)",xp_error); browser::Log(note); }
    text::SetOverlayProvider(&OverlayText);
    text::SetAlwaysProvider(&NoticeText); // dialog text also outside the browser (the lobby's leave dialog)
    installed.store(true); browser::Log("customs browser hooks installed");
    return ERROR_SUCCESS;
}
bool SetMode(bool on) noexcept {
    if(!installed.load() || disabled.load()) return false;
    // Reset before switching off, so an official result that lands right after the switch
    // is noticed and never wiped by our clear.
    if(!on) official_since_off.store(false);
    const bool was=mode.exchange(on);
    if(on && !was) { force_push.store(true); text::SetOverlayActive(true); browser::Log("customs browser OpenLink mode on"); }
    if(was && !on) { clear_pending.store(true); text::SetOverlayActive(false); browser::Log("customs browser OpenLink mode off"); }
    return true;
}
bool ParseThumbRef(const char* ref,uint8_t (&asset)[16],uint8_t (&version)[16]) noexcept { return ref && ParsePair(ref,asset,version); }
bool TakeNotice() noexcept { return notice_pending.exchange(false); }
// The leave confirmation reuses the same dialog text (the script opens the dialog itself).
void LeaveNotice() noexcept { notice_kind.store(5); notice_until.store(GetTickCount64()+15000); }
void RequeueNotice() noexcept { notice_pending.store(true); }
void Disable() noexcept { disabled.store(true); text::SetOverlayActive(false); if(mode.exchange(false)) browser::Log("customs browser OpenLink mode disabled"); }
bool Mode() noexcept { return mode.load(); }
void Tick() noexcept {
    if(!installed.load()) return;
    if(clear_pending.exchange(false)) {
        // Leave nothing of ours behind for the next official visit. Records are released and
        // the game vector emptied under records_lock (same order as CopyHook), unless a real
        // official result has already replaced our rows since the mode went off.
        AcquireSRWLockExclusive(&records_lock);
        uint8_t* old=records; const size_t old_count=record_count; records=nullptr; record_count=0;
        const bool official=official_since_off.load();
        if(!official && !mode.load()) { Vector empty={nullptr,nullptr,nullptr}; original_copy(0,&empty); }
        ReleaseSRWLockExclusive(&records_lock);
        Destroy(old,old_count);
        pushed_revision=0xffffffffu;
        char line[224]; sprintf_s(line,"customs browser cleared%s (blocked queries %u, swapped results %u, joins %u, description reads %u)",official ? " (official result already in)" : "",
            blocked_queries.load(),swapped_results.load(),caught_joins.load(),described.load());
        browser::Log(line);
        return;
    }
    if(!mode.load()) return;
    browser::BrowserRow probe[1]; uint32_t rev=0; browser::CopyRows(probe,0,rev);
    const bool forced=force_push.exchange(false);
    if(rev==pushed_revision && !forced) return;
    size_t count=0; static TileName fresh[kMaxRecords]; size_t fresh_count=0; uint8_t* set=Build(count,fresh,fresh_count);
    AcquireSRWLockExclusive(&records_lock);
    uint8_t* old=records; const size_t old_count=record_count; records=set; record_count=count;
    std::memcpy(names,fresh,sizeof(names)); name_count=fresh_count;
    if(mode.load()) { Vector ours={set,set ? set+count*kRecord : nullptr,set ? set+count*kRecord : nullptr}; original_copy(0,&ours); }
    ReleaseSRWLockExclusive(&records_lock);
    Destroy(old,old_count);
    pushed_revision=rev;
    char line[128]; sprintf_s(line,"customs browser records pushed: %zu%s (tiles renamed so far %u)",count,forced ? " (requested)" : "",renamed.load()); browser::Log(line);
}
}
}
namespace clientjoin {
namespace cbrowser {
bool TestParseGuid() noexcept {
    uint8_t g[16]; const char* s="70f884d7-6869-469d-b4d2-4219627e2d83";
    static const uint8_t want[16]={0xd7,0x84,0xf8,0x70,0x69,0x68,0x9d,0x46,0xb4,0xd2,0x42,0x19,0x62,0x7e,0x2d,0x83};
    uint8_t bad[16];
    return ParseGuid(s,std::strlen(s),g) && !std::memcmp(g,want,16) && !ParseGuid("70f884d7x6869-469d-b4d2-4219627e2d83",36,bad) && !ParseGuid(s,35,bad);
}
}
}
