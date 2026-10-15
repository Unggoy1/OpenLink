#pragma once
// Playlist voting for the OpenLink Launcher: the current ballot from the relay (see relay.h),
// as the retired app's BallotView (its vote.go), and casting a vote.
#include "relay.h"
namespace clientjoin {
namespace vote {
struct View {
    relay::Ballot ballot;
    double remaining_s;   // until the vote closes
    double starts_in_s;   // once closed: until the match starts, -1 unknown
    int mine;             // the player's choice (a vote just sent counts), -1 if none
    char error[96];       // why the last vote was not sent, or ""
};
// Receives ballots from the relay from now on.
void Install() noexcept;
// The current vote: false when there is none (no ballot for ~4 s).
bool Current(View& out) noexcept;
// The player left the server: forget the ballot and ignore ballots until Accept() (the next join).
void Drop() noexcept;
void Accept() noexcept;
// Votes for option `choice` of the current round. nullptr when sent, else why not (also kept
// in View::error).
const char* Cast(int choice) noexcept;
}
}
