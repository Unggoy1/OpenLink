// Lobby map header (OpenLink, AGPL-3.0); see mapheader.h and FN069.
// The lobby update 0x140953b98 reads the map to show from the global 0x1448d9518
// ({int kind; GUID asset @+4; GUID version @+0x14}, 0x24 bytes) through the leaf 0x1414f1b90,
// once for the asset and once for the version. The leaf's loads are RIP-relative, so it is
// replaced whole: the handler copies the global as the original does and, only for those two
// calls and only while an OpenLink lobby has a known map, returns that map instead. The global
// itself is never written (it feeds the player's own lobbies and session setup).
#define WIN32_LEAN_AND_MEAN
#include <windows.h>
#include <intrin.h>
#include "mapheader.h"
#include "detour.h"
#include "relay.h"
#include "vote.h"
#include "browser.h"
#include "cbrowser.h"
#include "overlay.h"
#include <atomic>
#include <cstdio>
#include <cstring>
#pragma intrinsic(_ReturnAddress)
namespace clientjoin {
namespace mapheader {
namespace {
constexpr uintptr_t kLeaf=0x14f1b90,kGlobal=0x48d9518,kAssetCall=0x953f21,kVersionCall=0x953f31;
uintptr_t image=0;
void (*log_sink)(const char*)=nullptr;
// The handler runs on game threads without locks: it reads an immutable snapshot through an
// atomic pointer; snapshots rotate through a ring, so one is never rewritten while in use.
struct Map { uint8_t asset[16],version[16]; };
Map ring[8]={}; size_t next=0;
std::atomic<const Map*> current{nullptr};
std::atomic<uint32_t> used{0};
void* __fastcall LeafHook(uint8_t* out) {
    std::memcpy(out,reinterpret_cast<const void*>(image+kGlobal),0x24);
    const uintptr_t from=reinterpret_cast<uintptr_t>(_ReturnAddress());
    if(from==image+kAssetCall || from==image+kVersionCall) {
        if(const Map* m=current.load(std::memory_order_acquire)) {
            const int32_t kind=2; std::memcpy(out,&kind,4); std::memcpy(out+4,m->asset,16); std::memcpy(out+0x14,m->version,16);
            used.fetch_add(1,std::memory_order_relaxed);
        }
    }
    return out;
}
// Worker thread only.
char shown[96]={}; uint32_t last_down=0; ULONGLONG down_at=0; bool logged_use=false;
void Log(const char* line) { if(log_sink) log_sink(line); }
}
// Its own thread: the engine tick pauses in an idle lobby (O190), the header must still follow votes.
static DWORD WINAPI Worker(void*) { for(;;) { Tick(); Sleep(250); } }
uint32_t Install(uintptr_t module_image,void (*log)(const char*)) noexcept {
    image=module_image; log_sink=log;
    static const uint8_t kExpected[]={0x8b,0x05,0xa2,0x79,0x3e,0x03,0x0f,0x10,0x05,0x7b,0x79,0x3e,0x03,0x0f,0x10,0x0d,0x84,0x79,0x3e,0x03,
                                      0x0f,0x11,0x01,0x0f,0x11,0x49,0x10,0x89,0x41,0x20,0x48,0x8b,0xc1,0xc3};
    uint32_t e=0;
    if(!detour::Replace(image,kLeaf,kExpected,sizeof(kExpected),reinterpret_cast<const void*>(&LeafHook),&e)) return e;
    if(HANDLE t=CreateThread(nullptr,0,&Worker,nullptr,0,nullptr)) CloseHandle(t); else return GetLastError();
    return 0;
}
void Tick() noexcept {
    if(!image) return;
    // Connected to an OpenLink server: its traffic is still arriving through our relay.
    const relay::Status st=relay::Snapshot(); const ULONGLONG now=GetTickCount64();
    if(st.down!=last_down) { last_down=st.down; down_at=now; }
    const bool connected=st.sessions>0 && now-down_at<5000;
    char ref[96]={}; const char* source="";
    const bool lobby=overlay::LobbyShowing();
    vote::View v={}; const bool ballot=vote::Current(v);
    {
        // Why the header is (not) ours, logged when it changes.
        static int seen=-1;
        const int state=(connected?1:0)|(lobby?2:0)|(ballot?4:0)|(ballot && v.ballot.closed?8:0)|(ballot && v.mine>=0?16:0);
        if(state!=seen && (lobby || seen>=0)) {
            seen=state; char line[160];
            sprintf_s(line,"lobby map header: connected=%d lobby=%d ballot=%s voted=%d (sessions %u)",connected?1:0,lobby?1:0,
                      !ballot ? "none" : v.ballot.closed ? "closed" : "open",ballot && v.mine>=0 ? 1 : 0,st.sessions);
            Log(line);
        }
    }
    if(connected && lobby) {
        if(ballot) {
            if(v.ballot.closed && v.ballot.winner>=0) { strcpy_s(ref,v.ballot.options[v.ballot.winner].thumb); source="vote winner"; }
            else if(!v.ballot.closed && v.mine>=0) { strcpy_s(ref,v.ballot.options[v.mine].thumb); source="your vote"; }
            else if(!v.ballot.closed) {
                // Not voted yet: the choice in the lead, if one leads alone (before any vote the stock
                // header stays, the game's default map).
                int lead=-1,best=0; bool tie=false;
                for(uint32_t i=0;i<v.ballot.count;++i) {
                    if(v.ballot.counts[i]>best) { best=v.ballot.counts[i]; lead=int(i); tie=false; }
                    else if(best>0 && v.ballot.counts[i]==best) tie=true;
                }
                if(lead>=0 && !tie) { strcpy_s(ref,v.ballot.options[lead].thumb); source="leading"; }
            }
        }
        if(!ref[0] && browser::JoinedThumb(ref)) source="server listing";
    }
    if(std::strcmp(ref,shown)) {
        strcpy_s(shown,ref);
        Map m={};
        char line[200];
        if(ref[0] && cbrowser::ParseThumbRef(ref,m.asset,m.version)) {
            ring[next]=m; current.store(&ring[next],std::memory_order_release); next=(next+1)%8;
            sprintf_s(line,"lobby map header -> %s (%s)",ref,source);
        } else { current.store(nullptr,std::memory_order_release); strcpy_s(line,"lobby map header: stock"); }
        Log(line);
    }
    if(!logged_use && used.load()) { logged_use=true; Log("lobby map header: the lobby update read our map"); }
}
}
}
