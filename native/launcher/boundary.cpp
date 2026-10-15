#define WIN32_LEAN_AND_MEAN
#include "boundary.h"
#include <tlhelp32.h>
#include <bcrypt.h>
#include <cwchar>
#include <cstring>
#include <cwctype>
#include <cstdint>
#include <initializer_list>
namespace clientjoin {
namespace {
bool ContainsEac(const wchar_t* name,bool prefix) noexcept {
    const wchar_t* word=L"easyanticheat";
    for(const wchar_t* p=name;*p;++p) {
        unsigned i=0; while(word[i] && p[i] && std::towlower(p[i])==word[i]) ++i;
        if(!word[i]) return true;
        if(prefix) return false;
    }
    return false;
}
}
DWORD CheckNoEac(DWORD pid) noexcept {
    SC_HANDLE scm=OpenSCManagerW(nullptr,nullptr,SC_MANAGER_CONNECT);
    if(!scm) return GetLastError();
    DWORD result=ERROR_SUCCESS;
    for(const auto* name:{L"EasyAntiCheat_EOS",L"EasyAntiCheat"}) {
        SC_HANDLE service=OpenServiceW(scm,name,SERVICE_QUERY_STATUS);
        if(!service) { if(GetLastError()!=ERROR_SERVICE_DOES_NOT_EXIST) result=GetLastError(); }
        else {
            SERVICE_STATUS_PROCESS status={}; DWORD bytes=0;
            if(!QueryServiceStatusEx(service,SC_STATUS_PROCESS_INFO,reinterpret_cast<BYTE*>(&status),sizeof(status),&bytes)) result=GetLastError();
            else if(status.dwCurrentState!=SERVICE_STOPPED) result=ERROR_ACCESS_DENIED;
            CloseServiceHandle(service);
        }
        if(result) break;
    }
    CloseServiceHandle(scm); if(result) return result;
    HANDLE snapshot=CreateToolhelp32Snapshot(TH32CS_SNAPPROCESS,0);
    if(snapshot==INVALID_HANDLE_VALUE) return GetLastError();
    PROCESSENTRY32W process={}; process.dwSize=sizeof(process);
    if(!Process32FirstW(snapshot,&process)) result=GetLastError();
    else {
        do { if(ContainsEac(process.szExeFile,true)) { result=ERROR_ACCESS_DENIED; break; } } while(Process32NextW(snapshot,&process));
        if(!result && GetLastError()!=ERROR_NO_MORE_FILES) result=GetLastError();
    }
    CloseHandle(snapshot); if(result || !pid) return result;
    snapshot=INVALID_HANDLE_VALUE;
    for(unsigned i=0;i<10;++i) {
        snapshot=CreateToolhelp32Snapshot(TH32CS_SNAPMODULE,pid);
        if(snapshot!=INVALID_HANDLE_VALUE || GetLastError()!=ERROR_BAD_LENGTH) break;
        Sleep(10);
    }
    if(snapshot==INVALID_HANDLE_VALUE) return GetLastError();
    MODULEENTRY32W module={}; module.dwSize=sizeof(module);
    if(!Module32FirstW(snapshot,&module)) result=GetLastError();
    else {
        do { if(ContainsEac(module.szModule,false)) { result=ERROR_ACCESS_DENIED; break; } } while(Module32NextW(snapshot,&module));
        if(!result && GetLastError()!=ERROR_NO_MORE_FILES) result=GetLastError();
    }
    CloseHandle(snapshot); return result;
}
bool ValidateFile(const wchar_t* path) noexcept {
    static const uint8_t wanted[32]={0x5d,0xa5,0x18,0xec,0x21,0xf5,0xab,0x7b,0xae,0xb0,0x21,0xae,0xed,0x2b,0x98,0x92,
        0xee,0xb6,0x53,0x20,0xdc,0x17,0xd2,0x68,0x2f,0x8f,0x55,0x27,0x1c,0x20,0x3d,0xa8};
    HANDLE file=CreateFileW(path,GENERIC_READ,FILE_SHARE_READ,nullptr,OPEN_EXISTING,FILE_ATTRIBUTE_NORMAL,nullptr);
    if(file==INVALID_HANDLE_VALUE) return false;
    BCRYPT_ALG_HANDLE algorithm=nullptr; BCRYPT_HASH_HANDLE hash=nullptr;
    uint8_t* object=nullptr; DWORD length=0,returned=0; bool result=false;
    do {
        if(BCryptOpenAlgorithmProvider(&algorithm,BCRYPT_SHA256_ALGORITHM,nullptr,0)<0) break;
        if(BCryptGetProperty(algorithm,BCRYPT_OBJECT_LENGTH,reinterpret_cast<PUCHAR>(&length),sizeof(length),&returned,0)<0 || !length || length>65536) break;
        object=static_cast<uint8_t*>(HeapAlloc(GetProcessHeap(),0,length)); if(!object) break;
        if(BCryptCreateHash(algorithm,&hash,object,length,nullptr,0,0)<0) break;
        uint8_t buffer[65536]; DWORD count=0; bool ok=true;
        for(;;) {
            if(!ReadFile(file,buffer,sizeof(buffer),&count,nullptr)) { ok=false; break; }
            if(!count) break;
            if(BCryptHashData(hash,buffer,count,0)<0) { ok=false; break; }
        }
        uint8_t digest[32]={};
        if(ok && BCryptFinishHash(hash,digest,sizeof(digest),0)>=0) result=std::memcmp(digest,wanted,32)==0;
    } while(false);
    if(hash) BCryptDestroyHash(hash); if(object) HeapFree(GetProcessHeap(),0,object);
    if(algorithm) BCryptCloseAlgorithmProvider(algorithm,0); CloseHandle(file); return result;
}
DWORD CheckTarget(HANDLE process,const wchar_t* expected) noexcept {
    if(WaitForSingleObject(process,0)!=WAIT_TIMEOUT) return ERROR_PROCESS_ABORTED;
    wchar_t path[32768]; DWORD length=32768; BOOL wow=TRUE;
    if(!QueryFullProcessImageNameW(process,0,path,&length) || !IsWow64Process(process,&wow)) return GetLastError();
    if(wow || !ValidateFile(path)) return ERROR_BAD_EXE_FORMAT;
    HANDLE a=CreateFileW(path,FILE_READ_ATTRIBUTES,FILE_SHARE_READ,nullptr,OPEN_EXISTING,0,nullptr);
    HANDLE b=CreateFileW(expected,FILE_READ_ATTRIBUTES,FILE_SHARE_READ,nullptr,OPEN_EXISTING,0,nullptr);
    BY_HANDLE_FILE_INFORMATION x={},y={};
    bool same=GetFileInformationByHandle(a,&x) && GetFileInformationByHandle(b,&y) &&
        x.dwVolumeSerialNumber==y.dwVolumeSerialNumber && x.nFileIndexHigh==y.nFileIndexHigh && x.nFileIndexLow==y.nFileIndexLow;
    if(a!=INVALID_HANDLE_VALUE) CloseHandle(a); if(b!=INVALID_HANDLE_VALUE) CloseHandle(b);
    if(!same) return ERROR_BAD_EXE_FORMAT;
    const DWORD pid=GetProcessId(process); DWORD result=CheckNoEac(pid); if(result) return result;
    HANDLE snapshot=CreateToolhelp32Snapshot(TH32CS_SNAPMODULE,pid);
    if(snapshot==INVALID_HANDLE_VALUE) return GetLastError();
    MODULEENTRY32W module={}; module.dwSize=sizeof(module);
    if(!Module32FirstW(snapshot,&module)) { result=GetLastError(); CloseHandle(snapshot); return result; }
    const uintptr_t base=reinterpret_cast<uintptr_t>(module.modBaseAddr); CloseHandle(snapshot);
    uint8_t server=1; SIZE_T bytes=0;
    if(!ReadProcessMemory(process,reinterpret_cast<void*>(base+0x4eadd40),&server,1,&bytes) || bytes!=1) return GetLastError();
    return server ? ERROR_INVALID_FUNCTION : ERROR_SUCCESS;
}
}
