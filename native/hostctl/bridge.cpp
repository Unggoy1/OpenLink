#define WIN32_LEAN_AND_MEAN
#include <winsock2.h>
#include <ws2tcpip.h>
#include <iphlpapi.h>
#include "bridge_api.h"
#include "game_b002.h"
#include "beacon_name.h"
#include <cstring>
#include <cstddef>

// Version1 is transport only. Explicit version2 opts into the build-pinned LAN
// backend after authentication. IPC enqueues descriptors; native functions run
// only on the engine callback. Selection is distinct from content application.
namespace {
SRWLOCK gate = SRWLOCK_INIT;
HANDLE worker = nullptr; // one launch per process; retained for Wait until exit
volatile LONG stopping = 0;
HostControlConfig launch = {};

uint16_t u16(const uint8_t* p) { return uint16_t(p[0]) | uint16_t(p[1])<<8; }
uint32_t u32(const uint8_t* p) { return uint32_t(p[0]) | uint32_t(p[1])<<8 | uint32_t(p[2])<<16 | uint32_t(p[3])<<24; }
uint64_t u64(const uint8_t* p) { return uint64_t(u32(p)) | uint64_t(u32(p+4))<<32; }
void p16(uint8_t* p,uint16_t v) { p[0]=uint8_t(v); p[1]=uint8_t(v>>8); }
void p32(uint8_t* p,uint32_t v) { for(unsigned i=0;i<4;++i) p[i]=uint8_t(v>>(8*i)); }
void p64(uint8_t* p,uint64_t v) { for(unsigned i=0;i<8;++i) p[i]=uint8_t(v>>(8*i)); }

bool equal_secret(const uint8_t* a,const uint8_t* b) {
    volatile uint8_t different = 0;
    for(unsigned i=0;i<32;++i) different = uint8_t(different | (a[i]^b[i]));
    return different==0;
}
bool nonzero(const uint8_t* p,unsigned size) {
    uint8_t bits=0; for(unsigned i=0;i<size;++i) bits|=p[i]; return bits!=0;
}
int ready(SOCKET s,bool writing,ULONGLONG deadline) {
    while(InterlockedCompareExchange(&stopping,0,0)==0) {
        if(deadline && GetTickCount64()>=deadline) return -1;
        fd_set set,errors; FD_ZERO(&set); FD_SET(s,&set); FD_ZERO(&errors); FD_SET(s,&errors); timeval slice={0,100000};
        int result=select(0,writing ? nullptr : &set,writing ? &set : nullptr,&errors,&slice);
        if(result>0) return 1;
        if(result==SOCKET_ERROR) return -1;
    }
    return 0;
}
bool send_all(SOCKET s,const uint8_t* p,unsigned n) {
    ULONGLONG deadline=GetTickCount64()+5000;
    while(n) {
        if(ready(s,true,deadline)!=1) return false;
        int got=send(s,reinterpret_cast<const char*>(p),int(n),0);
        if(got==SOCKET_ERROR && WSAGetLastError()==WSAEWOULDBLOCK) continue;
        if(got<=0) return false;
        p+=got; n-=unsigned(got);
    }
    return true;
}
int read_all(SOCKET s,uint8_t* p,unsigned n,bool allow_idle=false) {
    ULONGLONG deadline=allow_idle ? 0 : GetTickCount64()+5000;
    while(n) {
        int available=ready(s,false,deadline); if(available!=1) return available;
        int got=recv(s,reinterpret_cast<char*>(p),int(n),0);
        if(got==SOCKET_ERROR && WSAGetLastError()==WSAEWOULDBLOCK) continue;
        if(got==0) return 0; if(got<0) return -1;
        if(!deadline) deadline=GetTickCount64()+5000;
        p+=got; n-=unsigned(got);
    }
    return 1;
}
bool send_frame(SOCKET s,const uint8_t* p,unsigned n) {
    uint8_t length[4]; p32(length,n); return send_all(s,length,4) && send_all(s,p,n);
}
int read_frame(SOCKET s,uint8_t* p,unsigned capacity,unsigned& size,bool allow_idle=false) {
    uint8_t length[4]; int result=read_all(s,length,4,allow_idle); if(result!=1) return result;
    size=u32(length); if(size==0 || size>capacity) return -1;
    return read_all(s,p,size);
}
bool reply(SOCKET s,uint16_t code,uint64_t id,const hostctl::BackendReport* report=nullptr) {
    // v5: v2 layout (lifecycle state, match count, backend flags), the lobby probe at 112,
    // lobby leader XUID at 176 and leader re-asserts at 184, team diagnostics at 192.
    uint8_t b[256]={}; std::memcpy(b,"HICR",4); p16(b+4,5); p16(b+6,code);
    p64(b+8,id); p32(b+16,GetCurrentProcessId());
    if(report) {
        p32(b+20,report->gates); p64(b+24,report->generation);
        const uint8_t* ids[]={report->observed.map.asset,report->observed.map.version,report->observed.mode.asset,report->observed.mode.version};
        for(unsigned i=0;i<4;++i) hostctl::ConvertUuidLayout(ids[i],b+32+16*i);
        p32(b+96,uint32_t(report->state)); p32(b+100,report->matches); p32(b+104,report->flags);
        const auto& l=report->lobby;
        p32(b+112,l.flags); p32(b+116,uint32_t(l.connected)); p32(b+120,uint32_t(l.peers)); p32(b+124,l.peer_mask);
        p32(b+128,uint32_t(l.owner)); p32(b+132,uint32_t(l.host_peer)); p32(b+136,uint32_t(l.players)); p32(b+140,uint32_t(l.start_mode));
        b[144]=l.allowed; b[145]=l.content_prepared; b[146]=l.prep_started; b[147]=l.prep_done; b[148]=l.loading; b[149]=l.start;
        b[150]=l.blocked_start; b[151]=l.blocked_end;
        p32(b+152,uint32_t(l.users_required)); p32(b+156,uint32_t(l.game_type)); p32(b+160,uint32_t(l.session_kind));
        p32(b+164,uint32_t(l.end_game_table)); p32(b+168,uint32_t(l.end_game));
        p64(b+176,l.leader); p32(b+184,l.leader_sets);
        b[192]=uint8_t(l.lobby_variant_teams); b[193]=uint8_t(l.game_variant_teams); b[194]=l.last_teams_enabled; b[195]=l.team_fixes;
        p32(b+196,uint32_t(l.last_team_count)); p32(b+200,uint32_t(l.forced_team_count)); p32(b+204,uint32_t(l.game_state));
        std::memcpy(b+208,l.peer_team,sizeof(l.peer_team));
        static_assert(sizeof(l.peer_team)==48,"peer team bytes fill 208-255");
    } else std::memset(b+192,0xff,64);
    return send_frame(s,b,sizeof(b));
}

bool controller_owns_connection(SOCKET s) {
    sockaddr_in local={},peer={}; int local_size=sizeof(local),peer_size=sizeof(peer);
    if(getsockname(s,reinterpret_cast<sockaddr*>(&local),&local_size)!=0 || getpeername(s,reinterpret_cast<sockaddr*>(&peer),&peer_size)!=0) return false;
    if(local.sin_family!=AF_INET || peer.sin_family!=AF_INET || peer.sin_addr.s_addr!=htonl(INADDR_LOOPBACK)) return false;
    // Match the established reverse tuple, rather than a listener that could
    // have been replaced between an ownership check and connect(). Never send
    // the launch secret until the connected endpoint belongs to the controller.
    DWORD bytes=0;
    if(GetExtendedTcpTable(nullptr,&bytes,FALSE,AF_INET,TCP_TABLE_OWNER_PID_CONNECTIONS,0)!=ERROR_INSUFFICIENT_BUFFER) return false;
    for(unsigned attempt=0;attempt<3;++attempt) {
        if(bytes<offsetof(MIB_TCPTABLE_OWNER_PID,table) || bytes>4*1024*1024) return false;
        DWORD capacity=bytes;
        auto table=static_cast<MIB_TCPTABLE_OWNER_PID*>(HeapAlloc(GetProcessHeap(),0,capacity));
        if(!table) return false;
        DWORD result=GetExtendedTcpTable(table,&bytes,FALSE,AF_INET,TCP_TABLE_OWNER_PID_CONNECTIONS,0);
        bool matched=false;
        if(result==NO_ERROR && table->dwNumEntries<=(capacity-offsetof(MIB_TCPTABLE_OWNER_PID,table))/sizeof(MIB_TCPROW_OWNER_PID)) {
            for(DWORD i=0;i<table->dwNumEntries;++i) {
                const auto& row=table->table[i];
                if(row.dwState==MIB_TCP_STATE_ESTAB && row.dwOwningPid==launch.controller_pid && row.dwLocalAddr==peer.sin_addr.s_addr && uint16_t(row.dwLocalPort)==peer.sin_port && row.dwRemoteAddr==local.sin_addr.s_addr && uint16_t(row.dwRemotePort)==local.sin_port) { matched=true; break; }
            }
        }
        HeapFree(GetProcessHeap(),0,table);
        if(result!=ERROR_INSUFFICIENT_BUFFER) return matched;
    }
    return false;
}

DWORD session(SOCKET s) {
    if(!controller_owns_connection(s)) return ERROR_ACCESS_DENIED;
    uint8_t hello[52]={}; std::memcpy(hello,"HICT",4); p16(hello+4,1); p16(hello+6,1);
    p64(hello+8,1); std::memcpy(hello+16,launch.token,32); p32(hello+48,GetCurrentProcessId());
    if(!send_frame(s,hello,sizeof(hello))) return ERROR_CONNECTION_ABORTED;
    uint8_t response[96]; unsigned size=0;
    if(read_frame(s,response,sizeof(response),size)!=1 || size!=96 || std::memcmp(response,"HICR",4)!=0 || u16(response+4)!=1 || u16(response+6)!=0 || u64(response+8)!=1 || u32(response+16)!=GetCurrentProcessId()) return ERROR_ACCESS_DENIED;
    // Opt-in is explicit in the loader config and installation follows peer
    // authentication. Unsupported/incorrect role never installs a hook.
    DWORD backend_result=ERROR_NOT_SUPPORTED;
    if(launch.version==2) {
        AcquireSRWLockExclusive(&gate);
        if(InterlockedCompareExchange(&stopping,0,0)==0) backend_result=hostctl::InstallGameBackend();
        else backend_result=ERROR_CANCELLED;
        ReleaseSRWLockExclusive(&gate);
    }
    // Readiness waits check Stop every 100ms. Only the worker closes its socket.
    // Idle is unlimited; partial headers and payloads retain bounded deadlines.
    uint64_t previous_id=1;
    while(InterlockedCompareExchange(&stopping,0,0)==0) {
        uint8_t request[112]; int got=read_frame(s,request,sizeof(request),size,true);
        if(got==0) return ERROR_SUCCESS;
        if(got<0) return InterlockedCompareExchange(&stopping,0,0) ? ERROR_SUCCESS : ERROR_CONNECTION_ABORTED;
        if(size<48 || std::memcmp(request,"HICT",4)!=0 || u16(request+4)!=1 || !equal_secret(request+16,launch.token)) return ERROR_ACCESS_DENIED;
        uint64_t id=u64(request+8); if(id<=previous_id) return ERROR_INVALID_DATA; previous_id=id;
        uint16_t op=u16(request+6),code=4; hostctl::BackendReport report={}; bool have_report=false;
        if(op==2 && size==48) {
            code=2;
            if(launch.version==2) {
                if(backend_result!=ERROR_SUCCESS) code=1;
                else { report=hostctl::BackendStatus(); code=report.code; have_report=true; }
            }
        }
        // 6 Start: set start mode 1 on the next lobby tick. 7 ServerOwned(u32 mode 0/1/2).
        if((op==6 && size==48) || (op==7 && size==52)) {
            code=2;
            if(launch.version==2) {
                if(backend_result!=ERROR_SUCCESS) code=1;
                else if(op==6) { report=hostctl::BackendStart(2000); code=report.code; have_report=true; }
                else {
                    const uint32_t mode=u32(request+48);
                    if(mode>hostctl::ServerOwnedFilterOnly) code=4;
                    else {
                        const DWORD changed=hostctl::BackendServerOwned(mode);
                        report=hostctl::BackendStatus(); have_report=true;
                        code=changed==ERROR_SUCCESS ? 0 : changed==ERROR_INVALID_FUNCTION ? 1 : 5;
                    }
                }
            }
        }
        // 8 SetName: 48 bytes of printable ASCII, zero-padded (beacon_name.h).
        if(op==8 && size==96) {
            uint16_t units[hostctl::kBeaconNameUnits];
            if(hostctl::EncodeBeaconName(request+48,units)) {
                code=2;
                if(launch.version==2) {
                    if(backend_result!=ERROR_SUCCESS) code=1;
                    else { report=hostctl::BackendSetName(units,2000); code=report.code; have_report=true; }
                }
            }
        }
        // 9 SetLeader: u64 lobby leader XUID the server holds; 0 releases it.
        if(op==9 && size==56) {
            code=2;
            if(launch.version==2) {
                if(backend_result!=ERROR_SUCCESS) code=1;
                else { report=hostctl::BackendSetLeader(u64(request+48),2000); code=report.code; have_report=true; }
            }
        }
        // 10 TeamPolicy: u32 TeamPolicyFlag bits, u32 TeamMode, u32 team count, u32 team size (team_guard.h).
        if(op==10 && size==64) {
            code=2;
            if(launch.version==2) {
                if(backend_result!=ERROR_SUCCESS) code=1;
                else {
                    const DWORD changed=hostctl::BackendTeamPolicy(u32(request+48),u32(request+52),u32(request+56),u32(request+60));
                    report=hostctl::BackendStatus(); have_report=true;
                    code=changed==ERROR_SUCCESS ? 0 : 4;
                }
            }
        }
        if((op==3 || op==4 || op==5) && size==112) {
            bool valid=true; for(unsigned base=48;base<112;base+=16) valid=nonzero(request+base,16) && valid;
            if(valid) {
                code=2;
                if(launch.version==2) {
                    if(backend_result!=ERROR_SUCCESS) code=1;
                    else {
                        hostctl::ContentSelection desired={}; desired.mode.type=op==4 ? 10u : 6u;
                        if(op==5) desired.map.type=2;
                        uint8_t* ids[]={desired.map.asset,desired.map.version,desired.mode.asset,desired.mode.version};
                        for(unsigned i=0;i<4;++i) hostctl::ConvertUuidLayout(request+48+16*i,ids[i]);
                        // Map kind0 means preserve initialized native kind; no
                        // map enum is guessed from the controller's UUIDs.
                        report=hostctl::BackendSelect(desired,2000); code=report.code; have_report=true;
                    }
                }
            }
        }
        if(!reply(s,code,id,have_report ? &report : nullptr)) return ERROR_CONNECTION_ABORTED;
    }
    return ERROR_SUCCESS;
}

DWORD WINAPI run(void*) {
    WSADATA data; if(WSAStartup(MAKEWORD(2,2),&data)!=0) return ERROR_NETWORK_UNREACHABLE;
    SOCKET s=socket(AF_INET,SOCK_STREAM,IPPROTO_TCP);
    if(s==INVALID_SOCKET) { WSACleanup(); return ERROR_NETWORK_UNREACHABLE; }
    u_long nonblocking=1;
    if(ioctlsocket(s,FIONBIO,&nonblocking)!=0) { closesocket(s); WSACleanup(); return ERROR_CONNECTION_ABORTED; }
    sockaddr_in addr={}; addr.sin_family=AF_INET; addr.sin_port=htons(launch.port); addr.sin_addr.s_addr=htonl(INADDR_LOOPBACK);
    DWORD result=ERROR_CONNECTION_ABORTED;
    if(InterlockedCompareExchange(&stopping,0,0)==0) {
        bool connected=connect(s,reinterpret_cast<sockaddr*>(&addr),sizeof(addr))==0;
        if(!connected && WSAGetLastError()==WSAEWOULDBLOCK && ready(s,true,GetTickCount64()+5000)==1) {
            int error=0,size=sizeof(error);
            connected=getsockopt(s,SOL_SOCKET,SO_ERROR,reinterpret_cast<char*>(&error),&size)==0 && error==0;
        }
        if(connected) result=session(s);
    }
    if(InterlockedCompareExchange(&stopping,0,0)!=0) result=ERROR_SUCCESS;
    if(launch.version==2) { const DWORD cleanup=hostctl::StopGameBackend(); if(result==ERROR_SUCCESS) result=cleanup; }
    closesocket(s);
    WSACleanup(); return result;
}
}

extern "C" __declspec(dllexport) DWORD WINAPI HiHostControlStart(const HostControlConfig* config) {
    if(!config || config->size!=sizeof(HostControlConfig) || (config->version!=1 && config->version!=2) || config->port==0 || config->reserved!=0 || config->controller_pid==0 || !nonzero(config->token,32)) return ERROR_INVALID_PARAMETER;
    AcquireSRWLockExclusive(&gate);
    if(worker) { ReleaseSRWLockExclusive(&gate); return ERROR_ALREADY_EXISTS; }
    HMODULE own_module=nullptr;
    if(!GetModuleHandleExW(GET_MODULE_HANDLE_EX_FLAG_FROM_ADDRESS|GET_MODULE_HANDLE_EX_FLAG_PIN,reinterpret_cast<LPCWSTR>(&HiHostControlStart),&own_module)) { DWORD result=GetLastError(); ReleaseSRWLockExclusive(&gate); return result; }
    launch=*config; InterlockedExchange(&stopping,0);
    worker=CreateThread(nullptr,0,run,nullptr,0,nullptr);
    DWORD result=worker ? ERROR_SUCCESS : GetLastError();
    ReleaseSRWLockExclusive(&gate); return result;
}

extern "C" __declspec(dllexport) DWORD WINAPI HiHostControlWait(DWORD timeout_ms) {
    AcquireSRWLockShared(&gate); HANDLE thread=worker; ReleaseSRWLockShared(&gate);
    if(!thread) return ERROR_INVALID_HANDLE;
    DWORD result=WaitForSingleObject(thread,timeout_ms); if(result!=WAIT_OBJECT_0) return result;
    if(!GetExitCodeThread(thread,&result)) return GetLastError(); return result;
}

extern "C" __declspec(dllexport) DWORD WINAPI HiHostControlStop() {
    AcquireSRWLockExclusive(&gate);
    InterlockedExchange(&stopping,1);
    // The worker owns Winsock I/O and socket closure. Its readiness loop sees
    // this flag without cross-thread shutdown/close races.
    ReleaseSRWLockExclusive(&gate);
    const DWORD cleanup=launch.version==2 ? hostctl::StopGameBackend() : ERROR_SUCCESS;
    const DWORD stopped=HiHostControlWait(5000); return stopped==ERROR_SUCCESS ? cleanup : stopped;
}

BOOL WINAPI DllMain(HINSTANCE module,DWORD reason,LPVOID) {
    if(reason==DLL_PROCESS_ATTACH) DisableThreadLibraryCalls(module);
    return TRUE;
}
