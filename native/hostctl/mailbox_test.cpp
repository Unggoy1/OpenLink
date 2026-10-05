#define WIN32_LEAN_AND_MEAN
#include <windows.h>
#include "engine_mailbox.h"
#include <cstdio>
#include <cstring>
#include <stdexcept>

namespace {
int failures=0,calls=0; bool after_throw=false,uninitialized=false;
HANDLE entered=nullptr,release_setter=nullptr;
struct Provider { uintptr_t table=123; hostctl::ContentArray data={}; } provider;
void check(bool ok,const char* message) { if(!ok) { ++failures; std::fprintf(stderr,"FAIL %s\n",message); } }
uint8_t authority(void*) { return 1; }
const hostctl::ContentArray* current(void*) { return uninitialized ? nullptr : &provider.data; }
const hostctl::ContentArray* requested(void*) { return nullptr; }
uint8_t set(void*,const hostctl::ContentArray* next) {
    ++calls; provider.data=*next; uninitialized=false;
    if(entered) { SetEvent(entered); if(WaitForSingleObject(release_setter,5000)!=WAIT_OBJECT_0) throw std::runtime_error("fixture timeout"); }
    if(after_throw) throw std::runtime_error("notification"); return 1;
}
struct ThreadFrame { hostctl::CommandMailbox* box; hostctl::EngineFrame frame; };
DWORD WINAPI tick_thread(void* argument) { auto work=static_cast<ThreadFrame*>(argument); work->frame.owner_thread=GetCurrentThreadId(); work->box->Tick(work->frame); return 0; }
hostctl::ContentSelection selection() {
    hostctl::ContentSelection s={}; s.mode.type=6;
    for(unsigned i=0;i<16;++i) { s.map.asset[i]=uint8_t(i+1); s.map.version[i]=uint8_t(i+2); s.mode.asset[i]=uint8_t(i+3); s.mode.version[i]=uint8_t(i+4); } return s;
}
}
int main() {
    hostctl::CommandMailbox box;
    const auto desired=selection();
    auto denied=box.Submit(desired); check(denied==0,"disabled mailbox refuses commands");
    box.Enable(); uint64_t ticket=box.Submit(desired); check(ticket!=0,"enabled command accepted");
    check(box.Submit(desired)==0,"one command bound");
    provider.data.entries[0].map.type=2; // fixture only, no native map enum assertion
    provider.data.entries[0].mode.type=10;
    hostctl::EngineFrame frame={hostctl::GateBuild|hostctl::GateRole|hostctl::GateDispatch,
        hostctl::HostInGame,GetCurrentThreadId(),&provider,123,{authority,current,set,requested}};
    box.Tick(frame); check(calls==0 && box.Status().code==hostctl::CodePending,"no in-game selection");
    check(box.Cancel(ticket),"undispatched cancellation");
    frame.state=hostctl::HostPreGame; box.Tick(frame); check(calls==0,"cancelled command cannot execute later");
    ticket=box.Submit(desired); box.Tick(frame);
    check(box.Submit(desired)==0,"completed result retained until waiter consumes it");
    auto report=box.Wait(ticket,0); check(report.code==hostctl::CodeSelected && report.generation==ticket,"selection result correlated");
    check(report.observed.map.type==2 && report.observed.mode.type==6,"map type preserved mode path explicit");
    check(report.gates&hostctl::GateSelection,"selection gate"); check(!(report.gates&hostctl::GateContent),"no content claim");
    check(calls==1,"one actual submission");
    auto second=box.Submit(desired); check(second>ticket,"monotonic command ticket");
    after_throw=true; box.Tick(frame); check(box.Wait(second,0).code==hostctl::CodeFailed,"notification failure is failed"); after_throw=false;
    ticket=box.Submit(desired); box.Stop(); box.Tick(frame); check(box.Wait(ticket,0).code==hostctl::CodeFailed,"stop aborts pending command");
    check(box.Submit(desired)==0,"stopped mailbox refuses command");
    uint16_t reason=99; auto invalid=desired; invalid.mode.type=11;
    check(box.Submit(invalid,&reason)==0 && reason==hostctl::CodeInvalid,"invalid mode is not busy");
    reason=99; check(box.Submit(desired,&reason)==0 && reason==hostctl::CodePending,"disabled is not busy");
    hostctl::CommandMailbox concurrent; concurrent.Enable(); ticket=concurrent.Submit(desired);
    entered=CreateEventW(nullptr,TRUE,FALSE,nullptr); release_setter=CreateEventW(nullptr,TRUE,FALSE,nullptr);
    if(!entered || !release_setter) return 9;
    ThreadFrame work={&concurrent,frame}; HANDLE thread=CreateThread(nullptr,0,tick_thread,&work,0,nullptr);
    if(!thread) return 9;
    check(WaitForSingleObject(entered,5000)==WAIT_OBJECT_0,"owned setter in flight");
    check(concurrent.Wait(ticket,0).code==hostctl::CodePending,"in-flight timeout stays pending");
    check(!concurrent.Cancel(ticket) && !concurrent.Submit(desired),"in-flight work cannot be cancelled or replaced");
    concurrent.Stop(); concurrent.Enable(); check(!concurrent.Submit(desired),"in-flight stop cannot re-enable mailbox");
    SetEvent(release_setter); check(WaitForSingleObject(thread,5000)==WAIT_OBJECT_0,"owned callback drained");
    CloseHandle(thread); CloseHandle(entered); CloseHandle(release_setter); entered=nullptr; release_setter=nullptr;
    check(concurrent.Wait(ticket,0).code==hostctl::CodeFailed,"stop prevents late selection acknowledgement");
    hostctl::CommandMailbox abandoned; abandoned.Enable(); ticket=abandoned.Submit(desired);
    entered=CreateEventW(nullptr,TRUE,FALSE,nullptr); release_setter=CreateEventW(nullptr,TRUE,FALSE,nullptr);
    if(!entered || !release_setter) return 9;
    ThreadFrame abandoned_work={&abandoned,frame}; thread=CreateThread(nullptr,0,tick_thread,&abandoned_work,0,nullptr); if(!thread) return 9;
    check(WaitForSingleObject(entered,5000)==WAIT_OBJECT_0,"second owned setter in flight");
    check(abandoned.Wait(ticket,0).code==hostctl::CodePending,"public waiter may depart pending");
    SetEvent(release_setter); check(WaitForSingleObject(thread,5000)==WAIT_OBJECT_0,"late callback finished");
    CloseHandle(thread); CloseHandle(entered); CloseHandle(release_setter); entered=nullptr; release_setter=nullptr;
    check(abandoned.Submit(desired)!=0,"late receipt cannot permanently block later command");
    abandoned.Stop();
    hostctl::CommandMailbox timeout; timeout.Enable(); ticket=timeout.Submit(desired);
    check(timeout.Wait(ticket,1).code==hostctl::CodeFailed,"timeout cancels undispatched work");
    const int before_timeout=calls; timeout.Tick(frame); check(calls==before_timeout,"timeout cannot mutate later");
    hostctl::CommandMailbox initial; initial.Enable(); provider.data={}; uninitialized=true;
    auto first=desired; first.map.type=2;
    ticket=initial.Submit(first); initial.Tick(frame);
    check(initial.Wait(ticket,0).code==hostctl::CodeSelected && !uninitialized,"explicit first selection initializes absent current");
    hostctl::CommandMailbox ordinary; ordinary.Enable(); uninitialized=true;
    ticket=ordinary.Submit(desired); const auto before_pending=calls; ordinary.Tick(frame);
    check(ordinary.Wait(ticket,0).code==hostctl::CodePending && calls==before_pending,"ordinary selection cannot initialize"); uninitialized=false;
    hostctl::CommandMailbox wrong; wrong.Enable(); ticket=wrong.Submit(desired);
    frame.owner_thread=GetCurrentThreadId()+1; wrong.Tick(frame); check(wrong.Wait(ticket,0).code==hostctl::CodeFailed,"actual thread mismatch fails");
    std::printf("mailbox tests failures=%d\n",failures); return failures ? 1 : 0;
}
