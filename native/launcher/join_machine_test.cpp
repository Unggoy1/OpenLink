#include "join_machine.h"
#include <cstdio>
#include <cstdlib>
#include <cstring>
using namespace clientjoin;
void check(bool ok,const char* why) { if(!ok) { std::fprintf(stderr,"FAIL: %s\n",why); std::exit(1); } }
struct Engine {
    unsigned switches=0,arms=0,starts=0,begins=0,validations=0; bool succeed=true,can_init=true,session_after_switch=false; Request received={};
    uint32_t kind=0,kind_after_switch=1; char order[16]={}; unsigned order_count=0;
    static bool Switch(void* p) { auto& e=*static_cast<Engine*>(p); ++e.switches; e.order[e.order_count++]='S'; e.kind=e.kind_after_switch; if(e.session_after_switch) e.can_init=false; return e.succeed; }
    static bool Arm(void* p,const Request& r) { auto& e=*static_cast<Engine*>(p); ++e.arms; e.order[e.order_count++]='A'; e.received=r; return e.succeed; }
    static bool Start(void* p) { auto& e=*static_cast<Engine*>(p); ++e.starts; return e.succeed; }
    static uint32_t Kind(void* p) { auto& e=*static_cast<Engine*>(p); e.order[e.order_count++]='K'; return e.kind; }
    static bool Begin(void* p) { auto& e=*static_cast<Engine*>(p); ++e.begins; e.order[e.order_count++]='B'; return e.succeed; }
    static bool CanInit(void* p) { auto& e=*static_cast<Engine*>(p); ++e.validations; e.order[e.order_count++]='V'; return e.can_init; }
    unsigned selects=0; int select_result=SelectDispatched;
    static int Select(void* p,const Request& r) { auto& e=*static_cast<Engine*>(p); ++e.selects; e.received=r; return e.select_result; }
    Actions actions() { return {this,Switch,Arm,Start,Kind,Begin,CanInit,Select}; }
};
Request host() { Request r; strcpy_s(r.host,"192.168.1.20"); return r; }
int main() {
    Frame online={true,true,true,0,0},lan={true,true,true,0,1};
    {
        JoinMachine m; Engine e; auto r=host();
        check(m.Submit(r,100)==Pending,"valid join is queued");
        m.Tick(online,101,e.actions());
        check(e.switches==1 && e.arms==0,"online must switch before arming");
        m.Tick(online,200,e.actions());
        check(e.switches==1 && e.arms==0,"wait for LAN without repeated native switch");
        m.Tick(lan,201,e.actions());
        check(e.arms==1 && std::strcmp(e.received.host,"192.168.1.20")==0,"LAN confirmation arms selected host once");
        m.Tick(lan,9000,e.actions());
        check(e.starts==0,"fallback disabled by default");
        check(m.Submit(r,9001)==Busy,"one join per process prototype");
    }
    {
        JoinMachine m; Engine e; m.Submit(host(),0); m.Tick(lan,3001,e.actions());
        check(m.code==Timeout && !e.switches && !e.arms,"expired request never executes on late tick");
    }
    {
        JoinMachine m; Engine e; m.Submit(host(),0); auto f=lan; f.no_eac=false; m.Tick(f,1,e.actions());
        check(m.phase==Failed && !e.switches && !e.arms,"protection gate refuses all native actions");
    }
    {
        JoinMachine m; Engine e; auto r=host(); r.flags=1; m.Submit(r,0);
        m.Tick(lan,1,e.actions()); m.Tick(lan,2,e.actions()); m.Tick(lan,4002,e.actions()); m.Tick(lan,5000,e.actions());
        check(e.starts==1 && m.fallback_used,"explicit fallback runs once after four seconds");
    }
    {
        JoinMachine m; Engine e; auto r=host(); r.flags=1; m.Submit(r,0);
        m.Tick(lan,1,e.actions()); m.Tick(lan,2,e.actions()); auto f=lan; f.mode=5; f.front_end=false; f.pre_state=2; m.Tick(f,3,e.actions());
        check(m.phase==Armed,"host copied in state2 is not connect completion");
        f.pre_state=3; m.Tick(f,4,e.actions());
        check(m.phase==Entered && !e.starts,"native connect state3 is reported, not lobby admission");
    }
    {
        JoinMachine m; Engine e; m.Submit(host(),0); m.Cancel(); m.Tick(lan,1,e.actions());
        check(!e.switches && !e.arms,"cancelled queued work never runs");
    }
    {
        JoinMachine m; Engine e; Frame f={true,true,false,5,0,0,-1};
        check(m.Submit(host(),0)==Pending,"idle existing pre-game request is queued");
        m.Tick(f,1,e.actions());
        check(e.switches==1 && e.arms==1 && !std::strcmp(e.order,"SKA"),"idle pre-game switches, reads back LAN and arms before original update");
        f.kind=1; m.Tick(f,2,e.actions());
        check(m.phase==Armed,"pre-existing mode5 with idle state is not join progress");
        f.pre_state=2; m.Tick(f,3,e.actions());
        check(m.phase==Armed,"pre-game state2 is still before connect handoff");
        f.pre_state=3; m.Tick(f,4,e.actions());
        check(m.phase==Entered && !e.starts,"native connect handoff advance is recorded without fallback");
    }
    {
        JoinMachine m; Engine e; e.kind_after_switch=0; Frame f={true,true,false,5,0,0,-1};
        m.Submit(host(),0); m.Tick(f,1,e.actions());
        check(e.switches==1 && !e.arms,"deferred LAN switch cannot arm from stale kind");
        e.kind=1; f.kind=1; m.Tick(f,2,e.actions());
        check(e.switches==1 && e.arms==1,"deferred switch readback later arms once");
    }
    {
        JoinMachine m; Engine e; Frame f={true,true,false,5,0,2,-1}; m.Submit(host(),0); m.Tick(f,1,e.actions());
        check(m.phase==Failed && !e.switches && !e.arms,"busy pre-game state is never adopted");
    }
    {
        JoinMachine m; Engine e; Frame f={true,true,false,5,0,0,0x2711}; m.Submit(host(),0); m.Tick(f,1,e.actions());
        check(m.phase==Failed && !e.switches && !e.arms,"existing pre-game activity target is never redirected");
    }
    {
        JoinMachine m; Engine e; Frame f={true,true,false,5,0,0,-1}; auto r=host(); r.flags=4;
        check(m.Submit(r,0)==Pending,"explicit LAN-lobby initialization option accepted");
        m.Tick(f,1,e.actions());
        check(e.begins==1 && !std::strcmp(e.order,"VSKVAB"),"absence checked before switch and arming; lobby task runs once");
        f.kind=1; m.Tick(f,2,e.actions()); check(e.begins==1,"normal updates cannot repeat lobby creation");
    }
    {
        JoinMachine m; Engine e; Frame f={true,true,false,5,0,0,-1}; auto r=host(); r.flags=8;
        check(m.Submit(r,0)==Pending,"navigation-only request accepted");
        m.Tick(f,1,e.actions());
        check(e.begins==1 && !e.arms && !e.starts && !std::strcmp(e.order,"VSKVB"),"navigation requests LAN activity without arming auto-connect");
        check(uint32_t(m.phase)==5 && m.code==Ok,"navigation has its own completion, not Entered");
        f.kind=1; f.pre_state=3; m.Tick(f,2,e.actions()); m.Cancel();
        check(uint32_t(m.phase)==5 && e.begins==1 && !e.arms,"manual progression cannot cause later arming or fake join completion");
    }
    {
        JoinMachine m; Engine e; e.session_after_switch=true; Frame f={true,true,false,5,0,0,-1}; auto r=host(); r.flags=8;
        m.Submit(r,0); m.Tick(f,1,e.actions());
        check(m.phase==Failed && !e.arms && !e.begins,"navigation refuses a session appearing during switch");
    }
    {
        auto r=host(); r.flags=9; check(!ValidRequest(r),"navigation cannot combine fallback");
        r.flags=12; check(!ValidRequest(r),"navigation cannot combine automatic lobby join");
        r.flags=10; check(!ValidRequest(r),"navigation cannot combine mode2");
    }
    {
        JoinMachine m; Engine e; e.select_result=SelectWait; Frame f={true,true,false,5,0,0,-1}; auto r=host(); r.flags=16;
        check(m.Submit(r,0)==Pending,"stock selection request accepted");
        m.Tick(f,1,e.actions());
        check(e.begins==1 && !e.arms && !e.starts && !std::strcmp(e.order,"VSKVB") && m.phase==Selecting && m.code==Pending,"selection first requests normal host setup, never arms");
        f.kind=1; m.Tick(f,1+kSelectSettleMs-1,e.actions());
        check(!e.selects,"selection waits for host setup to settle");
        m.Tick(f,1+kSelectSettleMs,e.actions());
        check(e.selects==1 && m.phase==Selecting && !std::strcmp(e.received.host,"192.168.1.20"),"no matching discovered entry keeps waiting");
        e.select_result=SelectDispatched; m.Tick(f,2+kSelectSettleMs,e.actions());
        check(e.selects==2 && m.phase==Selected && m.code==Ok,"matching entry is selected once");
        m.Tick(f,3+kSelectSettleMs,e.actions()); m.Cancel();
        check(e.selects==2 && m.phase==Selected && !e.arms && e.begins==1,"completed selection is never repeated or followed by arming");
    }
    {
        JoinMachine m; Engine e; e.select_result=SelectWait; Frame f={true,true,false,5,0,0,-1}; auto r=host(); r.flags=16;
        m.Submit(r,0); m.Tick(f,1,e.actions()); f.kind=1;
        m.Tick(f,1+kSelectWindowMs,e.actions());
        check(m.phase==Failed && m.code==Timeout && !e.selects,"selection window expires without dispatch");
    }
    {
        JoinMachine m; Engine e; e.select_result=SelectRefused; Frame f={true,true,false,5,0,0,-1}; auto r=host(); r.flags=16;
        m.Submit(r,0); m.Tick(f,1,e.actions()); f.kind=1; m.Tick(f,1+kSelectSettleMs,e.actions());
        check(m.phase==Failed && m.code==Failure && e.selects==1,"refused selection fails without retry");
        m.Tick(f,2+kSelectSettleMs,e.actions()); check(e.selects==1,"failed selection is terminal");
    }
    {
        JoinMachine m; Engine e; Frame f={true,true,false,5,0,0,-1}; auto r=host(); r.flags=16;
        m.Submit(r,0); m.Tick(f,1,e.actions()); f.kind=0; m.Tick(f,1+kSelectSettleMs,e.actions());
        check(m.phase==Failed && !e.selects,"LAN loss blocks selection");
    }
    {
        JoinMachine m; Engine e; Frame f={true,true,false,5,0,0,-1}; auto r=host(); r.flags=16;
        m.Submit(r,0); m.Tick(f,1,e.actions()); f.kind=1; f.no_eac=false; m.Tick(f,1+kSelectSettleMs,e.actions());
        check(m.phase==Failed && !e.selects,"protection boundary refuses selection");
    }
    {
        JoinMachine m; Engine e; m.Submit([]{ auto r=host(); r.flags=16; return r; }(),0); m.Tick(lan,1,e.actions());
        check(m.phase==Failed && !e.switches && !e.begins && !e.selects,"selection is never started from the front end");
    }
    {
        auto r=host(); r.flags=17; check(!ValidRequest(r),"selection cannot combine fallback");
        r.flags=18; check(!ValidRequest(r),"selection cannot combine mode2");
        r.flags=20; check(!ValidRequest(r),"selection cannot combine auto-connect lobby");
        r.flags=24; check(!ValidRequest(r),"selection cannot combine navigation-only");
    }
    {
        JoinMachine m; Engine e; e.kind_after_switch=0; Frame f={true,true,false,5,0,0,-1}; auto r=host(); r.flags=4;
        m.Submit(r,0); m.Tick(f,1,e.actions()); check(!e.arms && !e.begins,"unconfirmed LAN never creates a lobby");
    }
    {
        JoinMachine m; Engine e; Frame f={true,true,false,5,0,0,-1}; auto r=host(); r.flags=4;
        m.Submit(r,0); f.no_eac=false; m.Tick(f,1,e.actions()); check(!e.switches && !e.arms && !e.begins,"protection boundary refuses initialization too");
    }
    {
        JoinMachine m; Engine e; e.can_init=false; Frame f={true,true,false,5,0,0,-1}; auto r=host(); r.flags=4;
        m.Submit(r,0); m.Tick(f,1,e.actions()); check(m.phase==Failed && !e.switches && !e.arms && !e.begins,"existing session refuses before any mutation");
    }
    {
        JoinMachine m; Engine e; e.session_after_switch=true; Frame f={true,true,false,5,0,0,-1}; auto r=host(); r.flags=4;
        m.Submit(r,0); m.Tick(f,1,e.actions()); check(m.phase==Failed && e.switches==1 && !e.arms && !e.begins,"session appearing after switch refuses before arming");
    }
    {
        auto r=host(); strcpy_s(r.host,"255.255.255.255");
        check(!ValidRequest(r),"broadcast is not a join destination");
        strcpy_s(r.host,"0.0.0.0"); check(!ValidRequest(r),"unspecified host is not a join destination");
        strcpy_s(r.host,"239.1.2.3"); check(!ValidRequest(r),"multicast is not a join destination");
        strcpy_s(r.host,"192.168.001.2"); check(!ValidRequest(r),"ambiguous octal host is refused");
        strcpy_s(r.host,"127.0.0.1"); check(ValidRequest(r),"same-PC server is allowed");
        r.flags=2; check(!ValidRequest(r),"mode2 requires two GUIDs");
        strcpy_s(r.map,"11111111-2222-3333-4444-555555555555");
        strcpy_s(r.variant,"aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"); check(ValidRequest(r),"explicit mode2 GUIDs accepted");
        r.flags=32; check(!ValidRequest(r),"unknown flags refused");
        r=host(); std::memset(r.host,'9',16); check(!ValidRequest(r),"unterminated IPC host rejected");
    }
    {
        JoinMachine m; Engine e; m.Submit(host(),0); auto f=lan; f.client=false; m.Tick(f,1,e.actions());
        check(m.phase==Failed && !e.switches && !e.arms,"server role cannot join");
    }
    {
        JoinMachine m; Engine e; auto r=host(); r.flags=1; m.Submit(r,0); m.Tick(lan,1,e.actions()); m.Tick(lan,2,e.actions());
        m.Tick(online,4002,e.actions()); check(m.phase==Failed && !e.starts,"LAN loss blocks fallback");
    }
    {
        JoinMachine m; Engine e; Frame f={true,true,false,5,0,0,-1}; auto r=host(); r.flags=16;
        m.Submit(r,0); m.Tick(f,1,e.actions()); f.kind=1; m.Tick(f,1+kSelectSettleMs,e.actions());
        check(m.phase==Selected && m.Submit(r,9000)==Busy,"finished join blocks until reset");
        m.Reset(); check(m.phase==Idle && m.Submit(r,9001)==Pending,"reset allows another join");
        Engine e2; f.kind=0; m.Tick(f,9002,e2.actions()); check(e2.begins==1 && m.phase==Selecting,"second join re-runs host setup");
    }
    std::puts("join-machine tests passed");
}
