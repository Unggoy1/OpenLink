#pragma once
#include <cstdint>
namespace clientjoin {
struct Request { char host[16]={},map[37]={},variant[37]={}; uint32_t flags=0; };
enum Phase : uint32_t { Idle,WaitingLan,Armed,Entered,Failed,LobbyRequested,Selecting,Selected };
// Select action results: wait for a matching browse entry, dispatched once, or refused.
enum Selection : int { SelectWait=0,SelectDispatched=1,SelectRefused=-1 };
enum Code : uint16_t { Ok,Unsupported,NotReady,Busy,Invalid,Failure,Pending,Timeout };
struct Frame { bool client=false,no_eac=false,front_end=false; int32_t mode=-1; uint32_t kind=0xffffffff; int32_t pre_state=-1,pre_target=-1; };
struct Actions {
    void* context;
    bool (*switch_lan)(void*);
    bool (*arm)(void*,const Request&);
    bool (*start)(void*);
    uint32_t (*kind)(void*);
    bool (*begin_lan_lobby)(void*);
    bool (*can_init_lobby)(void*);
    int (*select)(void*,const Request&);
};
// Flag 16: normal LAN host setup, then the stock Server-picker selection
// of the discovered entry whose address is the request host. No auto-connect arming.
constexpr uint64_t kSelectSettleMs=5000,kSelectWindowMs=30000;
inline bool ValidIPv4(const char* p) noexcept {
    unsigned at=0;
    for(unsigned part=0;part<4;++part) {
        unsigned n=0,count=0,start=at;
        while(at<16 && p[at]>='0' && p[at]<='9') { n=n*10+unsigned(p[at++]-'0'); if(++count>3 || n>255) return false; }
        if(!count || at>=16) return false;
        if((count>1 && p[start]=='0') || (!part && (!n || n>=224))) return false;
        if(part==3) return p[at]=='\0';
        if(p[at++]!='.') return false;
    }
    return false;
}
inline bool ValidGuid(const char* p) noexcept {
    for(unsigned i=0;i<36;++i) {
        if(i==8 || i==13 || i==18 || i==23) { if(p[i]!='-') return false; }
        else if(!((p[i]>='0' && p[i]<='9') || (p[i]>='a' && p[i]<='f') || (p[i]>='A' && p[i]<='F'))) return false;
    }
    return p[36]=='\0';
}
inline bool ValidRequest(const Request& r) noexcept {
    return ValidIPv4(r.host) && !(r.flags&~31u) && (!(r.flags&28) || r.flags==4 || r.flags==8 || r.flags==16) &&
        ((r.flags&2) ? ValidGuid(r.map)&&ValidGuid(r.variant) : !r.map[0]&&!r.variant[0]);
}
class JoinMachine {
public:
    Code Submit(const Request& r,uint64_t now) noexcept {
        if(!ValidRequest(r)) return Invalid;
        if(phase!=Idle) return Busy;
        request_=r; deadline_=now+3000; phase=WaitingLan; code=Pending; return code;
    }
    void Tick(const Frame& f,uint64_t now,const Actions& a) noexcept {
        if(phase!=WaitingLan && phase!=Armed && phase!=Selecting) return;
        if(!f.client || !f.no_eac) { phase=Failed; code=Failure; return; }
        if(phase==Selecting) {
            if(f.kind!=1) { phase=Failed; code=Failure; return; }
            if(now-requested_at_<kSelectSettleMs) return;
            if(now-requested_at_>=kSelectWindowMs) { phase=Failed; code=Timeout; return; }
            const int selected=a.select(a.context,request_);
            if(selected==SelectDispatched) { phase=Selected; code=Ok; }
            else if(selected!=SelectWait) { phase=Failed; code=Failure; }
            return;
        }
        if(phase==WaitingLan) {
            if(now>=deadline_) { phase=Failed; code=Timeout; return; }
            const bool front=f.front_end && f.mode==0;
            const bool pre=!f.front_end && f.mode==5 && f.pre_state==0 && f.pre_target==-1;
            if(!front && !pre) { phase=Failed; code=NotReady; return; }
            const bool lobby=(request_.flags&28)!=0;
            if(front && lobby) { phase=Failed; code=NotReady; return; }
            if(lobby && !lobby_checked_) {
                if(!a.can_init_lobby(a.context)) { phase=Failed; code=Failure; return; }
                lobby_checked_=true;
            }
            if(!switched_) {
                switched_=true;
                if(!a.switch_lan(a.context)) { phase=Failed; code=Failure; return; }
                if(front) return;
            }
            const uint32_t kind=pre ? a.kind(a.context) : f.kind;
            if(kind!=1) return;
            if(lobby && !a.can_init_lobby(a.context)) { phase=Failed; code=Failure; return; }
            if(request_.flags&24) {
                if(!a.begin_lan_lobby(a.context)) { phase=Failed; code=Failure; return; }
                if(request_.flags&16) { requested_at_=now; phase=Selecting; code=Pending; return; }
                phase=LobbyRequested; code=Ok; return;
            }
            if(!a.arm(a.context,request_)) { phase=Failed; code=Failure; return; }
            if((request_.flags&4) && !a.begin_lan_lobby(a.context)) { phase=Failed; code=Failure; return; }
            armed_at_=now; phase=Armed; code=Ok; return;
        }
        if(f.kind!=1) { phase=Failed; code=Failure; return; }
        if(!f.front_end && f.mode==5 && f.pre_state==3) { phase=Entered; code=Ok; return; }
        if(f.front_end && (request_.flags&1) && !fallback_used && f.mode==0 && now-armed_at_>=4000) {
            fallback_used=true;
            if(!a.start(a.context)) { phase=Failed; code=Failure; }
        }
    }
    void Cancel() noexcept { if(phase==WaitingLan || phase==Armed || phase==Selecting) { phase=Failed; code=Failure; } }
    // Forget a finished join so another can start (caller ensures the game is idle).
    void Reset() noexcept { *this=JoinMachine(); }
    Phase phase=Idle;
    Code code=NotReady;
    bool fallback_used=false;
private:
    Request request_={};
    uint64_t deadline_=0,armed_at_=0,requested_at_=0;
    bool switched_=false,lobby_checked_=false;
};
}
