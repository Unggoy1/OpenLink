#pragma once
#include <cstdint>

namespace hostctl {
constexpr int8_t kObserverTeam=0x1f;
constexpr unsigned kMaxTeams=8;      // custom games allow up to 8 teams (plus observers)
constexpr unsigned kMaxTeamSize=32;  // largest session
// The game's free-for-all team rule (142ebfcbc with teams disabled): each peer
// is on its own team, its peer index, or on the observer team if it asked for
// it. An already assigned observer stays. Returns true with want set when the
// peer's assigned team must change.
inline bool FfaTeamFix(unsigned peer,int8_t requested,int8_t assigned,int8_t& want) noexcept {
    want=requested==kObserverTeam ? kObserverTeam : int8_t(peer);
    return assigned!=want && assigned!=kObserverTeam;
}
inline bool IsObserver(int8_t requested,int8_t assigned,int8_t selected) noexcept {
    return requested==kObserverTeam || assigned==kObserverTeam || selected==kObserverTeam;
}
// BackendTeamPolicy flags and balance modes.
enum TeamPolicyFlag : uint32_t { TeamGuardFfa=1,TeamBalance=2 };
enum TeamMode : uint32_t { TeamModeEven=0,TeamModeShuffle=1 };

// Number of teams for a team-mode match with `players` non-observers: the
// playlist's team count if given, else enough teams of `size` for everyone,
// else 2 (Eagle and Cobra). Always 1-8.
inline unsigned TeamCount(unsigned players,unsigned count,unsigned size) noexcept {
    unsigned teams=2;
    if(count) teams=count;
    else if(size) teams=(players+size-1)/size;
    return teams<1 ? 1 : teams>kMaxTeams ? kMaxTeams : teams;
}

// Assigns n players (in peer order, current = their current team, -1 none) to
// `teams` teams whose sizes differ by at most one; want[i] receives each team.
// Even keeps players on a current team that is in range while it has room (the
// teams that already have the most players get the larger sizes) and moves only
// the rest, filling teams in team order. Shuffle deals a random permutation
// round-robin. n <= kMaxTeamSize.
inline void AssignTeams(unsigned n,const int8_t* current,unsigned teams,uint32_t mode,uint64_t seed,int8_t* want) noexcept {
    if(!n || !teams) return;
    if(teams>kMaxTeams) teams=kMaxTeams;
    if(n>kMaxTeamSize) n=kMaxTeamSize;
    if(mode==TeamModeShuffle) {
        unsigned order[kMaxTeamSize];
        for(unsigned i=0;i<n;++i) order[i]=i;
        uint64_t s=seed ? seed : 0x9e3779b97f4a7c15ull;
        for(unsigned i=n-1;i>0;--i) {
            s^=s<<13; s^=s>>7; s^=s<<17; // xorshift64
            const unsigned j=unsigned(s%(i+1)); const unsigned t=order[i]; order[i]=order[j]; order[j]=t;
        }
        for(unsigned k=0;k<n;++k) want[order[k]]=int8_t(k%teams);
        return;
    }
    unsigned have[kMaxTeams]={},target[kMaxTeams],kept[kMaxTeams]={};
    for(unsigned i=0;i<n;++i) if(current[i]>=0 && unsigned(current[i])<teams) ++have[current[i]];
    const unsigned base=n/teams; unsigned extra=n%teams;
    for(unsigned t=0;t<teams;++t) target[t]=base;
    bool larger[kMaxTeams]={};
    while(extra--) { // the fullest teams (lowest index on ties) take the larger size
        unsigned best=kMaxTeams;
        for(unsigned t=0;t<teams;++t) if(!larger[t] && (best==kMaxTeams || have[t]>have[best])) best=t;
        larger[best]=true; ++target[best];
    }
    bool placed[kMaxTeamSize]={};
    for(unsigned i=0;i<n;++i) {
        const int8_t c=current[i];
        if(c>=0 && unsigned(c)<teams && kept[c]<target[c]) { want[i]=c; ++kept[c]; placed[i]=true; }
    }
    unsigned t=0;
    for(unsigned i=0;i<n;++i) {
        if(placed[i]) continue;
        while(kept[t]>=target[t]) ++t;
        want[i]=int8_t(t); ++kept[t];
    }
}
}
