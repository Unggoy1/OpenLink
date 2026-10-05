#pragma once
#include <windows.h>
#include <cstdint>

struct HostControlConfig {
    uint32_t size;
    uint32_t version; // 1=transport only, 2=explicit build-pinned LAN backend opt-in
    uint16_t port;
    uint16_t reserved;
    uint32_t controller_pid;
    uint8_t token[32];
};
static_assert(sizeof(HostControlConfig)==48,"host-control configuration ABI");

using HostControlStart = DWORD (WINAPI*)(const HostControlConfig*);
using HostControlStop = DWORD (WINAPI*)();
using HostControlWait = DWORD (WINAPI*)(DWORD);
