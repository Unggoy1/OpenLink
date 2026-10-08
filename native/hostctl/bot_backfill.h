#pragma once
#include <cstdint>
#include "team_guard.h"

namespace hostctl {
// Bot backfill (FN029): the server tops a match up with bots to `fill_to`
// players and removes a bot each time a player joins. The engine allows at
// most 8 bots while Bot_EnforceCountLimit is on (the default, 142c262ac) and
// 31 players plus bots.
constexpr unsigned kMaxBots=8;
constexpr unsigned kMaxParticipants=31;
constexpr unsigned kMaxBotFill=24; // largest fill_to accepted (Big Team Battle size)

// BackendBotPolicy flags and difficulties.
enum BotPolicyFlag : uint32_t { BotBackfill=1 };
enum BotDifficulty : uint32_t { BotRecruit=0,BotMarine=1,BotOdst=2,BotSpartan=3 };
// Engine difficulty codes for BotDifficulty (tables 143ce4b08/143cef528 hold
// 9,6,7,8; 6 is what Lua Bot_Add uses; the names are inferred).
inline uint16_t BotDifficultyCode(uint32_t difficulty) noexcept {
    static const uint16_t codes[4]={9,6,7,8};
    return difficulty<4 ? codes[difficulty] : uint16_t(6);
}

// The difficulty code to create a bot with: `code` if its bit is set in the
// registered mask (bit i = code i), else the registered code nearest in the
// easy-to-hard order 9,6,7,8 (ties: the easier one), else 0 (none registered).
inline uint16_t BotDifficultyFallback(uint16_t code,uint16_t mask) noexcept {
    static const uint16_t order[4]={9,6,7,8};
    if(code<16 && (mask>>code)&1) return code;
    int at=1; for(int i=0;i<4;++i) if(order[i]==code) at=i;
    for(int distance=1;distance<4;++distance)
        for(int side=-1;side<=1;side+=2) {
            const int i=at+side*distance;
            if(i>=0 && i<4 && (mask>>order[i])&1) return order[i];
        }
    return 0;
}

// Bots wanted with `humans` players: enough to reach fill_to, at most max_bots
// (and kMaxBots) and within the participant limit; none without players.
inline unsigned BotsWanted(unsigned humans,unsigned fill_to,unsigned max_bots) noexcept {
    if(!humans || humans>=fill_to || humans>=kMaxParticipants) return 0;
    unsigned want=fill_to-humans;
    if(max_bots>kMaxBots) max_bots=kMaxBots;
    if(want>max_bots) want=max_bots;
    if(want>kMaxParticipants-humans) want=kMaxParticipants-humans;
    return want;
}

// One match participant as the bot tick sees it (participant +0x538/+0x53c/+0x285).
struct BotParticipant {
    bool bot;        // has a bot handle (+0x53c != -1)
    int8_t team;     // +0x285 ^ 0x9e; kObserverTeam for observers
    uint32_t handle; // bot handle, for removal
};
struct BotAction {
    enum Kind : uint8_t { None,Add,Remove } kind;
    int8_t team;     // Add: team for the new bot, -1 = a free FFA team (the engine picks it)
    uint32_t handle; // Remove: the bot to remove
};

// Decides at most one change for n participants (n <= 32). teams is 0 for a
// free-for-all mode, else the match's team count (2-8; raised to cover any
// team a player is on). A new bot joins the team with the fewest participants
// (lowest index on ties); a removed bot comes from the largest team that has
// one. Observers are neither players nor counted on a team.
inline BotAction DecideBots(const BotParticipant* p,unsigned n,unsigned fill_to,unsigned max_bots,unsigned teams) noexcept {
    BotAction action={BotAction::None,-1,0};
    unsigned humans=0,bots=0,count[kMaxTeams]={};
    if(teams) {
        if(teams<2) teams=2;
        for(unsigned i=0;i<n;++i)
            if(!p[i].bot && p[i].team>=0 && p[i].team!=kObserverTeam && unsigned(p[i].team)>=teams && unsigned(p[i].team)<kMaxTeams)
                teams=unsigned(p[i].team)+1;
        if(teams>kMaxTeams) teams=kMaxTeams;
    }
    for(unsigned i=0;i<n;++i) {
        if(p[i].team==kObserverTeam && !p[i].bot) continue;
        if(p[i].bot) ++bots; else ++humans;
        if(teams && p[i].team>=0 && unsigned(p[i].team)<teams) ++count[p[i].team];
    }
    const unsigned want=BotsWanted(humans,fill_to,max_bots);
    if(bots<want) {
        action.kind=BotAction::Add;
        if(teams) {
            unsigned best=0;
            for(unsigned t=1;t<teams;++t) if(count[t]<count[best]) best=t;
            action.team=int8_t(best);
        }
    } else if(bots>want) {
        int chosen=-1; unsigned size=0;
        for(unsigned i=0;i<n;++i) {
            if(!p[i].bot) continue;
            const unsigned s=teams && p[i].team>=0 && unsigned(p[i].team)<teams ? count[p[i].team] : 0;
            if(chosen<0 || s>=size) { chosen=int(i); size=s; }
        }
        if(chosen>=0) { action.kind=BotAction::Remove; action.team=p[chosen].team; action.handle=p[chosen].handle; }
    }
    return action;
}
}
