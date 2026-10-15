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
