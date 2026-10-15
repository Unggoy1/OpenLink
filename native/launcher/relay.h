#pragma once
// In-process OpenLink player relay (port of connect.Start: directory
// listing lookup, beacon replay to the local game, UDP forwarding to the
// listed endpoint). It outlives the controller and ends with the game process
// or when the server leaves the directory and stops answering.
#include <cstdint>
#include <cstddef>
namespace clientjoin {
namespace relay {
struct Config {
    char directory[160]={};  // http(s)://host[:port], no path
    char listing[40]={};     // directory listing ID
    char lan_ip[16]={};      // empty: loopback mode (127.0.0.1); else broadcast from this IPv4
};
enum State : uint32_t { Idle,Resolving,Bound,Advertising,Answered,Gone,Failed };
struct Status {
    State state=Idle; uint32_t error=0,adverts=0,beacons=0,up=0,down=0,sessions=0; uint8_t local[4]={};
};
// Ready is called once from the relay thread when beacons are being advertised;
// it receives the local IPv4 text the game will see as the server's address.
using ReadyCallback=void (*)(const char* local_ip);
bool ValidConfig(const Config&) noexcept;
uint32_t Start(const Config&,ReadyCallback) noexcept; // Windows error; one relay per process
Status Snapshot() noexcept;
// Pure helpers, exposed for tests.
size_t Base64Decode(const char* text,size_t length,uint8_t* out,size_t capacity) noexcept; // 0 = invalid
bool FindListing(const char* json,size_t length,const char* id,char* host,size_t host_capacity,uint16_t& port) noexcept;
bool FindListingByEndpoint(const char* json,size_t length,const char* host,uint16_t port,char* id,size_t id_capacity) noexcept;
bool ParseBeacon(const char* json,size_t length,uint8_t* beacon,size_t capacity,size_t& size,int64_t& age_ms) noexcept;
bool IsBallot(const uint8_t* datagram,size_t size) noexcept;
// Playlist votes (vote/vote.go): while a vote runs, OpenLink Server sends each player
// "HICOMM-BALLOT {json}" about once a second through that player's proxy session; a vote is
// "HICOMM-VOTE {"round":N,"choice":i}" sent back on the same session socket (the server keys
// votes by source address). There is no close message: a ballot older than ~4 s means gone.
constexpr size_t kMaxVoteOptions=4;
struct BallotOption { char id[81]; char name[81]; char thumb[80]; };
struct Ballot {
    uint64_t round; uint32_t count; BallotOption options[kMaxVoteOptions];
    int64_t remaining_ms; int32_t counts[kMaxVoteOptions]; int32_t mine; bool closed; int32_t winner; int64_t start_ms;
};
// Parses one ballot datagram (prefix included) and checks it like vote.Ballot.Validate.
bool ParseBallot(const uint8_t* datagram,size_t size,Ballot& out) noexcept;
// Called on the relay thread for every valid ballot of the current session.
void SetBallotCallback(void (*callback)(const Ballot&)) noexcept;
// Sends one datagram to the server on every live session of the current relay (the next relay
// loop pass, within ~100 ms). False when no session is live or the datagram is too large.
bool SendToServer(const char* data,size_t size) noexcept;
// The OpenLink app protocol level this launcher implements (joins through the OpenLink Server
// proxy). Keep it equal to api.AppProtocol in internal/api (a Go test checks); a listing that
// needs a higher level is shown but not joined ("update OpenLink Launcher").
constexpr int32_t kAppProtocol=1;
// Browser rows from GET /v1/servers.
struct Row { char id[40]; char name[96]; char match[96]; char phase[16]; char status[16]; char thumb[96]; char mode[96]; char description[484]; int32_t players; int32_t app_protocol; bool joinable; };
bool ParseServerList(const char* json,size_t length,Row* rows,size_t capacity,size_t& count) noexcept;
uint32_t ListServers(const char* directory,Row* rows,size_t capacity,size_t& count) noexcept; // 0 or Windows error
}
}
