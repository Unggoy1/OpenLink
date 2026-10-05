#define WIN32_LEAN_AND_MEAN
#include "engine_mailbox.h"
#include <cstring>

namespace hostctl {
namespace {
bool has_ids(const ContentSelection& value) {
    const uint8_t* fields[]={value.map.asset,value.map.version,value.mode.asset,value.mode.version};
    for(auto field:fields) { uint8_t any=0; for(unsigned i=0;i<16;++i) any=uint8_t(any|field[i]); if(!any) return false; }
    return true;
}
}
CommandMailbox::CommandMailbox() noexcept { status_.code=CodePending; status_.state=StateUnknown; }
void CommandMailbox::Enable() noexcept {
    AcquireSRWLockExclusive(&lock_);
    if(!pending_ && !applying_) { enabled_=true; status_.flags=FlagInstalled; status_.code=CodePending; }
    ReleaseSRWLockExclusive(&lock_);
}
void CommandMailbox::Complete(uint16_t code) noexcept {
    pending_=false; applying_=false; status_.flags&=~uint32_t(FlagCommandPending);
    completed_ticket_=ticket_; completed_=status_; completed_.code=code; uncollected_=!waiter_departed_;
    if(code!=CodeSelected) completed_.gates&=~uint32_t(GateSelection);
    WakeAllConditionVariable(&changed_);
}
void CommandMailbox::Stop() noexcept {
    AcquireSRWLockExclusive(&lock_); enabled_=false;
    status_.flags&=~uint32_t(FlagInstalled|FlagDesiredActive); status_.gates&=~uint32_t(GateSelection);
    if(pending_ && !applying_) Complete(CodeFailed);
    WakeAllConditionVariable(&changed_); ReleaseSRWLockExclusive(&lock_);
}
uint64_t CommandMailbox::Submit(const ContentSelection& selection,uint16_t* rejection) noexcept {
    if(rejection) *rejection=CodeInvalid;
    if(!has_ids(selection) || (selection.mode.type!=0 && selection.mode.type!=6 && selection.mode.type!=10) ||
        (selection.map.type!=0 && (selection.map.type!=2 || selection.mode.type!=6))) return 0;
    AcquireSRWLockExclusive(&lock_); uint64_t result=0;
    if(rejection) *rejection=!enabled_ ? CodePending : sequence_==UINT64_MAX ? CodeFailed : CodeBusy;
    if(enabled_ && !pending_ && !applying_ && !uncollected_ && sequence_!=UINT64_MAX) {
        ticket_=++sequence_; result=ticket_; requested_=selection; pending_=true; waiter_departed_=false;
        status_.flags|=FlagCommandPending; status_.gates&=~uint32_t(GateSelection);
    }
    ReleaseSRWLockExclusive(&lock_); return result;
}
bool CommandMailbox::Cancel(uint64_t ticket) noexcept {
    AcquireSRWLockExclusive(&lock_); bool cancelled=pending_ && !applying_ && ticket_==ticket;
    if(cancelled) { Complete(CodeFailed); uncollected_=false; } ReleaseSRWLockExclusive(&lock_); return cancelled;
}
BackendReport CommandMailbox::Wait(uint64_t ticket,uint32_t milliseconds) noexcept {
    if(milliseconds>2000) milliseconds=2000;
    const ULONGLONG deadline=GetTickCount64()+milliseconds;
    AcquireSRWLockExclusive(&lock_);
    while(ticket && ticket_==ticket && (pending_ || applying_) && enabled_) {
        const auto now=GetTickCount64(); if(now>=deadline) break;
        if(!SleepConditionVariableSRW(&changed_,&lock_,DWORD(deadline-now),0)) break;
    }
    BackendReport result=status_; result.code=CodePending; result.gates&=~uint32_t(GateSelection);
    if(ticket && completed_ticket_==ticket) { result=completed_; uncollected_=false; }
    else if(!ticket || ticket_!=ticket || !enabled_) result.code=CodeFailed;
    // Only an undispatched command is cancellable. In-flight native calls may
    // already have mutated the provider and remain visible through status.
    else if(pending_ && !applying_) { Complete(CodeFailed); result=completed_; uncollected_=false; }
    else if(applying_) waiter_departed_=true; // no public ticket survives a Pending return
    ReleaseSRWLockExclusive(&lock_); return result;
}
BackendReport CommandMailbox::Status() noexcept {
    AcquireSRWLockShared(&lock_); auto result=status_; result.code=CodePending; result.gates&=~uint32_t(GateSelection);
    ReleaseSRWLockShared(&lock_); return result;
}
void CommandMailbox::Tick(const EngineFrame& frame) noexcept {
    AcquireSRWLockExclusive(&lock_);
    if(!enabled_) { ReleaseSRWLockExclusive(&lock_); return; }
    status_.state=frame.state; ++status_.ticks; status_.flags|=FlagTicking;
    if(frame.state==HostInGame && last_state_!=HostInGame) ++status_.matches;
    last_state_=frame.state;
    status_.gates=frame.gates & uint32_t(GateBuild|GateRole|GateDispatch);
    if(!frame.owner_thread || frame.owner_thread!=GetCurrentThreadId()) {
        status_.flags|=FlagFaulted; enabled_=false;
        if(pending_ && !applying_) Complete(CodeFailed);
        ReleaseSRWLockExclusive(&lock_); return;
    }
    const uint32_t required=GateBuild|GateRole|GateDispatch;
    const bool lobby=(frame.state==HostSetup || frame.state==HostPreGame) && (status_.gates&required)==required;
    if(lobby) status_.gates|=GateLobby;
    if(!pending_ || applying_ || !lobby || !frame.provider || !frame.provider_table || !frame.functions.Current) {
        ReleaseSRWLockExclusive(&lock_); return;
    }
    applying_=true; pending_=false; const auto command=requested_;
    ReleaseSRWLockExclusive(&lock_);
    auto desired=command; ProviderResult outcome=ProviderResult::Failed;
    ContentSelection observed={};
    try {
        uintptr_t table=0; std::memcpy(&table,frame.provider,sizeof(table));
        if(table==frame.provider_table) {
            const auto current=frame.functions.Current(frame.provider);
            const bool initialize=command.map.type==2 && command.mode.type==6;
            if(current || initialize) {
                if(!desired.map.type) desired.map.type=current->entries[0].map.type;
                if(!desired.mode.type) desired.mode.type=current->entries[0].mode.type;
                if(desired.map.type && (desired.mode.type==6 || desired.mode.type==10)) {
                    outcome=SelectLanContent(frame.provider,frame.provider_table,frame.owner_thread,desired,frame.functions,initialize);
                    if(outcome==ProviderResult::Selected) observed=desired;
                } else outcome=ProviderResult::Invalid;
            } else outcome=ProviderResult::Pending;
        } else outcome=ProviderResult::Invalid;
    } catch(...) { outcome=ProviderResult::Failed; }
    AcquireSRWLockExclusive(&lock_);
    status_.detail=uint32_t(outcome);
    if(enabled_ && outcome==ProviderResult::Selected) {
        status_.generation=ticket_; status_.observed=observed;
        status_.flags|=FlagProviderReady|FlagDesiredActive; status_.gates|=GateSelection;
        Complete(CodeSelected);
    } else Complete(outcome==ProviderResult::Pending ? CodePending : CodeFailed);
    ReleaseSRWLockExclusive(&lock_);
}
}
