// In-process OpenLink player relay; behaviour follows connect.Start
// (community/connect/connect.go), internal/relay and internal/beacon.
#define WIN32_LEAN_AND_MEAN
#include <winsock2.h>
#include <ws2tcpip.h>
#include <mstcpip.h>
#ifndef SIO_UDP_CONNRESET
#define SIO_UDP_CONNRESET _WSAIOW(IOC_VENDOR,12)
#endif
#include <winhttp.h>
#include "relay.h"
#include "version.h"
#include <atomic>
#include <new>
#include <cstring>
#include <cstdlib>
namespace clientjoin {
namespace relay {
namespace {
constexpr uint16_t kGamePort=1343,kDiscoveryPort=7117;
constexpr ULONGLONG kBeaconInterval=1000,kBeaconFresh=15000,kBeaconPoll=2000,kGoneAfter=10000,kIdle=120000;
constexpr size_t kMaxBeacon=512,kMaxBody=1<<20,kMaxSessions=4;
constexpr char kBallotPrefix[]="HICOMM-BALLOT ";

SRWLOCK lock=SRWLOCK_INIT;
Config config={};
ReadyCallback ready_callback=nullptr;
std::atomic<bool> started{false};
Status status={};
uint8_t beacon[kMaxBeacon]={}; size_t beacon_size=0; ULONGLONG beacon_at=0; // GetTickCount64 of capture
std::atomic<ULONGLONG> last_down{0};
std::atomic<bool> gone{false};
char server_host[256]={}; uint16_t server_port=0;

void Set(State s,uint32_t error=0) { AcquireSRWLockExclusive(&lock); status.state=s; if(error) status.error=error; ReleaseSRWLockExclusive(&lock); }
template<class F> void Update(F f) { AcquireSRWLockExclusive(&lock); f(status); ReleaseSRWLockExclusive(&lock); }

// ---- minimal JSON scanning (directory responses only) ----
struct Cursor { const char* p; const char* end; };
void Space(Cursor& c) { while(c.p<c.end && (*c.p==' '||*c.p=='\t'||*c.p=='\r'||*c.p=='\n')) ++c.p; }
// Reads a JSON string into out (\u escapes become UTF-8; surrogate pairs become ?).
bool String(Cursor& c,char* out,size_t capacity,size_t* length=nullptr) {
    Space(c); if(c.p>=c.end || *c.p!='"') return false; ++c.p; size_t n=0;
    while(c.p<c.end && *c.p!='"') {
        char ch=*c.p++;
        if(ch=='\\') {
            if(c.p>=c.end) return false; char e=*c.p++;
            switch(e) { case '"': case '\\': case '/': ch=e; break; case 'b': ch='\b'; break; case 'f': ch='\f'; break;
                case 'n': ch='\n'; break; case 'r': ch='\r'; break; case 't': ch='\t'; break;
                case 'u': { if(c.end-c.p<4) return false; unsigned v=0; for(int i=0;i<4;++i) { char h=*c.p++; v<<=4;
                    if(h>='0'&&h<='9') v|=unsigned(h-'0'); else if(h>='a'&&h<='f') v|=unsigned(h-'a'+10); else if(h>='A'&&h<='F') v|=unsigned(h-'A'+10); else return false; }
                    if(v<0x80) { ch=char(v); break; }
                    // Non-ASCII escapes are written as UTF-8 (surrogate pairs become '?').
                    char u[3]; size_t un;
                    if(v>=0xd800 && v<0xe000) { u[0]='?'; un=1; }
                    else if(v<0x800) { u[0]=char(0xc0|(v>>6)); u[1]=char(0x80|(v&0x3f)); un=2; }
                    else { u[0]=char(0xe0|(v>>12)); u[1]=char(0x80|((v>>6)&0x3f)); u[2]=char(0x80|(v&0x3f)); un=3; }
                    for(size_t k=0;k<un;++k) { if(out) { if(n+1>=capacity) return false; out[n]=u[k]; } ++n; }
                    continue; }
                default: return false; }
        }
        if(out) { if(n+1>=capacity) return false; out[n]=ch; }
        ++n;
    }
    if(c.p>=c.end) return false; ++c.p;
    if(out) out[n]=0; if(length) *length=n; return true;
}
bool Skip(Cursor& c,int depth=0);
bool Number(Cursor& c,int64_t& v) {
    Space(c); const char* s=c.p; bool neg=false; if(c.p<c.end && *c.p=='-') { neg=true; ++c.p; }
    if(c.p>=c.end || *c.p<'0' || *c.p>'9') return false; int64_t n=0;
    while(c.p<c.end && *c.p>='0' && *c.p<='9') { if(n>(INT64_MAX-9)/10) return false; n=n*10+(*c.p++-'0'); }
    while(c.p<c.end && (*c.p=='.'||*c.p=='e'||*c.p=='E'||*c.p=='+'||*c.p=='-'||(*c.p>='0'&&*c.p<='9'))) ++c.p; // fraction ignored
    v=neg ? -n : n; return c.p>s;
}
bool Skip(Cursor& c,int depth) {
    if(depth>32) return false; Space(c); if(c.p>=c.end) return false;
    if(*c.p=='"') return String(c,nullptr,0);
    if(*c.p=='{'||*c.p=='[') {
        const char close=*c.p=='{' ? '}' : ']'; const bool object=*c.p=='{'; ++c.p; Space(c);
        if(c.p<c.end && *c.p==close) { ++c.p; return true; }
        for(;;) {
            if(object) { if(!String(c,nullptr,0)) return false; Space(c); if(c.p>=c.end || *c.p!=':') return false; ++c.p; }
            if(!Skip(c,depth+1)) return false; Space(c); if(c.p>=c.end) return false;
            if(*c.p==',') { ++c.p; continue; } if(*c.p==close) { ++c.p; return true; } return false;
        }
    }
    const char* s=c.p; while(c.p<c.end && *c.p!=','&&*c.p!='}'&&*c.p!=']'&&*c.p!=' '&&*c.p!='\n'&&*c.p!='\r'&&*c.p!='\t') ++c.p;
    return c.p>s;
}
struct Listing { char id[64]; char host[256]; int64_t port; };
// Reads one object's top-level id/host/port.
bool ReadListing(Cursor& c,Listing& l) {
    l={}; l.port=-1; Space(c); if(c.p>=c.end || *c.p!='{') return false; ++c.p; Space(c);
    if(c.p<c.end && *c.p=='}') { ++c.p; return true; }
    for(;;) {
        char key[32]; const Cursor saved=c; if(!String(c,key,sizeof(key))) { // long keys are skipped, not fatal
            c=saved; if(!String(c,nullptr,0)) return false; key[0]=0; }
        Space(c); if(c.p>=c.end || *c.p!=':') return false; ++c.p; Space(c);
        bool ok;
        if(!std::strcmp(key,"id") && c.p<c.end && *c.p=='"') ok=String(c,l.id,sizeof(l.id));
        else if(!std::strcmp(key,"host") && c.p<c.end && *c.p=='"') ok=String(c,l.host,sizeof(l.host));
        else if(!std::strcmp(key,"port") && c.p<c.end && *c.p!='"') ok=Number(c,l.port);
        else ok=Skip(c);
        if(!ok) return false; Space(c); if(c.p>=c.end) return false;
        if(*c.p==',') { ++c.p; continue; } if(*c.p=='}') { ++c.p; return true; } return false;
    }
}
template<class F> bool EachListing(const char* json,size_t length,F f) {
    Cursor c={json,json+length}; Space(c); if(c.p>=c.end || *c.p!='[') return false; ++c.p; Space(c);
    if(c.p<c.end && *c.p==']') return true;
    for(;;) {
        Listing l; if(!ReadListing(c,l)) return false; if(f(l)) return true;
        Space(c); if(c.p>=c.end) return false; if(*c.p==',') { ++c.p; continue; } if(*c.p==']') return true; return false;
    }
}

// ---- HTTP (WinHTTP, GET only, same origin) ----
struct Url { wchar_t host[256]; INTERNET_PORT port; bool secure; };
bool ParseDirectory(const char* text,Url& u) {
    wchar_t wide[160]={}; size_t n=std::strlen(text); if(!n || n>=160) return false;
    for(size_t i=0;i<n;++i) { if(text[i]<=' ' || text[i]>126) return false; wide[i]=wchar_t(text[i]); }
    URL_COMPONENTS parts={}; parts.dwStructSize=sizeof(parts); parts.lpszHostName=u.host; parts.dwHostNameLength=256;
    wchar_t path[160]={}; parts.lpszUrlPath=path; parts.dwUrlPathLength=160;
    if(!WinHttpCrackUrl(wide,DWORD(n),0,&parts) || !parts.dwHostNameLength) return false;
    if(parts.nScheme!=INTERNET_SCHEME_HTTP && parts.nScheme!=INTERNET_SCHEME_HTTPS) return false;
    if(parts.dwUrlPathLength>1 || (parts.dwUrlPathLength==1 && path[0]!=L'/')) return false;
    u.port=parts.nPort; u.secure=parts.nScheme==INTERNET_SCHEME_HTTPS; return true;
}
// Returns HTTP status (0 on transport failure); body is NUL-terminated.
DWORD Get(const Url& u,const char* path,char* body,size_t capacity,size_t& size) {
    size=0; if(capacity) body[0]=0;
    wchar_t wpath[160]={}; size_t n=std::strlen(path); if(n>=160) return 0; for(size_t i=0;i<n;++i) wpath[i]=wchar_t(path[i]);
    DWORD code=0;
    HINTERNET session=WinHttpOpen(L"OpenLink-Launcher/" OPENLINK_VERSION_W,WINHTTP_ACCESS_TYPE_AUTOMATIC_PROXY,WINHTTP_NO_PROXY_NAME,WINHTTP_NO_PROXY_BYPASS,0);
    if(!session) return 0;
    WinHttpSetTimeouts(session,5000,5000,5000,5000);
    DWORD redirect=WINHTTP_OPTION_REDIRECT_POLICY_NEVER; WinHttpSetOption(session,WINHTTP_OPTION_REDIRECT_POLICY,&redirect,sizeof(redirect));
    HINTERNET connection=WinHttpConnect(session,u.host,u.port,0);
    HINTERNET request=connection ? WinHttpOpenRequest(connection,L"GET",wpath,nullptr,WINHTTP_NO_REFERER,WINHTTP_DEFAULT_ACCEPT_TYPES,u.secure ? WINHTTP_FLAG_SECURE : 0) : nullptr;
    if(request && WinHttpSendRequest(request,WINHTTP_NO_ADDITIONAL_HEADERS,0,WINHTTP_NO_REQUEST_DATA,0,0,0) && WinHttpReceiveResponse(request,nullptr)) {
        DWORD length=sizeof(code);
        if(WinHttpQueryHeaders(request,WINHTTP_QUERY_STATUS_CODE|WINHTTP_QUERY_FLAG_NUMBER,WINHTTP_HEADER_NAME_BY_INDEX,&code,&length,WINHTTP_NO_HEADER_INDEX)) {
            for(;;) {
                DWORD got=0; if(size+1>=capacity) { code=0; break; }
                if(!WinHttpReadData(request,body+size,DWORD(capacity-1-size),&got)) { code=0; break; }
                if(!got) break; size+=got;
            }
            body[size]=0;
        } else code=0;
    }
    if(request) WinHttpCloseHandle(request); if(connection) WinHttpCloseHandle(connection); WinHttpCloseHandle(session);
    return code;
}

bool LocalAllowed(const sockaddr_in& a,uint32_t lan) {
    const uint32_t ip=ntohl(a.sin_addr.s_addr); return (ip>>24)==127 || (lan && a.sin_addr.s_addr==lan);
}
void NoConnReset(SOCKET s) { BOOL off=FALSE; DWORD bytes=0; WSAIoctl(s,SIO_UDP_CONNRESET,&off,sizeof(off),nullptr,0,&bytes,nullptr,nullptr); }
bool Resolve(const char* host,uint16_t port,sockaddr_in& out) {
    addrinfo hints={}; hints.ai_family=AF_INET; hints.ai_socktype=SOCK_DGRAM; addrinfo* result=nullptr;
    char service[8]; _itoa_s(port,service,sizeof(service),10);
    if(getaddrinfo(host,service,&hints,&result) || !result) return false;
    std::memcpy(&out,result->ai_addr,sizeof(out)); freeaddrinfo(result); return true;
}

// Beacon poller: newest beacon every 2 s; follows a relisted server by endpoint
// and declares the session gone like connect.pollBeacons.
// each relay session runs while its generation is current; Start() bumps it.
std::atomic<uint32_t> generation{0};
DWORD WINAPI PollBeacons(void* parameter) {
    const uint32_t mine=static_cast<uint32_t>(reinterpret_cast<uintptr_t>(parameter));
    Url u={}; ParseDirectory(config.directory,u);
    char id[64]; strcpy_s(id,config.listing);
    static char body[kMaxBody];
    ULONGLONG missing=0;
    while(!gone.load() && generation.load()==mine) {
        char path[128]; strcpy_s(path,"/v1/servers/"); strcat_s(path,id); strcat_s(path,"/beacon");
        size_t size=0; const DWORD code=Get(u,path,body,sizeof(body),size);
        if(code==200) {
            missing=0; uint8_t b[kMaxBeacon]; size_t n=0; int64_t age=0;
            if(ParseBeacon(body,size,b,sizeof(b),n,age) && age>=0) {
                AcquireSRWLockExclusive(&lock);
                const ULONGLONG now=GetTickCount64(),at=ULONGLONG(age)<now ? now-ULONGLONG(age) : 0;
                if(n!=beacon_size || std::memcmp(b,beacon,n)) { std::memcpy(beacon,b,n); beacon_size=n; beacon_at=at; ++status.beacons; }
                ReleaseSRWLockExclusive(&lock);
            }
        } else if(code==404) {
            char other[64]={};
            if(Get(u,"/v1/servers",body,sizeof(body),size)==200 && FindListingByEndpoint(body,size,server_host,server_port,other,sizeof(other)) && std::strcmp(other,id)) {
                strcpy_s(id,other); missing=0; continue;
            }
            const ULONGLONG now=GetTickCount64(); if(!missing) missing=now;
            const ULONGLONG down=last_down.load();
            if(now-missing>=kGoneAfter && (!down || now-down>=kGoneAfter)) { gone.store(true); Set(Gone); break; }
        }
        Sleep(DWORD(kBeaconPoll));
    }
    return 0;
}

struct Session { sockaddr_in client; SOCKET upstream; ULONGLONG last; bool answered; };

DWORD Run(uint32_t mine) {
    Set(Resolving);
    WSADATA data; if(WSAStartup(MAKEWORD(2,2),&data)) { Set(Failed,ERROR_NETWORK_UNREACHABLE); return 1; }
    Url u={}; static char body[kMaxBody]; size_t size=0;
    if(!ParseDirectory(config.directory,u)) { Set(Failed,ERROR_INVALID_PARAMETER); return 1; }
    const DWORD code=Get(u,"/v1/servers",body,sizeof(body),size);
    if(code!=200 || !FindListing(body,size,config.listing,server_host,sizeof(server_host),server_port)) { Set(Failed,code==200 ? ERROR_NOT_FOUND : ERROR_CONNECTION_REFUSED); return 1; }
    sockaddr_in upstream={};
    if(!Resolve(server_host,server_port,upstream)) { Set(Failed,ERROR_HOST_UNREACHABLE); return 1; }
    sockaddr_in local={}; local.sin_family=AF_INET; local.sin_port=htons(kGamePort);
    uint32_t lan=0; sockaddr_in target={}; target.sin_family=AF_INET; target.sin_port=htons(kDiscoveryPort);
    if(config.lan_ip[0]) { inet_pton(AF_INET,config.lan_ip,&local.sin_addr); lan=local.sin_addr.s_addr; target.sin_addr.s_addr=INADDR_BROADCAST; }
    else { local.sin_addr.s_addr=htonl(INADDR_LOOPBACK); target.sin_addr.s_addr=htonl(INADDR_LOOPBACK); }
    SOCKET listen_socket=socket(AF_INET,SOCK_DGRAM,IPPROTO_UDP), advert=socket(AF_INET,SOCK_DGRAM,IPPROTO_UDP);
    BOOL yes=TRUE;
    sockaddr_in advert_bind=local; advert_bind.sin_port=0;
    if(listen_socket==INVALID_SOCKET || advert==INVALID_SOCKET || bind(listen_socket,reinterpret_cast<sockaddr*>(&local),sizeof(local)) ||
        setsockopt(advert,SOL_SOCKET,SO_BROADCAST,reinterpret_cast<const char*>(&yes),sizeof(yes)) || bind(advert,reinterpret_cast<sockaddr*>(&advert_bind),sizeof(advert_bind))) {
        const DWORD e=WSAGetLastError(); if(listen_socket!=INVALID_SOCKET) closesocket(listen_socket); if(advert!=INVALID_SOCKET) closesocket(advert);
        Set(Failed,e ? e : ERROR_ADDRESS_ALREADY_ASSOCIATED); return 1;
    }
    NoConnReset(listen_socket);
    Update([&](Status& s){ s.state=Bound; std::memcpy(s.local,&local.sin_addr,4); });
    HANDLE poller=CreateThread(nullptr,0,PollBeacons,reinterpret_cast<void*>(uintptr_t(mine)),0,nullptr);
    char local_text[16]={}; inet_ntop(AF_INET,&local.sin_addr,local_text,sizeof(local_text));

    Session sessions[kMaxSessions]={}; for(auto& s:sessions) s.upstream=INVALID_SOCKET;
    ULONGLONG next_advert=0; uint32_t adverts=0; bool announced=false;
    uint8_t buffer[2048];
    while(!gone.load() && generation.load()==mine) {
        const ULONGLONG now=GetTickCount64();
        if(now>=next_advert) {
            next_advert=now+kBeaconInterval;
            uint8_t b[kMaxBeacon]; size_t n=0;
            AcquireSRWLockShared(&lock); if(beacon_size && now-beacon_at<kBeaconFresh) { n=beacon_size; std::memcpy(b,beacon,n); } ReleaseSRWLockShared(&lock);
            if(n && sendto(advert,reinterpret_cast<const char*>(b),int(n),0,reinterpret_cast<sockaddr*>(&target),sizeof(target))==int(n)) {
                ++adverts; Update([&](Status& s){ s.adverts=adverts; if(s.state==Bound) s.state=Advertising; });
                if(!announced && adverts>=2 && ready_callback && generation.load()==mine) { announced=true; ready_callback(local_text); }
            }
        }
        for(auto& s:sessions) if(s.upstream!=INVALID_SOCKET && now-s.last>=kIdle) { closesocket(s.upstream); s.upstream=INVALID_SOCKET; }
        fd_set readable; FD_ZERO(&readable); FD_SET(listen_socket,&readable);
        for(auto& s:sessions) if(s.upstream!=INVALID_SOCKET) FD_SET(s.upstream,&readable);
        timeval slice={0,100000};
        const int ready=select(0,&readable,nullptr,nullptr,&slice);
        if(ready==SOCKET_ERROR) { Sleep(10); continue; }
        if(!ready) continue;
        if(FD_ISSET(listen_socket,&readable)) {
            sockaddr_in from={}; int from_size=sizeof(from);
            const int got=recvfrom(listen_socket,reinterpret_cast<char*>(buffer),sizeof(buffer),0,reinterpret_cast<sockaddr*>(&from),&from_size);
            if(got>0 && LocalAllowed(from,lan)) {
                Session* use=nullptr;
                for(auto& s:sessions) if(s.upstream!=INVALID_SOCKET && s.client.sin_addr.s_addr==from.sin_addr.s_addr && s.client.sin_port==from.sin_port) { use=&s; break; }
                if(!use) for(auto& s:sessions) if(s.upstream==INVALID_SOCKET) {
                    SOCKET up=socket(AF_INET,SOCK_DGRAM,IPPROTO_UDP);
                    if(up!=INVALID_SOCKET && !connect(up,reinterpret_cast<sockaddr*>(&upstream),sizeof(upstream))) { NoConnReset(up); s={from,up,now,false}; use=&s; }
                    else if(up!=INVALID_SOCKET) closesocket(up);
                    break;
                }
                if(use && send(use->upstream,reinterpret_cast<const char*>(buffer),got,0)==got) { use->last=now; Update([](Status& s){ ++s.up; }); }
            }
        }
        for(auto& s:sessions) {
            if(s.upstream==INVALID_SOCKET || !FD_ISSET(s.upstream,&readable)) continue;
            const int got=recv(s.upstream,reinterpret_cast<char*>(buffer),sizeof(buffer),0);
            if(got<=0) continue;
            s.last=now; last_down.store(now);
            if(IsBallot(buffer,size_t(got))) continue; // vote traffic belongs to the app, never the game
            if(sendto(listen_socket,reinterpret_cast<const char*>(buffer),got,0,reinterpret_cast<sockaddr*>(&s.client),sizeof(s.client))==got) {
                const bool first=!s.answered; s.answered=true;
                Update([&](Status& st){ ++st.down; if(first || st.state==Advertising) st.state=Answered; });
            }
        }
        uint32_t active=0; for(auto& s:sessions) if(s.upstream!=INVALID_SOCKET) ++active;
        Update([&](Status& st){ st.sessions=active; });
    }
    for(auto& s:sessions) if(s.upstream!=INVALID_SOCKET) closesocket(s.upstream);
    closesocket(listen_socket); closesocket(advert);
    // The poller shares this session's state; a successor starts only after it exits.
    if(poller) { WaitForSingleObject(poller,INFINITE); CloseHandle(poller); }
    if(generation.load()!=mine) Set(Gone);
    return 0;
}
// Start() hands each session to a launcher thread that first waits for the
// previous session (whose generation was bumped) to release the port and threads.
SRWLOCK start_lock=SRWLOCK_INIT;
HANDLE current_thread=nullptr;
struct Launch { Config config; ReadyCallback callback; HANDLE previous; uint32_t generation; };
DWORD WINAPI LaunchSession(void* parameter) {
    Launch job=*static_cast<Launch*>(parameter); delete static_cast<Launch*>(parameter);
    if(job.previous) { WaitForSingleObject(job.previous,INFINITE); CloseHandle(job.previous); }
    if(generation.load()!=job.generation) return 0; // superseded while waiting
    AcquireSRWLockExclusive(&lock);
    config=job.config; ready_callback=job.callback; status=Status{}; beacon_size=0; beacon_at=0;
    ReleaseSRWLockExclusive(&lock);
    gone.store(false); last_down.store(0); server_host[0]=0; server_port=0;
    return Run(job.generation);
}
}

namespace {
// Top-level keys of one listing plus match.name/match.phase; other values skipped.
bool ReadRow(Cursor& c,Row& r) {
    r={}; r.players=-1; Space(c); if(c.p>=c.end || *c.p!='{') return false; ++c.p;
    for(;;) {
        Space(c); if(c.p<c.end && *c.p=='}') { ++c.p; return true; }
        char key[32]; const Cursor saved=c; if(!String(c,key,sizeof(key))) { c=saved; if(!String(c,nullptr,0)) return false; key[0]=0; }
        Space(c); if(c.p>=c.end || *c.p!=':') return false; ++c.p; Space(c);
        bool ok; int64_t n=0;
        const bool text=c.p<c.end && *c.p=='"';
        if(text && !std::strcmp(key,"id")) ok=String(c,r.id,sizeof(r.id));
        else if(text && !std::strcmp(key,"name")) { const Cursor s=c; ok=String(c,r.name,sizeof(r.name)); if(!ok) { c=s; ok=String(c,nullptr,0); r.name[0]=0; } }
        else if(text && !std::strcmp(key,"status")) { const Cursor s=c; ok=String(c,r.status,sizeof(r.status)); if(!ok) { c=s; ok=String(c,nullptr,0); r.status[0]=0; } }
        else if(text && !std::strcmp(key,"description")) { const Cursor s=c; ok=String(c,r.description,sizeof(r.description)); if(!ok) { c=s; ok=String(c,nullptr,0); r.description[0]=0; } }
        else if(!std::strcmp(key,"players") && !text) { ok=Number(c,n); if(ok) r.players=int32_t(n<0 ? -1 : n>100000 ? 100000 : n); }
        else if(!std::strcmp(key,"app_protocol") && !text) { ok=Number(c,n); if(ok) r.app_protocol=int32_t(n<0 ? 0 : n>1000000 ? 1000000 : n); }
        else if(!std::strcmp(key,"joinable") && !text) { r.joinable=c.end-c.p>=4 && !std::strncmp(c.p,"true",4); ok=Skip(c); }
        else if(!std::strcmp(key,"match") && c.p<c.end && *c.p=='{') {
            ++c.p; ok=true;
            for(;;) {
                Space(c); if(c.p<c.end && *c.p=='}') { ++c.p; break; }
                char inner[16]; const Cursor s=c; if(!String(c,inner,sizeof(inner))) { c=s; if(!String(c,nullptr,0)) return false; inner[0]=0; }
                Space(c); if(c.p>=c.end || *c.p!=':') return false; ++c.p; Space(c);
                bool v; const bool t=c.p<c.end && *c.p=='"';
                if(t && !std::strcmp(inner,"name")) { const Cursor s2=c; v=String(c,r.match,sizeof(r.match)); if(!v) { c=s2; v=String(c,nullptr,0); r.match[0]=0; } }
                else if(t && !std::strcmp(inner,"phase")) { const Cursor s2=c; v=String(c,r.phase,sizeof(r.phase)); if(!v) { c=s2; v=String(c,nullptr,0); r.phase[0]=0; } }
                else if(t && !std::strcmp(inner,"thumb")) { const Cursor s2=c; v=String(c,r.thumb,sizeof(r.thumb)); if(!v) { c=s2; v=String(c,nullptr,0); r.thumb[0]=0; } }
                else if(t && !std::strcmp(inner,"mode")) { const Cursor s2=c; v=String(c,r.mode,sizeof(r.mode)); if(!v) { c=s2; v=String(c,nullptr,0); r.mode[0]=0; } }
                else v=Skip(c);
                if(!v) return false; Space(c); if(c.p>=c.end) return false;
                if(*c.p==',') { ++c.p; continue; } if(*c.p=='}') { ++c.p; break; } return false;
            }
        }
        else ok=Skip(c);
        if(!ok) return false; Space(c); if(c.p>=c.end) return false;
        if(*c.p==',') { ++c.p; continue; } if(*c.p=='}') { ++c.p; return true; } return false;
    }
}
}
bool ParseServerList(const char* json,size_t length,Row* rows,size_t capacity,size_t& count) noexcept {
    count=0; Cursor c={json,json+length}; Space(c); if(c.p>=c.end || *c.p!='[') return false; ++c.p; Space(c);
    if(c.p<c.end && *c.p==']') return true;
    for(;;) {
        Row r; if(!ReadRow(c,r)) return false;
        if(r.id[0] && count<capacity) rows[count++]=r;
        Space(c); if(c.p>=c.end) return false; if(*c.p==',') { ++c.p; continue; } if(*c.p==']') return true; return false;
    }
}
uint32_t ListServers(const char* directory,Row* rows,size_t capacity,size_t& count) noexcept {
    count=0; Url u={}; if(!directory || !ParseDirectory(directory,u)) return ERROR_INVALID_PARAMETER;
    static char body[kMaxBody]; static SRWLOCK list_lock=SRWLOCK_INIT; size_t size=0;
    AcquireSRWLockExclusive(&list_lock);
    const DWORD code=Get(u,"/v1/servers",body,sizeof(body),size);
    const uint32_t result=code!=200 ? (code ? ERROR_BAD_NET_RESP : ERROR_CONNECTION_REFUSED) : ParseServerList(body,size,rows,capacity,count) ? 0u : uint32_t(ERROR_INVALID_DATA);
    ReleaseSRWLockExclusive(&list_lock); return result;
}
bool IsBallot(const uint8_t* d,size_t n) noexcept { const size_t p=sizeof(kBallotPrefix)-1; return n>=p && !std::memcmp(d,kBallotPrefix,p); }

size_t Base64Decode(const char* text,size_t length,uint8_t* out,size_t capacity) noexcept {
    auto value=[](char c)->int { if(c>='A'&&c<='Z') return c-'A'; if(c>='a'&&c<='z') return c-'a'+26; if(c>='0'&&c<='9') return c-'0'+52; if(c=='+') return 62; if(c=='/') return 63; return -1; };
    if(!length || length%4) return 0; size_t n=0;
    for(size_t i=0;i<length;i+=4) {
        int v[4]; int pad=0;
        for(int k=0;k<4;++k) { const char c=text[i+k]; if(c=='=') { if(i+4!=length || k<2) return 0; v[k]=0; ++pad; } else { if(pad) return 0; v[k]=value(c); if(v[k]<0) return 0; } }
        const uint32_t triple=uint32_t(v[0])<<18|uint32_t(v[1])<<12|uint32_t(v[2])<<6|uint32_t(v[3]);
        const int bytes=3-pad; if(n+size_t(bytes)>capacity) return 0;
        out[n++]=uint8_t(triple>>16); if(bytes>1) out[n++]=uint8_t(triple>>8); if(bytes>2) out[n++]=uint8_t(triple);
    }
    return n;
}
bool FindListing(const char* json,size_t length,const char* id,char* host,size_t host_capacity,uint16_t& port) noexcept {
    bool found=false;
    const bool ok=EachListing(json,length,[&](const Listing& l){
        if(std::strcmp(l.id,id)) return false;
        if(!l.host[0] || l.port<1 || l.port>65535 || std::strlen(l.host)>=host_capacity) return true;
        strcpy_s(host,host_capacity,l.host); port=uint16_t(l.port); found=true; return true; });
    return ok && found;
}
bool FindListingByEndpoint(const char* json,size_t length,const char* host,uint16_t port,char* id,size_t id_capacity) noexcept {
    bool found=false;
    const bool ok=EachListing(json,length,[&](const Listing& l){
        if(std::strcmp(l.host,host) || l.port!=port || !l.id[0] || std::strlen(l.id)>=id_capacity) return false;
        strcpy_s(id,id_capacity,l.id); found=true; return true; });
    return ok && found;
}
bool ParseBeacon(const char* json,size_t length,uint8_t* out,size_t capacity,size_t& size,int64_t& age_ms) noexcept {
    Cursor c={json,json+length}; Space(c); if(c.p>=c.end || *c.p!='{') return false; ++c.p;
    bool have_beacon=false,have_age=false; static char text[kMaxBeacon*2];
    for(;;) {
        Space(c); if(c.p<c.end && *c.p=='}') break;
        char key[32]; const Cursor saved=c; if(!String(c,key,sizeof(key))) { c=saved; if(!String(c,nullptr,0)) return false; key[0]=0; } Space(c); if(c.p>=c.end || *c.p!=':') return false; ++c.p;
        bool ok;
        if(!std::strcmp(key,"beacon")) { size_t n=0; ok=String(c,text,sizeof(text),&n); if(ok) { size=Base64Decode(text,n,out,capacity); have_beacon=size>=16; } }
        else if(!std::strcmp(key,"age_ms")) { ok=Number(c,age_ms); have_age=ok; }
        else ok=Skip(c);
        if(!ok) return false; Space(c); if(c.p>=c.end) return false;
        if(*c.p==',') { ++c.p; continue; } if(*c.p=='}') break; return false;
    }
    return have_beacon && have_age;
}
bool ValidConfig(const Config& c) noexcept {
    if(c.directory[sizeof(c.directory)-1] || c.listing[sizeof(c.listing)-1] || c.lan_ip[sizeof(c.lan_ip)-1]) return false;
    const size_t n=std::strlen(c.listing); if(!n) return false;
    for(size_t i=0;i<n;++i) { const char ch=c.listing[i]; if(!((ch>='0'&&ch<='9')||(ch>='a'&&ch<='z')||(ch>='A'&&ch<='Z')||ch=='-'||ch=='_')) return false; }
    Url u={}; if(!ParseDirectory(c.directory,u)) return false;
    if(c.lan_ip[0]) { in_addr a={}; if(inet_pton(AF_INET,c.lan_ip,&a)!=1) return false; const uint32_t ip=ntohl(a.s_addr); if(!ip || (ip>>24)==127 || (ip>>28)>=14) return false; }
    return true;
}
uint32_t Start(const Config& c,ReadyCallback callback) noexcept {
    if(!ValidConfig(c)) return ERROR_INVALID_PARAMETER;
    // Replaces any previous session (it is told to stop and is waited for off-thread).
    auto* job=new(std::nothrow) Launch{c,callback,nullptr,0};
    if(!job) return ERROR_NOT_ENOUGH_MEMORY;
    AcquireSRWLockExclusive(&start_lock);
    job->previous=current_thread; job->generation=generation.fetch_add(1)+1;
    HANDLE thread=CreateThread(nullptr,0,LaunchSession,job,0,nullptr);
    DWORD e=ERROR_SUCCESS;
    if(thread) current_thread=thread;
    else { e=GetLastError(); current_thread=job->previous; delete job; }
    ReleaseSRWLockExclusive(&start_lock);
    if(e) Set(Failed,e);
    started.store(true); return e;
}
Status Snapshot() noexcept { AcquireSRWLockShared(&lock); Status s=status; ReleaseSRWLockShared(&lock); return s; }
}
}
