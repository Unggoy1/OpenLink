#include "team_guard.h"
#include <cstdio>

int main() {
    int failures=0;
    const auto check=[&](bool ok,const char* label) { if(!ok) { ++failures; std::fprintf(stderr,"FAIL: %s\n",label); } };
    int8_t want=0;
    check(!hostctl::FfaTeamFix(0,-1,0,want) && want==0,"peer 0 on team 0 stays");
    check(hostctl::FfaTeamFix(1,-1,0,want) && want==1,"peer 1 sharing team 0 moves to team 1");
    check(hostctl::FfaTeamFix(3,2,2,want) && want==3,"a requested team is ignored in FFA");
    check(hostctl::FfaTeamFix(2,-1,-1,want) && want==2,"an unassigned peer gets its own team");
    check(hostctl::FfaTeamFix(1,hostctl::kObserverTeam,1,want) && want==hostctl::kObserverTeam,"observer request is honoured");
    check(!hostctl::FfaTeamFix(1,-1,hostctl::kObserverTeam,want),"an assigned observer stays");
    check(hostctl::IsObserver(hostctl::kObserverTeam,0,0) && hostctl::IsObserver(-1,hostctl::kObserverTeam,0) &&
        hostctl::IsObserver(-1,0,hostctl::kObserverTeam) && !hostctl::IsObserver(2,2,2),"observer detection");
    check(hostctl::BalancedTeam(0)==0 && hostctl::BalancedTeam(1)==1 && hostctl::BalancedTeam(2)==0 && hostctl::BalancedTeam(5)==1,
        "balanced teams alternate Eagle/Cobra");
    std::printf("team guard checks: failures=%d\n",failures);
    return failures ? 1 : 0;
}
