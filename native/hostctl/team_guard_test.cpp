#include "team_guard.h"
#include <cstdio>
#include <cstring>

namespace {
// Team sizes of an assignment.
void Sizes(unsigned n,const int8_t* want,unsigned teams,unsigned* sizes) {
    std::memset(sizes,0,sizeof(unsigned)*hostctl::kMaxTeams);
    for(unsigned i=0;i<n;++i) if(want[i]>=0 && unsigned(want[i])<teams) ++sizes[want[i]];
}
bool Even(unsigned n,const int8_t* want,unsigned teams) {
    unsigned sizes[hostctl::kMaxTeams]; Sizes(n,want,teams,sizes);
    unsigned lo=n,hi=0,sum=0;
    for(unsigned t=0;t<teams;++t) { lo=sizes[t]<lo ? sizes[t] : lo; hi=sizes[t]>hi ? sizes[t] : hi; sum+=sizes[t]; }
    return sum==n && hi-lo<=1;
}
}

int main() {
    int failures=0;
    const auto check=[&](bool ok,const char* label) { if(!ok) { ++failures; std::fprintf(stderr,"FAIL: %s\n",label); } };
    using namespace hostctl;
    int8_t want=0;
    check(!FfaTeamFix(0,-1,0,want) && want==0,"peer 0 on team 0 stays");
    check(FfaTeamFix(1,-1,0,want) && want==1,"peer 1 sharing team 0 moves to team 1");
    check(FfaTeamFix(3,2,2,want) && want==3,"a requested team is ignored in FFA");
    check(FfaTeamFix(2,-1,-1,want) && want==2,"an unassigned peer gets its own team");
    check(FfaTeamFix(1,kObserverTeam,1,want) && want==kObserverTeam,"observer request is honoured");
    check(!FfaTeamFix(1,-1,kObserverTeam,want),"an assigned observer stays");
    check(IsObserver(kObserverTeam,0,0) && IsObserver(-1,kObserverTeam,0) && IsObserver(-1,0,kObserverTeam) && !IsObserver(2,2,2),
        "observer detection");

    check(TeamCount(10,0,0)==2,"default is two teams");
    check(TeamCount(10,4,0)==4,"playlist count");
    check(TeamCount(10,0,3)==4,"teams of 3 for 10 players -> 4 teams");
    check(TeamCount(8,0,4)==2 && TeamCount(9,0,4)==3,"teams of 4");
    check(TeamCount(4,0,4)==2 && TeamCount(3,0,4)==2 && TeamCount(1,0,4)==2,"never fewer than two teams");
    check(TeamCount(40,0,1)==8 && TeamCount(5,12,0)==8,"at most 8 teams");
    check(TeamCount(0,0,3)==2,"no players still means two teams");

    {   // one player on Cobra keeps Cobra
        const int8_t current[]={1}; int8_t out[1]={};
        AssignTeams(1,current,2,TeamModeEven,0,out); check(out[0]==1,"even keeps a lone Cobra player");
    }
    {   // a carried-over Hades pick is reset to Eagle
        const int8_t current[]={2}; int8_t out[1]={};
        AssignTeams(1,current,2,TeamModeEven,0,out); check(out[0]==0,"even resets Hades to Eagle in a 2-team mode");
    }
    {   // 3 on Eagle, 1 on Cobra: one Eagle player (the last) moves
        const int8_t current[]={0,0,0,1}; int8_t out[4]={};
        AssignTeams(4,current,2,TeamModeEven,0,out);
        check(out[0]==0 && out[1]==0 && out[2]==1 && out[3]==1,"even moves only the excess player");
    }
    {   // friends who picked the same team stay together when there is room
        const int8_t current[]={1,1,-1,-1}; int8_t out[4]={};
        AssignTeams(4,current,2,TeamModeEven,0,out);
        check(out[0]==1 && out[1]==1 && out[2]==0 && out[3]==0,"even keeps a pair together");
    }
    {   // 10 players, teams of 3 -> 4 teams of 3,3,2,2
        int8_t current[10]; std::memset(current,0xff,sizeof(current)); int8_t out[10]={};
        const unsigned teams=TeamCount(10,0,3);
        AssignTeams(10,current,teams,TeamModeEven,0,out);
        unsigned sizes[kMaxTeams]; Sizes(10,out,teams,sizes);
        check(teams==4 && sizes[0]==3 && sizes[1]==3 && sizes[2]==2 && sizes[3]==2,"10 players in teams of 3 -> 3,3,2,2");
    }
    {   // shuffle: always even, and different seeds give different deals
        int8_t current[8]; std::memset(current,0,sizeof(current)); int8_t a[8]={},b[8]={};
        AssignTeams(8,current,2,TeamModeShuffle,1,a);
        AssignTeams(8,current,2,TeamModeShuffle,99,b);
        check(Even(8,a,2) && Even(8,b,2),"shuffle is even");
        check(std::memcmp(a,b,sizeof(a))!=0,"shuffle depends on the seed");
        int8_t c[7]={}; AssignTeams(7,current,3,TeamModeShuffle,5,c); check(Even(7,c,3),"shuffle into 3 teams is even");
    }
    std::printf("team guard checks: failures=%d\n",failures);
    return failures ? 1 : 0;
}
