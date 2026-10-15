// Directory and server address rules (OpenLink, AGPL-3.0); see netcheck.h.
#define WIN32_LEAN_AND_MEAN
#include <winsock2.h>
#include <ws2tcpip.h>
#include "netcheck.h"
#include <cstdint>
#include <cstring>
namespace clientjoin {
namespace netcheck {
const char kDirectoryRule[]="the directory address must start with https:// (http:// only for a directory on this PC or your local network)";
namespace {
enum Kind { NotIp, Local, Public, Unusable }; // Unusable: unspecified or multicast
Kind V4(const uint8_t* a) {
    if(a[0]==127 || a[0]==10 || (a[0]==172 && (a[1]&0xf0)==16) || (a[0]==192 && a[1]==168) || (a[0]==169 && a[1]==254)) return Local;
    if((a[0]|a[1]|a[2]|a[3])==0 || (a[0]&0xf0)==0xe0) return Unusable;
    return Public;
}
Kind Classify(const char* host) {
    char h[64]; size_t n=std::strlen(host);
    if(n>=2 && host[0]=='[' && host[n-1]==']') { host+=1; n-=2; } // URL form of an IPv6 address
    if(!n || n>=sizeof(h)) return NotIp;
    std::memcpy(h,host,n); h[n]=0;
    uint8_t a[16];
    if(inet_pton(AF_INET,h,a)==1) return V4(a);
    if(inet_pton(AF_INET6,h,a)!=1) return NotIp;
    static const uint8_t mapped[12]={0,0,0,0,0,0,0,0,0,0,0xff,0xff};
    if(!std::memcmp(a,mapped,12)) return V4(a+12); // ::ffff:a.b.c.d is that IPv4 address
    bool zero=true; for(int i=0;i<15;++i) if(a[i]) zero=false;
    if(zero && a[15]==1) return Local;                          // ::1
    if(zero && a[15]==0) return Unusable;                       // ::
    if((a[0]&0xfe)==0xfc || (a[0]==0xfe && (a[1]&0xc0)==0x80)) return Local; // fc00::/7, fe80::/10
    if(a[0]==0xff) return Unusable;                             // multicast
    return Public;
}
// The host part of "scheme://host[:port][/...]" (IPv6 in brackets kept).
bool UrlHost(const char* url,size_t skip,char* out,size_t capacity) {
    const char* p=url+skip; size_t n=0;
    if(*p=='[') { while(p[n] && p[n]!=']') ++n; if(p[n]!=']') return false; ++n; }
    else while(p[n] && p[n]!=':' && p[n]!='/' && p[n]!='?' && p[n]!='#') ++n;
    if(!n || n>=capacity || std::memchr(p,'@',n)) return false;
    std::memcpy(out,p,n); out[n]=0; return true;
}
bool StartsWith(const char* s,const char* prefix) { return !_strnicmp(s,prefix,std::strlen(prefix)); }
bool LocalUrl(const char* url) {
    char host[256]; size_t skip=0;
    if(StartsWith(url,"http://")) skip=7; else if(StartsWith(url,"https://")) skip=8; else return false;
    return UrlHost(url,skip,host,sizeof(host)) && LocalHost(host);
}
}
bool LocalHost(const char* host) noexcept {
    return host && (!_stricmp(host,"localhost") || Classify(host)==Local);
}
bool DirectoryAllowed(const char* url) noexcept {
    if(!url) return false;
    char host[256];
    if(StartsWith(url,"https://")) return UrlHost(url,8,host,sizeof(host));
    return StartsWith(url,"http://") && LocalUrl(url);
}
bool ServerHostAllowed(const char* directory,const char* host) noexcept {
    if(!directory || !host || !host[0]) return false;
    if(LocalUrl(directory)) return true;
    if(!_stricmp(host,"localhost")) return false;
    const Kind k=Classify(host);
    return k==NotIp || k==Public;
}
}
}
