#pragma once
// Directory and server address rules, the same as the retired OpenLink app's (netcheck.go).
namespace clientjoin {
namespace netcheck {
// "localhost", or a loopback, private or link-local IPv4/IPv6 address (brackets allowed).
bool LocalHost(const char* host) noexcept;
// A directory address the launcher may use: https://, or http:// only for a directory on this PC
// or the local network (testing), so nobody on the way can rewrite the server list.
bool DirectoryAllowed(const char* url) noexcept;
// Whether a listing's server host may be contacted: a directory on the internet must not send
// players to addresses on their own network (it could use the launcher to probe them); a local
// test directory may. DNS names are allowed (the directory only lists names of trusted hosts).
bool ServerHostAllowed(const char* directory,const char* host) noexcept;
// The message shown for a directory DirectoryAllowed refuses.
extern const char kDirectoryRule[];
}
}
