// Steam-free steam_api64.dll for the OpenLink Server's Halo Infinite (B002)
// LAN server process. Implements only the Steam calls a B002 server makes and
// stops the process with a named diagnostic (0xe0534645) on anything else.
// Refuses to initialize without -server, so a player client never runs on it.
#define WIN32_LEAN_AND_MEAN
#include <windows.h>
#include <shellapi.h>
#include <atomic>
#include <cstdint>
#include <cstdio>
#include <cstring>
#include <cwchar>
#include <map>
#include <mutex>
#include <cstddef>

static HMODULE selfModule;
static INIT_ONCE logInit=INIT_ONCE_STATIC_INIT;
static SRWLOCK logMutex=SRWLOCK_INIT;
static HANDLE logFile=INVALID_HANDLE_VALUE;
static unsigned long long logSequence;
static std::recursive_mutex stateMutex;
static std::atomic<bool> initialized{false};
static uint64_t generation=1;
static std::atomic<uint64_t> pumps{0};
struct Callback { void** table; unsigned char flags; unsigned char padding[3]; int id; };
struct Context { void(*initialize)(void*); uint64_t counter; void* value; };
static_assert(offsetof(Callback,id)==12 && offsetof(Context,value)==16,"verified x64 ABI");
static std::map<Callback*,int> callbacks;
using WarningHook=void(*)(int,const char*);
static WarningHook warningHook;

static BOOL CALLBACK OpenLog(PINIT_ONCE,PVOID,PVOID*) {
    wchar_t path[32768];
    auto count=GetModuleFileNameW(selfModule,path,32768);
    if (!count || count>=32768) return TRUE;
    auto slash=wcsrchr(path,L'\\');
    if (!slash) return TRUE;
    wchar_t file[80];
    swprintf_s(file,L"steamfree-%lu.jsonl",GetCurrentProcessId());
    if (wcscpy_s(slash+1,size_t(32768-(slash+1-path)),file)) return TRUE;
    logFile=CreateFileW(path,GENERIC_WRITE,FILE_SHARE_READ,nullptr,CREATE_ALWAYS,FILE_ATTRIBUTE_NORMAL,nullptr);
    return TRUE;
}
static void Log(const char* event,const char* name,long long value=0) {
    auto previous=GetLastError();
    InitOnceExecuteOnce(&logInit,OpenLog,nullptr,nullptr);
    if (logFile!=INVALID_HANDLE_VALUE) {
        AcquireSRWLockExclusive(&logMutex);
        ++logSequence;
        if (logSequence<=100000) {
            char clean[256];unsigned n=0;
            for (;name && *name && n<sizeof(clean)-1;++name) {
                unsigned char c=static_cast<unsigned char>(*name);
                clean[n++]=(c>=32 && c<127 && c!='"' && c!='\\')?char(c):'_';
            }
            clean[n]=0;
            LARGE_INTEGER tick;QueryPerformanceCounter(&tick);
            char line[512];
            int bytes=sprintf_s(line,"{\"seq\":%llu,\"qpc\":%lld,\"tid\":%lu,\"event\":\"%s\",\"name\":\"%s\",\"value\":%lld}\n",
                logSequence,tick.QuadPart,GetCurrentThreadId(),event,clean,value);
            DWORD written;
            if (bytes>0) WriteFile(logFile,line,DWORD(bytes),&written,nullptr);
        } else if (logSequence==100001) {
            const char marker[]="{\"event\":\"limit\",\"max_records\":100000}\n";
            DWORD written;WriteFile(logFile,marker,sizeof(marker)-1,&written,nullptr);
        }
        ReleaseSRWLockExclusive(&logMutex);
    }
    SetLastError(previous);
}
[[noreturn]] static void Unsupported(const char* kind,const char* name,long long value=0) {
    Log(kind,name,value);
    if (logFile!=INVALID_HANDLE_VALUE) FlushFileBuffers(logFile);
    RaiseException(0xe0534645,EXCEPTION_NONCONTINUABLE,0,nullptr);
    TerminateProcess(GetCurrentProcess(),0xe0534645);
    std::abort();
}

extern "C" void UtilsWarning(void*,WarningHook hook) {
    std::lock_guard<std::recursive_mutex> lock(stateMutex);warningHook=hook;
    Log("method","Utils009.SetWarningMessageHook",hook?1:0);
}
extern "C" const char* AppsLanguage(void*) {
    Log("method","Apps008.GetCurrentGameLanguage");return "english";
}
extern "C" bool AppsSubscribed(void*,uint32_t appId) {
    Log("method","Apps008.BIsSubscribedApp",appId);
    // LAN hosting uses the free base-game data. Optional high-resolution and
    // campaign entries stay disabled; this is not a Steam ownership service.
    return appId==1240440;
}
extern "C" const char* AppsQuery(void*,const char*) {
    Log("method","Apps008.GetLaunchQueryParam");return "";
}
extern "C" int AppsLaunch(void*,char* output,int capacity) {
    Log("method","Apps008.GetLaunchCommandLine");
    if (output && capacity>0) output[0]=0;
    return 0;
}
extern "C" bool HtmlShutdown(void*) {
    Log("method","HTMLSurface005.Shutdown");return true;
}
#include "generated.h"
struct Interface { void* const* table; };
static Interface utils{Table0},apps{Table1},html{Table2};

extern "C" [[noreturn]] void UnsupportedExport(unsigned index) {
    if (index>=sizeof(ExportNames)/sizeof(ExportNames[0])) Unsupported("unsupported_export","invalid_index",index);
    Unsupported("unsupported_export",ExportNames[index]);
}
extern "C" [[noreturn]] void UnsupportedSlot(void*,unsigned group,unsigned slot) {
    static const char* const names[]={"SteamUtils009","STEAMAPPS_INTERFACE_VERSION008","STEAMHTMLSURFACE_INTERFACE_VERSION_005"};
    Unsupported("unsupported_slot",group<3?names[group]:"unknown",slot);
}

static bool IsServer() {
    int count=0;
    auto args=CommandLineToArgvW(GetCommandLineW(),&count);
    if (!args) return false;
    bool server=false;
    for (int i=1;i<count;++i) if (_wcsicmp(args[i],L"-server")==0) server=true;
    LocalFree(args);return server;
}
extern "C" {
void* g_pSteamClientGameServer=nullptr;
bool SteamAPI_RestartAppIfNecessary(uint32_t appId) {
    Log("api","SteamAPI_RestartAppIfNecessary",appId);return false;
}
bool SteamAPI_Init() {
    if (!IsServer()) { Log("init_refused","requires_-server");return false; }
    std::lock_guard<std::recursive_mutex> lock(stateMutex);
    if (!initialized) { ++generation;initialized=true;pumps=0; }
    Log("init","server_local_compatibility",static_cast<long long>(generation));return true;
}
bool SteamAPI_InitSafe() { return SteamAPI_Init(); }
void SteamAPI_Shutdown() {
    std::lock_guard<std::recursive_mutex> lock(stateMutex);
    initialized=false;++generation;callbacks.clear();warningHook=nullptr;
    Log("shutdown","server_local_compatibility",static_cast<long long>(pumps.load()));
}
int SteamAPI_GetHSteamUser() { return initialized?1:0; }
int SteamAPI_GetHSteamPipe() { return initialized?1:0; }
int GetHSteamUser() { return SteamAPI_GetHSteamUser(); }
int GetHSteamPipe() { return SteamAPI_GetHSteamPipe(); }
void* SteamInternal_FindOrCreateUserInterface(int,const char* version) {
    if (!initialized) Unsupported("invalid_lifecycle","interface_before_init");
    if (!version) Unsupported("unsupported_interface","null");
    Log("interface",version);
    if (!std::strcmp(version,"SteamUtils009")) return &utils;
    if (!std::strcmp(version,"STEAMAPPS_INTERFACE_VERSION008")) return &apps;
    if (!std::strcmp(version,"STEAMHTMLSURFACE_INTERFACE_VERSION_005")) return &html;
    Unsupported("unsupported_interface",version);
}
void* SteamInternal_ContextInit(void* data) {
    if (!data) Unsupported("invalid_context","null");
    std::lock_guard<std::recursive_mutex> lock(stateMutex);
    if (!initialized) Unsupported("invalid_lifecycle","context_before_init");
    auto ctx=static_cast<Context*>(data);
    if (ctx->counter!=generation || !ctx->value) {
        if (!ctx->initialize) Unsupported("invalid_context","null_initializer");
        ctx->initialize(&ctx->value);
        if (!ctx->value) Unsupported("invalid_context","initializer_returned_null");
        ctx->counter=generation;
        Log("context","cache_initialized",static_cast<long long>(generation));
    }
    return &ctx->value;
}
void SteamAPI_RegisterCallback(Callback* cb,int id) {
    if (!cb) Unsupported("invalid_callback","null");
    std::lock_guard<std::recursive_mutex> lock(stateMutex);
    callbacks[cb]=id;cb->flags|=1;cb->id=id;
    Log("callback_register","callback",id);
}
void SteamAPI_UnregisterCallback(Callback* cb) {
    if (!cb) return;
    std::lock_guard<std::recursive_mutex> lock(stateMutex);
    callbacks.erase(cb);cb->flags&=static_cast<unsigned char>(~1);
    Log("callback_unregister","callback",cb->id);
}
void SteamAPI_RegisterCallResult(Callback*,uint64_t) {
    Unsupported("unsupported_call_result","SteamAPI_RegisterCallResult");
}
void SteamAPI_UnregisterCallResult(Callback*,uint64_t) {
    Log("call_result_unregister","SteamAPI_UnregisterCallResult");
}
void SteamAPI_RunCallbacks() {
    uint64_t count=++pumps;
    if (count==1 || count%1000==0) Log("pump","registry_only_no_synthetic_events",static_cast<long long>(count));
}
}
BOOL WINAPI DllMain(HINSTANCE instance,DWORD reason,LPVOID) {
    if (reason==DLL_PROCESS_ATTACH) selfModule=instance;
    return TRUE;
}
