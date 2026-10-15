#pragma once
// openlink-launcher.json (next to the launcher and its DLL): a small flat JSON object of strings,
// read by the DLL (settings) and the launcher (checked before the game starts).
#ifndef WIN32_LEAN_AND_MEAN
#define WIN32_LEAN_AND_MEAN
#endif
#include <windows.h>
#include <cstdio>
#include <cstring>
#include <cwchar>
namespace clientjoin {
namespace config {
// Reads a top-level "key": "value" string (no escapes).
inline bool String(const char* json,const char* key,char* out,size_t capacity) {
    char quoted[64]; if(sprintf_s(quoted,"\"%s\"",key)<0) return false;
    const char* at=std::strstr(json,quoted); if(!at) return false;
    at+=std::strlen(quoted); while(*at==' '||*at=='\t'||*at=='\r'||*at=='\n') ++at;
    if(*at++!=':') return false; while(*at==' '||*at=='\t'||*at=='\r'||*at=='\n') ++at;
    if(*at++!='"') return false;
    size_t n=0; while(at[n] && at[n]!='"' && at[n]!='\\') ++n;
    if(at[n]!='"' || n>=capacity) return false;
    std::memcpy(out,at,n); out[n]=0; return true;
}
// Reads openlink-launcher.json from the folder of `module` (nullptr: this program) into json.
// ERROR_SUCCESS (json empty when there is no file) or a Windows error.
inline DWORD Read(HMODULE module,char (&json)[4096]) {
    json[0]=0;
    wchar_t path[MAX_PATH]; const DWORD n=GetModuleFileNameW(module,path,MAX_PATH);
    wchar_t* slash=n && n<MAX_PATH ? wcsrchr(path,L'\\') : nullptr;
    if(!slash || size_t(slash-path)+24>=MAX_PATH) return ERROR_BAD_PATHNAME;
    wcscpy_s(slash+1,MAX_PATH-size_t(slash+1-path),L"openlink-launcher.json");
    HANDLE f=CreateFileW(path,GENERIC_READ,FILE_SHARE_READ,nullptr,OPEN_EXISTING,0,nullptr);
    if(f==INVALID_HANDLE_VALUE) { const DWORD e=GetLastError(); return e==ERROR_FILE_NOT_FOUND ? ERROR_SUCCESS : e; }
    DWORD got=0; const BOOL read=ReadFile(f,json,sizeof(json)-1,&got,nullptr); const DWORD e=GetLastError(); CloseHandle(f);
    if(!read) return e;
    json[got]=0; return ERROR_SUCCESS;
}
}
}
