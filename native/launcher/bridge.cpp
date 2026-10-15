// OpenLink Launcher DLL entry point (OpenLink, AGPL-3.0). openlink-launcher.exe loads this
// DLL into a Halo Infinite player client that runs without anti-cheat and calls
// HiOpenLinkStart once; OpenLink then stays active for the life of that game process.
#define WIN32_LEAN_AND_MEAN
#include <winsock2.h>
#include <windows.h>
#include "backend.h"
#include "boundary.h"
#include "overlay.h"
#include "config.h"
#include "netcheck.h"
#include <cstring>
#include <cstdio>
#include <cwchar>
namespace {
volatile LONG started=0;
}
// Starts OpenLink for the whole game process. Settings come from openlink-launcher.json
// next to this DLL, all optional: "directory" (default: the public OpenLink Directory) and
// "lan_ip" (empty: loopback relay, the normal mode; set it to this PC's LAN address only
// when an OpenLink Server runs on the same PC and owns the loopback game port) and
// "vote_overlay" ("off" hides the in-game vote panel) and "vote_keys" (four hotkeys, default
// "Alt+1, Alt+2, Alt+3, Alt+4"; see hotkey.h).
extern "C" __declspec(dllexport) DWORD WINAPI HiOpenLinkStart(void*) {
    if(InterlockedCompareExchange(&started,1,0)) return ERROR_ALREADY_EXISTS;
    const DWORD boundary=clientjoin::CheckNoEac(GetCurrentProcessId()); if(boundary) return boundary;
    wchar_t game[32768]; const DWORD count=GetModuleFileNameW(nullptr,game,32768);
    if(!count || count>=32768 || !clientjoin::ValidateFile(game)) return ERROR_BAD_EXE_FORMAT;
    HMODULE own=nullptr;
    if(!GetModuleHandleExW(GET_MODULE_HANDLE_EX_FLAG_FROM_ADDRESS|GET_MODULE_HANDLE_EX_FLAG_PIN,reinterpret_cast<LPCWSTR>(&HiOpenLinkStart),&own)) return GetLastError();
    char json[4096]; if(const DWORD e=clientjoin::config::Read(own,json)) return e;
    char directory[160]="https://openlink-dir.unggoy.xyz",lan_ip[16]={},value[160];
    if(clientjoin::config::String(json,"directory",value,sizeof(value)) && value[0]) strcpy_s(directory,value);
    if(!clientjoin::netcheck::DirectoryAllowed(directory)) return ERROR_INVALID_PARAMETER; // the launcher says why first
    if(clientjoin::config::String(json,"lan_ip",value,sizeof(value))) { if(std::strlen(value)>=sizeof(lan_ip)) return ERROR_INVALID_PARAMETER; strcpy_s(lan_ip,value); }
    if(clientjoin::config::String(json,"vote_overlay",value,sizeof(value)) && !_stricmp(value,"off")) clientjoin::overlay::SetEnabled(false);
    if(clientjoin::config::String(json,"vote_keys",value,sizeof(value))) {
        clientjoin::hotkey::Key keys[clientjoin::hotkey::kVoteKeys];
        if(!clientjoin::hotkey::ParseVoteKeys(value,keys,nullptr,0)) return ERROR_INVALID_PARAMETER; // the launcher says why first
        clientjoin::overlay::SetVoteKeys(keys);
    }
    WSADATA data; if(WSAStartup(MAKEWORD(2,2),&data)) return ERROR_NETWORK_UNREACHABLE; // kept for the process lifetime
    return clientjoin::StartPersistent(directory,lan_ip);
}

BOOL WINAPI DllMain(HINSTANCE module,DWORD reason,LPVOID) {
    if(reason==DLL_PROCESS_ATTACH) DisableThreadLibraryCalls(module);
    return TRUE;
}
