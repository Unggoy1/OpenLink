#define WIN32_LEAN_AND_MEAN
#include <windows.h>
#include "game_b002.h"
#include <cstdio>
int main() {
    const auto result=hostctl::InstallGameBackend();
    if(result!=ERROR_NOT_SUPPORTED) { std::fprintf(stderr,"test-owned exe not rejected: %lu\n",static_cast<unsigned long>(result)); return 1; }
    const auto status=hostctl::BackendStatus();
    if(status.gates || status.generation || status.code==hostctl::CodeSelected || status.code==hostctl::CodeApplied) return 2;
    if(hostctl::StopGameBackend()!=ERROR_SUCCESS) return 3;
    std::printf("owned executable rejected before hook\n"); return 0;
}
