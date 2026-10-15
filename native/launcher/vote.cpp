// Playlist voting state (OpenLink, AGPL-3.0): mirrors the app's BallotView and Vote
// (browser/vote.go) on top of the relay's ballots.
#define WIN32_LEAN_AND_MEAN
#include <windows.h>
#include "vote.h"
#include "overlay.h"
#include <cstdio>
#include <cstring>
namespace clientjoin {
namespace vote {
namespace {
constexpr ULONGLONG kFresh=4000; // the server repeats ballots about once a second
SRWLOCK lock=SRWLOCK_INIT;
relay::Ballot current={}; ULONGLONG received=0; bool have=false;
uint64_t pending_round=0; int pending_choice=-1; char error[96]={};
void OnBallot(const relay::Ballot& b) {
    AcquireSRWLockExclusive(&lock);
    if(!have || b.round!=current.round) { pending_round=0; pending_choice=-1; error[0]=0; }
    if(pending_round==b.round && b.mine==pending_choice) { pending_round=0; pending_choice=-1; } // echoed back
    current=b; received=GetTickCount64(); have=true;
    ReleaseSRWLockExclusive(&lock);
    overlay::Wake();
}
}
void Install() noexcept { relay::SetBallotCallback(&OnBallot); }
bool Current(View& v) noexcept {
    AcquireSRWLockShared(&lock);
    const ULONGLONG age=have ? GetTickCount64()-received : kFresh+1;
    const bool fresh=have && age<=kFresh;
    if(fresh) {
        v.ballot=current;
        const double seconds=double(age)/1000.0;
        v.remaining_s=current.remaining_ms/1000.0-seconds; if(v.remaining_s<0) v.remaining_s=0;
        v.starts_in_s=-1;
        if(current.closed && current.start_ms>=0) { v.starts_in_s=current.start_ms/1000.0-seconds; if(v.starts_in_s<0) v.starts_in_s=0; }
        v.mine=pending_round==current.round && pending_choice>=0 ? pending_choice : current.mine;
        strcpy_s(v.error,error);
    }
    ReleaseSRWLockShared(&lock);
    return fresh;
}
const char* Cast(int choice) noexcept {
    View v={};
    if(!Current(v) || v.ballot.closed || v.remaining_s<=0) return "the vote is closed";
    if(choice<0 || uint32_t(choice)>=v.ballot.count) return "no such choice";
    char datagram[96];
    const int n=sprintf_s(datagram,"HICOMM-VOTE {\"round\":%llu,\"choice\":%d}",static_cast<unsigned long long>(v.ballot.round),choice);
    const bool sent=n>0 && relay::SendToServer(datagram,size_t(n));
    AcquireSRWLockExclusive(&lock);
    if(sent) { pending_round=v.ballot.round; pending_choice=choice; error[0]=0; }
    else strcpy_s(error,"join the server before voting");
    ReleaseSRWLockExclusive(&lock);
    overlay::Wake();
    return sent ? nullptr : "join the server before voting";
}
}
}
