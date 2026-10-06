#pragma once
#include <cstdint>

namespace hostctl {
constexpr int8_t kObserverTeam=0x1f;
// The game's free-for-all team rule (142ebfcbc with teams disabled): each peer
// is on its own team, its peer index, or on the observer team if it asked for
// it. An already assigned observer stays. Returns true with want set when the
// peer's assigned team must change.
inline bool FfaTeamFix(unsigned peer,int8_t requested,int8_t assigned,int8_t& want) noexcept {
    want=requested==kObserverTeam ? kObserverTeam : int8_t(peer);
    return assigned!=want && assigned!=kObserverTeam;
}
// Team balance: observers keep their team; the others, in peer order, alternate
// between the first two teams (Eagle 0, Cobra 1).
inline bool IsObserver(int8_t requested,int8_t assigned,int8_t selected) noexcept {
    return requested==kObserverTeam || assigned==kObserverTeam || selected==kObserverTeam;
}
inline int8_t BalancedTeam(unsigned ordinal) noexcept { return int8_t(ordinal%2); }
// BackendTeamPolicy flags.
enum TeamPolicyFlag : uint32_t { TeamGuardFfa=1,TeamBalance=2 };
}
