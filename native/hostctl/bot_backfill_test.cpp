#include "bot_backfill.h"
#include <cstdio>

int main() {
    int failures=0;
    const auto check=[&](bool ok,const char* label) { if(!ok) { ++failures; std::fprintf(stderr,"FAIL: %s\n",label); } };
    using namespace hostctl;
    check(BotsWanted(0,8,8)==0,"no bots without players");
    check(BotsWanted(1,8,8)==7,"one player: seven bots to reach eight");
    check(BotsWanted(3,8,2)==2,"max_bots caps the fill");
    check(BotsWanted(1,24,20)==kMaxBots,"never more than the engine's eight bots");
    check(BotsWanted(8,8,8)==0 && BotsWanted(10,8,8)==0,"a full match needs no bots");
    check(BotsWanted(30,24,8)==0,"fill_to below the player count");
    check(BotDifficultyCode(BotRecruit)==9 && BotDifficultyCode(BotMarine)==6 && BotDifficultyCode(BotOdst)==7 &&
        BotDifficultyCode(BotSpartan)==8 && BotDifficultyCode(9)==6,"difficulty codes");

    check(BotDifficultyFallback(6,1u<<6)==6,"registered difficulty is used");
    check(BotDifficultyFallback(8,(1u<<6)|(1u<<7))==7,"spartan falls back to the nearest registered (odst)");
    check(BotDifficultyFallback(9,1u<<7)==7,"recruit falls back past an unregistered marine");
    check(BotDifficultyFallback(6,(1u<<9)|(1u<<7))==9,"ties go to the easier difficulty");
    check(BotDifficultyFallback(6,0)==0,"no registered difficulty: no bot");

    // FFA: one player, fill to 4: add on a free team.
    BotParticipant ffa[8]={{false,0,0}};
    auto a=DecideBots(ffa,1,4,8,0);
    check(a.kind==BotAction::Add && a.team==-1,"FFA adds on a free team");
    BotParticipant ffa_full[]={{false,0,0},{true,30,11},{true,29,12},{true,28,13}};
    check(DecideBots(ffa_full,4,4,8,0).kind==BotAction::None,"FFA at the target does nothing");
    BotParticipant ffa_join[]={{false,0,0},{false,1,0},{true,30,11},{true,29,12},{true,28,13}};
    a=DecideBots(ffa_join,5,4,8,0);
    check(a.kind==BotAction::Remove && (a.handle==11 || a.handle==12 || a.handle==13),"FFA removes a bot when a player joins");

    // Teams: two players on Eagle, fill to 4: bots go to Cobra.
    BotParticipant two_eagle[]={{false,0,0},{false,0,0}};
    a=DecideBots(two_eagle,2,4,8,2);
    check(a.kind==BotAction::Add && a.team==1,"team mode adds to the smaller team");
    BotParticipant mixed[]={{false,0,0},{false,1,0},{true,0,21}};
    a=DecideBots(mixed,3,4,8,2);
    check(a.kind==BotAction::Add && a.team==1,"ties broken by the smaller team");
    BotParticipant tie[]={{false,0,0},{false,1,0}};
    a=DecideBots(tie,2,4,8,2);
    check(a.kind==BotAction::Add && a.team==0,"equal teams: lowest index");
    // A player joins Cobra: the bot on the larger team leaves.
    BotParticipant joined[]={{false,0,0},{false,1,0},{false,1,0},{true,0,31},{true,1,32}};
    a=DecideBots(joined,5,4,8,2);
    check(a.kind==BotAction::Remove && a.handle==32,"team mode removes from the larger team");
    // A player on team 2 of a two-team setting widens the team range.
    BotParticipant third[]={{false,0,0},{false,1,0},{false,2,0}};
    a=DecideBots(third,3,6,8,2);
    check(a.kind==BotAction::Add && a.team==0,"teams cover every player's team");
    // Observers are not players.
    BotParticipant watch[]={{false,0,0},{false,kObserverTeam,0}};
    check(BotsWanted(1,4,8)==3 && DecideBots(watch,2,2,8,2).kind==BotAction::Add,"observers do not count as players");
    // Everyone left: bots go.
    BotParticipant empty[]={{true,0,41},{true,1,42}};
    check(DecideBots(empty,2,4,8,2).kind==BotAction::Remove,"bots leave an empty match");
    check(DecideBots(nullptr,0,4,8,0).kind==BotAction::None,"nothing to do without participants");

    if(failures) return 1;
    std::puts("bot backfill tests passed");
    return 0;
}
