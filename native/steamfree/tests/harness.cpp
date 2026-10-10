#include <windows.h>
#include <cstdint>
#include <cstdio>
#include <cstring>
#include <atomic>
#include <thread>
#include <vector>
#include <cstddef>

struct Context { void(*initialize)(void*); uint64_t counter; void* value; };
struct Callback { void** table; unsigned char flags; unsigned char padding[3]; int id; };
static_assert(offsetof(Context,value)==16 && offsetof(Callback,id)==12,"ABI offsets");
static std::atomic<unsigned> calls{0};
static void*(*factory)(int,const char*);
static int(*getUser)();
static void Initialize(void* cell) { ++calls; *static_cast<void**>(cell)=factory(getUser(),"STEAMAPPS_INTERFACE_VERSION008"); }
static void RunResult(void*,void*,bool,uint64_t) {}
static void RunCallback(void*,void*) {}
static int Size(void*) { return 1; }
template<class T> T Symbol(HMODULE m,const char* n) {
    auto value=GetProcAddress(m,n);
    if (!value) { std::fprintf(stderr,"FAIL missing %s\n",n); ExitProcess(2); }
    return reinterpret_cast<T>(value);
}
template<class T> T Method(void* self,unsigned slot) {
    return reinterpret_cast<T>((*static_cast<void***>(self))[slot]);
}
int wmain(int argc,wchar_t** argv) {
    SetErrorMode(SEM_FAILCRITICALERRORS|SEM_NOGPFAULTERRORBOX);
    if (argc<2) return 2;
    auto dll=LoadLibraryW(argv[1]);
    if (!dll) { std::fprintf(stderr,"FAIL replacement DLL unavailable (%lu)\n",GetLastError()); return 1; }
    auto init=Symbol<bool(*)()>(dll,"SteamAPI_Init");
    auto shutdown=Symbol<void(*)()>(dll,"SteamAPI_Shutdown");
    factory=Symbol<void*(*)(int,const char*)>(dll,"SteamInternal_FindOrCreateUserInterface");
    getUser=Symbol<int(*)()>(dll,"SteamAPI_GetHSteamUser");
    auto contextInit=Symbol<void*(*)(void*)>(dll,"SteamInternal_ContextInit");
    auto reg=Symbol<void(*)(void*,int)>(dll,"SteamAPI_RegisterCallback");
    auto unreg=Symbol<void(*)(void*)>(dll,"SteamAPI_UnregisterCallback");
    auto pump=Symbol<void(*)()>(dll,"SteamAPI_RunCallbacks");
    bool nonServer=argc>2 && wcscmp(argv[2],L"nonserver")==0;
    if (nonServer) {
        bool refused=!init() && getUser()==0;
        std::printf("non-server initialization refused: %s\n",refused?"PASS":"FAIL");
        return refused?0:1;
    }
    if (!init()) { std::fprintf(stderr,"FAIL server init\n"); return 1; }
    if (argc>2 && wcscmp(argv[2],L"interface")==0) { factory(getUser(),"SteamUser020"); return 3; }
    if (argc>2 && wcscmp(argv[2],L"export")==0) { Symbol<void(*)()>(dll,"SteamAPI_ISteamUser_GetSteamID")(); return 3; }
    if (argc>2 && wcscmp(argv[2],L"callresult")==0) {
        void* callbackTable[]={reinterpret_cast<void*>(RunResult),reinterpret_cast<void*>(RunCallback),reinterpret_cast<void*>(Size)};
        Callback result{callbackTable,0,{0,0,0},0};
        Symbol<void(*)(void*,uint64_t)>(dll,"SteamAPI_RegisterCallResult")(&result,123);return 3;
    }
    auto apps=factory(getUser(),"STEAMAPPS_INTERFACE_VERSION008");
    if (!apps) return 1;
    if (argc>2 && wcscmp(argv[2],L"slot")==0) { Method<void(*)(void*)>(apps,0)(apps); return 3; }
    std::atomic<int> errors{0};
    auto subscribed=Method<bool(*)(void*,uint32_t)>(apps,6);
    if (!subscribed(apps,1240440) || subscribed(apps,1708090) || subscribed(apps,1708091) || subscribed(apps,1708092)) ++errors;
    if (Symbol<bool(*)(uint32_t)>(dll,"SteamAPI_RestartAppIfNecessary")(1240440)) ++errors;
    if (getUser()==0 || Symbol<int(*)()>(dll,"SteamAPI_GetHSteamPipe")()==0) ++errors;
    auto language=Method<const char*(*)(void*)>(apps,4);
    const char* english=language(apps);
    if (!english || std::strcmp(english,"english") || language(apps)!=english) ++errors;
    auto launch=Method<int(*)(void*,char*,int)>(apps,26);
    struct { char before; char output[4]; char after; } buffer={'A',{'x','x','x','x'},'Z'};
    if (launch(apps,buffer.output,1)!=0 || buffer.before!='A' || buffer.after!='Z' || buffer.output[0]!=0 || buffer.output[1]!='x') ++errors;
    buffer.output[0]='x';
    if (launch(apps,buffer.output,0)!=0 || buffer.output[0]!='x') ++errors;
    if (launch(apps,buffer.output,-1)!=0 || buffer.output[0]!='x') ++errors;
    if (launch(apps,nullptr,0)!=0) ++errors;
    auto query=Method<const char*(*)(void*,const char*)>(apps,21);
    auto empty=query(apps,"handle");
    if (!empty || *empty || query(apps,"inviteXuid")!=empty) ++errors;
    auto utils=factory(0,"SteamUtils009");
    if (!utils) return 1;
    Method<void(*)(void*,void*)>(utils,16)(utils,nullptr);
    auto html=factory(getUser(),"STEAMHTMLSURFACE_INTERFACE_VERSION_005");
    if (!html || !Method<bool(*)(void*)>(html,2)(html)) ++errors;
    Context ctx{Initialize,0,nullptr};
    std::vector<std::thread> workers;
    for (int i=0;i<8;++i) workers.emplace_back([&,i] {
        for (int j=0;j<100;++j) if (contextInit(&ctx)!=&ctx.value) ++errors;
    });
    for (auto& worker:workers) worker.join();
    if (calls!=1 || ctx.value!=apps) ++errors;
    auto counter=ctx.counter;
    contextInit(&ctx);
    if (calls!=1 || ctx.counter!=counter) ++errors;
    void* table[]={reinterpret_cast<void*>(RunResult),reinterpret_cast<void*>(RunCallback),reinterpret_cast<void*>(Size)};
    Callback cb{table,0,{0,0,0},0};
    reg(&cb,101);
    if (!(cb.flags&1) || cb.id!=101) ++errors;
    reg(&cb,103);
    if (!(cb.flags&1) || cb.id!=103) ++errors;
    pump();unreg(&cb);unreg(&cb);
    if (cb.flags&1) ++errors;
    workers.clear();
    for (int i=0;i<8;++i) workers.emplace_back([&,i] {
        Callback own{table,0,{0,0,0},0};
        for (int j=0;j<100;++j) {
            reg(&own,337+i);pump();
            if (!(own.flags&1) || own.id!=337+i) ++errors;
            unreg(&own);
            if (own.flags&1) ++errors;
        }
    });
    for (auto& worker:workers) worker.join();
    auto unregResult=Symbol<void(*)(void*,uint64_t)>(dll,"SteamAPI_UnregisterCallResult");
    unregResult(&cb,123);
    shutdown();
    if (getUser()!=0 || !init()) ++errors;
    if (contextInit(&ctx)!=&ctx.value || ctx.value!=apps || calls!=2 || ctx.counter==counter) ++errors;
    shutdown();shutdown();
    std::printf("server-only ABI: handles, 3 interfaces, strings/buffer bounds, context cache/reinit/8 threads, callback flags/IDs/concurrency: %s\n",errors?"FAIL":"PASS");
    return errors?1:0;
}
