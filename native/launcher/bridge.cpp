// OpenLink Launcher DLL entry point (OpenLink, AGPL-3.0). openlink-launcher.exe loads this
// DLL into a Halo Infinite player client that runs without anti-cheat and calls
// HiOpenLinkStart once; OpenLink then stays active for the life of that game process.
#define WIN32_LEAN_AND_MEAN
#include <winsock2.h>
#include <windows.h>
#include "backend.h"
#include "boundary.h"
#include <cstring>
#include <cstdio>
#include <cwchar>
namespace {
// Reads a top-level "key": "value" string (no escapes) from a small JSON config.
bool ConfigString(const char* json,const char* key,char* out,size_t capacity) {
    char quoted[64]; if(sprintf_s(quoted,"\"%s\"",key)<0) return false;
    const char* at=std::strstr(json,quoted); if(!at) return false;
    at+=std::strlen(quoted); while(*at==' '||*at=='\t'||*at=='\r'||*at=='\n') ++at;
    if(*at++!=':') return false; while(*at==' '||*at=='\t'||*at=='\r'||*at=='\n') ++at;
    if(*at++!='"') return false;
    size_t n=0; while(at[n] && at[n]!='"' && at[n]!='\\') ++n;
    if(at[n]!='"' || n>=capacity) return false;
    std::memcpy(out,at,n); out[n]=0; return true;
}
volatile LONG started=0;
}
// Starts OpenLink for the whole game process. Settings come from openlink-launcher.json
// next to this DLL, all optional: "directory" (default: the public OpenLink Directory) and
// "lan_ip" (empty: loopback relay, the normal mode; set it to this PC's LAN address only
// when an OpenLink Server runs on the same PC and owns the loopback game port).
extern "C" __declspec(dllexport) DWORD WINAPI HiOpenLinkStart(void*) {
    if(InterlockedCompareExchange(&started,1,0)) return ERROR_ALREADY_EXISTS;
    const DWORD boundary=clientjoin::CheckNoEac(GetCurrentProcessId()); if(boundary) return boundary;
    wchar_t game[32768]; const DWORD count=GetModuleFileNameW(nullptr,game,32768);
    if(!count || count>=32768 || !clientjoin::ValidateFile(game)) return ERROR_BAD_EXE_FORMAT;
    HMODULE own=nullptr;
    if(!GetModuleHandleExW(GET_MODULE_HANDLE_EX_FLAG_FROM_ADDRESS|GET_MODULE_HANDLE_EX_FLAG_PIN,reinterpret_cast<LPCWSTR>(&HiOpenLinkStart),&own)) return GetLastError();
    wchar_t path[MAX_PATH]; const DWORD n=GetModuleFileNameW(own,path,MAX_PATH);
    wchar_t* slash=n && n<MAX_PATH ? wcsrchr(path,L'\\') : nullptr;
    if(!slash || size_t(slash-path)+24>=MAX_PATH) return ERROR_BAD_PATHNAME;
    wcscpy_s(slash+1,MAX_PATH-size_t(slash+1-path),L"openlink-launcher.json");
    char directory[160]="https://openlink-dir.unggoy.xyz",lan_ip[16]={};
    HANDLE f=CreateFileW(path,GENERIC_READ,FILE_SHARE_READ,nullptr,OPEN_EXISTING,0,nullptr);
    if(f!=INVALID_HANDLE_VALUE) {
        char json[4096]={}; DWORD got=0;
        const BOOL read=ReadFile(f,json,sizeof(json)-1,&got,nullptr); CloseHandle(f);
        if(!read) return GetLastError();
        json[got]=0;
        char value[160];
        if(ConfigString(json,"directory",value,sizeof(value)) && value[0]) strcpy_s(directory,value);
        if(ConfigString(json,"lan_ip",value,sizeof(value))) { if(std::strlen(value)>=sizeof(lan_ip)) return ERROR_INVALID_PARAMETER; strcpy_s(lan_ip,value); }
    }
    WSADATA data; if(WSAStartup(MAKEWORD(2,2),&data)) return ERROR_NETWORK_UNREACHABLE; // kept for the process lifetime
    return clientjoin::StartPersistent(directory,lan_ip);
}

BOOL WINAPI DllMain(HINSTANCE module,DWORD reason,LPVOID) {
    if(reason==DLL_PROCESS_ATTACH) DisableThreadLibraryCalls(module);
    return TRUE;
}
