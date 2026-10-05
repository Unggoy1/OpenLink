#define WIN32_LEAN_AND_MEAN
#include "bridge_api.h"
#include <tlhelp32.h>
#include <cstdio>
#include <cstdlib>
#include <cwchar>
#include <fcntl.h>
#include <io.h>
#include <string>
#include <vector>

namespace {
struct Handle {
    HANDLE value;
    explicit Handle(HANDLE h):value(h){}
    ~Handle(){if(value && value!=INVALID_HANDLE_VALUE) CloseHandle(value);}
    Handle(const Handle&)=delete; Handle& operator=(const Handle&)=delete;
};
bool SameFile(const wchar_t* left,const wchar_t* right) {
    Handle a(CreateFileW(left,FILE_READ_ATTRIBUTES,FILE_SHARE_READ|FILE_SHARE_WRITE|FILE_SHARE_DELETE,nullptr,OPEN_EXISTING,0,nullptr));
    Handle b(CreateFileW(right,FILE_READ_ATTRIBUTES,FILE_SHARE_READ|FILE_SHARE_WRITE|FILE_SHARE_DELETE,nullptr,OPEN_EXISTING,0,nullptr));
    BY_HANDLE_FILE_INFORMATION x={},y={};
    return GetFileInformationByHandle(a.value,&x) && GetFileInformationByHandle(b.value,&y) &&
        x.dwVolumeSerialNumber==y.dwVolumeSerialNumber && x.nFileIndexHigh==y.nFileIndexHigh && x.nFileIndexLow==y.nFileIndexLow;
}
DWORD Module(DWORD pid,const wchar_t* path,uintptr_t* base,DWORD* size) {
    HANDLE snapshot=INVALID_HANDLE_VALUE;
    for(unsigned i=0;i<100;++i) {
        snapshot=CreateToolhelp32Snapshot(TH32CS_SNAPMODULE,pid);
        if(snapshot!=INVALID_HANDLE_VALUE || GetLastError()!=ERROR_BAD_LENGTH) break;
        Sleep(20);
    }
    if(snapshot==INVALID_HANDLE_VALUE) return GetLastError();
    Handle owner(snapshot); MODULEENTRY32W m={};m.dwSize=sizeof(m);
    if(!Module32FirstW(snapshot,&m)) return GetLastError();
    do {
        if(SameFile(path,m.szExePath)) {*base=reinterpret_cast<uintptr_t>(m.modBaseAddr);*size=m.modBaseSize;return 0;}
    } while(Module32NextW(snapshot,&m));
    DWORD error=GetLastError(); return error==ERROR_NO_MORE_FILES ? ERROR_MOD_NOT_FOUND : error;
}
DWORD Call(HANDLE process,uintptr_t entry,const void* data,size_t bytes,DWORD* result) {
    void* parameter=nullptr;
    if(bytes) {
        parameter=VirtualAllocEx(process,nullptr,bytes,MEM_COMMIT|MEM_RESERVE,PAGE_READWRITE);
        if(!parameter) return GetLastError();
        SIZE_T written=0;
        if(!WriteProcessMemory(process,parameter,data,bytes,&written) || written!=bytes) {
            DWORD error=GetLastError();VirtualFreeEx(process,parameter,0,MEM_RELEASE);return error ? error : ERROR_WRITE_FAULT;
        }
    }
    Handle thread(CreateRemoteThread(process,nullptr,0,reinterpret_cast<LPTHREAD_START_ROUTINE>(entry),parameter,0,nullptr));
    if(!thread.value) {DWORD error=GetLastError();if(parameter)VirtualFreeEx(process,parameter,0,MEM_RELEASE);return error;}
    DWORD wait=WaitForSingleObject(thread.value,10000);
    if(wait!=WAIT_OBJECT_0) {
        // It may still be reading its argument. Never free it or terminate the thread.
        std::fprintf(stderr,"hostctl loader: remote call unfinished; argument retained until target exit\n");
        return wait==WAIT_TIMEOUT ? ERROR_TIMEOUT : GetLastError();
    }
    DWORD error=GetExitCodeThread(thread.value,result) ? 0 : GetLastError();
    if(parameter && !VirtualFreeEx(process,parameter,0,MEM_RELEASE) && !error) error=GetLastError();
    return error;
}
DWORD Load(HANDLE process,DWORD pid,const wchar_t* dll,uintptr_t* loaded,uintptr_t* stop,HostControlConfig* config) {
    // Map our export image without executing its entrypoint or resolving imports.
    HMODULE local=LoadLibraryExW(dll,nullptr,DONT_RESOLVE_DLL_REFERENCES);
    if(!local) return GetLastError();
    auto image=reinterpret_cast<const IMAGE_DOS_HEADER*>(local);
    auto nt=reinterpret_cast<const IMAGE_NT_HEADERS*>(reinterpret_cast<uintptr_t>(local)+image->e_lfanew);
    FARPROC start=GetProcAddress(local,"HiHostControlStart"),end=GetProcAddress(local,"HiHostControlStop");
    uintptr_t start_rva=reinterpret_cast<uintptr_t>(start)-reinterpret_cast<uintptr_t>(local);
    uintptr_t stop_rva=reinterpret_cast<uintptr_t>(end)-reinterpret_cast<uintptr_t>(local);
    DWORD image_size=nt->OptionalHeader.SizeOfImage;
    bool valid=nt->FileHeader.Machine==IMAGE_FILE_MACHINE_AMD64 && start && end && start_rva<image_size && stop_rva<image_size;
    FreeLibrary(local); if(!valid) return ERROR_BAD_EXE_FORMAT;
    DWORD remote_size=0;uintptr_t remote=0;
    DWORD lookup=Module(pid,dll,&remote,&remote_size);
    if(!lookup) return ERROR_ALREADY_EXISTS;
    if(lookup!=ERROR_MOD_NOT_FOUND) return lookup;
    FARPROC load=GetProcAddress(GetModuleHandleW(L"kernel32.dll"),"LoadLibraryW");
    HMODULE owner=nullptr;
    if(!load || !GetModuleHandleExW(GET_MODULE_HANDLE_EX_FLAG_FROM_ADDRESS|GET_MODULE_HANDLE_EX_FLAG_UNCHANGED_REFCOUNT,reinterpret_cast<LPCWSTR>(load),&owner)) return GetLastError();
    wchar_t owner_path[32768];
    DWORD count=GetModuleFileNameW(owner,owner_path,32768);
    if(!count || count>=32768) return ERROR_INSUFFICIENT_BUFFER;
    uintptr_t owner_base=0;DWORD owner_size=0;
    DWORD error=Module(pid,owner_path,&owner_base,&owner_size);if(error) return error;
    uintptr_t load_rva=reinterpret_cast<uintptr_t>(load)-reinterpret_cast<uintptr_t>(owner);
    if(load_rva>=owner_size) return ERROR_INVALID_ADDRESS;
    DWORD result=0;
    error=Call(process,owner_base+load_rva,dll,(std::wcslen(dll)+1)*sizeof(wchar_t),&result);if(error) return error;
    // DWORD thread exitcodes cannot hold an x64 module pointer. Enumerate it.
    error=Module(pid,dll,&remote,&remote_size);if(error) return error;
    if(remote_size!=image_size || start_rva>=remote_size || stop_rva>=remote_size) return ERROR_BAD_EXE_FORMAT;
    *loaded=remote;*stop=remote+stop_rva;
    error=Call(process,remote+start_rva,config,sizeof(*config),&result);
    return error ? error : result;
}
}

int wmain(int argc,wchar_t** argv) {
    _setmode(_fileno(stdin),_O_BINARY);_setmode(_fileno(stdout),_O_BINARY);
    DWORD error=ERROR_INVALID_PARAMETER;
    HANDLE raw=nullptr;uintptr_t module=0,stop=0;
    HostControlConfig config={};bool started=false;
    wchar_t* tail=nullptr;wchar_t* handle_tail=nullptr;
    unsigned long parsed=argc==5 ? std::wcstoul(argv[2],&tail,10) : 0;
    unsigned long long inherited=argc==5 ? std::wcstoull(argv[1],&handle_tail,10) : 0;
    if(sizeof(void*)==8 && argc==5 && parsed && tail && !*tail && inherited && handle_tail && !*handle_tail && std::fread(&config,1,sizeof(config),stdin)==sizeof(config) &&
        config.size==sizeof(config) && (config.version==1 || config.version==2) && config.port && !config.reserved && config.controller_pid) {
        raw=reinterpret_cast<HANDLE>(uintptr_t(inherited));
        if(GetProcessId(raw)!=DWORD(parsed)) error=ERROR_INVALID_HANDLE;
        else {
            wchar_t executable[32768];DWORD length=32768;BOOL wow=FALSE;
            if(!QueryFullProcessImageNameW(raw,0,executable,&length) || !IsWow64Process(raw,&wow)) error=GetLastError();
            else if(wow || !SameFile(executable,argv[4])) error=ERROR_BAD_EXE_FORMAT;
            else {error=Load(raw,DWORD(parsed),argv[3],&module,&stop,&config);started=error==0;}
        }
    }
    Handle process(raw);
    std::fwrite(&error,sizeof(error),1,stdout);std::fflush(stdout);
    SecureZeroMemory(&config,sizeof(config));
    if(error) {std::fprintf(stderr,"hostctl loader: start error %lu\n",error);return 1;}
    // Keep the exact process handle until the controller closes this pipe.
    while(std::getc(stdin)!=EOF) {}
    if(started && WaitForSingleObject(raw,0)==WAIT_TIMEOUT) {
        DWORD result=0;error=Call(raw,stop,nullptr,0,&result);if(!error)error=result;
        if(error) {std::fprintf(stderr,"hostctl loader: stop error %lu\n",error);return 2;}
    }
    return 0;
}
