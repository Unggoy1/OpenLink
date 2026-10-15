// Loads the OpenLink Launcher DLL into this test process (not the game) and checks that
// HiOpenLinkStart refuses before installing anything, and that no other entry point exists.
#define WIN32_LEAN_AND_MEAN
#include <windows.h>
#include "boundary.h"
#include <cstdio>
int wmain(int argc,wchar_t** argv) {
    if(argc!=2) return 2;
    HMODULE dll=LoadLibraryW(argv[1]); if(!dll) { std::fprintf(stderr,"LoadLibrary: %lu\n",GetLastError()); return 1; }
    using OpenLinkStart=DWORD (WINAPI*)(void*);
    auto start=reinterpret_cast<OpenLinkStart>(GetProcAddress(dll,"HiOpenLinkStart"));
    if(!start) return 3;
    if(GetProcAddress(dll,"HiHostControlStart") || GetProcAddress(dll,"HiHostControlStop") || GetProcAddress(dll,"HiHostControlWait")) return 4;
    const DWORD boundary=clientjoin::CheckNoEac(GetCurrentProcessId());
    const DWORD refused=start(nullptr);
    if(refused!=(boundary ? boundary : ERROR_BAD_EXE_FORMAT)) { std::fprintf(stderr,"non-game start accepted or wrong refusal: %lu\n",refused); return 5; }
    if(start(nullptr)!=ERROR_ALREADY_EXISTS) return 6; // one start per process, even after a refusal
    FreeLibrary(dll);
    std::puts("DLL entry point present; a non-game process is refused before any hook or network use");
    return 0;
}
