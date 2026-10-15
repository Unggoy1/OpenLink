// Launcher update check (OpenLink, AGPL-3.0); see update.h. Mirrors internal/release/release.go.
#include "update.h"
#include <cstdio>
#include <cstdlib>
#include <cstring>
#include <cstdint>
namespace clientjoin {
namespace update {
namespace {
struct Semver { long major,minor,patch; char pre[48]; };
bool Digits(const char* p,size_t n,long& v) {
    if(!n || n>9) return false; v=0;
    for(size_t i=0;i<n;++i) { if(p[i]<'0' || p[i]>'9') return false; v=v*10+(p[i]-'0'); }
    return true;
}
// vMAJOR.MINOR.PATCH[-PRE][+BUILD] (build ignored), as ParseSemver.
bool Parse(const char* s,Semver& v) {
    v={};
    if(*s=='v') ++s;
    const char* end=std::strchr(s,'+'); const size_t n=end ? size_t(end-s) : std::strlen(s);
    const char* dash=static_cast<const char*>(std::memchr(s,'-',n));
    const size_t core=dash ? size_t(dash-s) : n;
    const char* d1=static_cast<const char*>(std::memchr(s,'.',core)); if(!d1) return false;
    const char* d2=static_cast<const char*>(std::memchr(d1+1,'.',core-size_t(d1+1-s))); if(!d2) return false;
    if(!Digits(s,size_t(d1-s),v.major) || !Digits(d1+1,size_t(d2-d1-1),v.minor) || !Digits(d2+1,core-size_t(d2+1-s),v.patch)) return false;
    if(dash) { const size_t pn=n-core-1; if(!pn || pn>=sizeof(v.pre)) return false; std::memcpy(v.pre,dash+1,pn); v.pre[pn]=0; }
    return true;
}
bool PreNewer(const char* a,const char* b) {
    for(;;) {
        const char* ae=std::strchr(a,'.'); const char* be=std::strchr(b,'.');
        const size_t an=ae ? size_t(ae-a) : std::strlen(a), bn=be ? size_t(be-b) : std::strlen(b);
        long ai=0,bi=0; const bool anum=Digits(a,an,ai),bnum=Digits(b,bn,bi);
        if(anum && bnum) { if(ai!=bi) return ai>bi; }
        else if(anum) return false; // numeric identifiers sort before alphanumeric ones
        else if(bnum) return true;
        else { const int c=std::strncmp(a,b,an<bn ? an : bn); if(c || an!=bn) return c ? c>0 : an>bn; }
        if(!ae || !be) return ae && !be; // a longer list wins when all shared ones are equal
        a=ae+1; b=be+1;
    }
}
bool Newer(const Semver& v,const Semver& o) {
    if(v.major!=o.major) return v.major>o.major;
    if(v.minor!=o.minor) return v.minor>o.minor;
    if(v.patch!=o.patch) return v.patch>o.patch;
    if(!std::strcmp(v.pre,o.pre)) return false;
    if(!v.pre[0]) return true; // a release is newer than its pre-releases
    if(!o.pre[0]) return false;
    return PreNewer(v.pre,o.pre);
}
// ---- a small JSON reader: only what a releases list needs ----
struct Cursor { const char* p; const char* end; int depth; };
void Space(Cursor& c) { while(c.p<c.end && (*c.p==' '||*c.p=='\t'||*c.p=='\r'||*c.p=='\n')) ++c.p; }
// A string; its text (escapes decoded, \u to '?') into out when given.
bool String(Cursor& c,char* out,size_t capacity) {
    Space(c); if(c.p>=c.end || *c.p!='"') return false; ++c.p;
    size_t n=0; bool fits=true;
    while(c.p<c.end && *c.p!='"') {
        char ch=*c.p++;
        if(ch=='\\') {
            if(c.p>=c.end) return false;
            const char e=*c.p++;
            switch(e) { case 'n': ch='\n'; break; case 't': ch='\t'; break; case 'r': ch='\r'; break; case 'b': ch='\b'; break; case 'f': ch='\f'; break;
                case 'u': if(c.end-c.p<4) return false; c.p+=4; ch='?'; break; default: ch=e; }
        }
        if(out) { if(n+1<capacity) out[n++]=ch; else fits=false; }
    }
    if(c.p>=c.end) return false; ++c.p;
    if(out) out[n]=0;
    return fits;
}
bool Skip(Cursor& c);
template<class F> bool Object(Cursor& c,F field) {
    Space(c); if(c.p>=c.end || *c.p!='{' || ++c.depth>32) return false; ++c.p; Space(c);
    if(c.p<c.end && *c.p=='}') { ++c.p; --c.depth; return true; }
    for(;;) {
        char key[32]; const Cursor saved=c;
        if(!String(c,key,sizeof(key))) { c=saved; if(!String(c,nullptr,0)) return false; key[0]=0; }
        Space(c); if(c.p>=c.end || *c.p!=':') return false; ++c.p;
        if(!field(key,c)) return false;
        Space(c); if(c.p>=c.end) return false;
        if(*c.p==',') { ++c.p; continue; }
        if(*c.p=='}') { ++c.p; --c.depth; return true; }
        return false;
    }
}
template<class F> bool Array(Cursor& c,F item) {
    Space(c); if(c.p>=c.end || *c.p!='[' || ++c.depth>32) return false; ++c.p; Space(c);
    if(c.p<c.end && *c.p==']') { ++c.p; --c.depth; return true; }
    for(;;) {
        if(!item(c)) return false;
        Space(c); if(c.p>=c.end) return false;
        if(*c.p==',') { ++c.p; continue; }
        if(*c.p==']') { ++c.p; --c.depth; return true; }
        return false;
    }
}
bool Skip(Cursor& c) {
    Space(c); if(c.p>=c.end) return false;
    if(*c.p=='"') return String(c,nullptr,0);
    if(*c.p=='{') return Object(c,[](const char*,Cursor& k){ return Skip(k); });
    if(*c.p=='[') return Array(c,[](Cursor& k){ return Skip(k); });
    const char* start=c.p;
    while(c.p<c.end && *c.p!=',' && *c.p!='}' && *c.p!=']' && *c.p!=' ' && *c.p!='\r' && *c.p!='\n' && *c.p!='\t') ++c.p;
    return c.p>start;
}
bool Bool(Cursor& c,bool& v) {
    Space(c);
    if(c.end-c.p>=4 && !std::strncmp(c.p,"true",4)) { v=true; c.p+=4; return true; }
    if(c.end-c.p>=5 && !std::strncmp(c.p,"false",5)) { v=false; c.p+=5; return true; }
    return Skip(c);
}
}
bool Released(const char* version) noexcept {
    Semver v; if(!version || !Parse(version,v)) return false;
    if(std::strstr(v.pre,"dirty")) return false;
    // git describe: "<commits>-g<hash>" after the tag
    const char* g=std::strstr(v.pre,"-g");
    if(g) { long n=0; if(Digits(v.pre,size_t(g-v.pre),n)) return false; }
    return true;
}
bool Pick(const char* json,size_t length,const char* current,const char* asset,Found& out) noexcept {
    out={};
    Semver cur; if(!json || !asset || !Released(current) || !Parse(current,cur)) return false;
    Semver best=cur; bool found=false;
    Cursor c={json,json+length,0};
    const bool ok=Array(c,[&](Cursor& k){
        char tag[64]={},url[256]={}; bool draft=false,pre=false,has=false,url_ok=true,tag_ok=true;
        const bool read=Object(k,[&](const char* key,Cursor& f)->bool{
            if(!std::strcmp(key,"tag_name")) { const Cursor s=f; tag_ok=String(f,tag,sizeof(tag)); if(!tag_ok) { f=s; return Skip(f); } return true; }
            if(!std::strcmp(key,"html_url")) { const Cursor s=f; url_ok=String(f,url,sizeof(url)); if(!url_ok) { f=s; return Skip(f); } return true; }
            if(!std::strcmp(key,"draft")) return Bool(f,draft);
            if(!std::strcmp(key,"prerelease")) return Bool(f,pre);
            if(!std::strcmp(key,"assets")) return Array(f,[&](Cursor& a){
                return Object(a,[&](const char* akey,Cursor& af)->bool{
                    if(std::strcmp(akey,"name")) return Skip(af);
                    char name[128]; const Cursor s=af;
                    if(String(af,name,sizeof(name))) { if(!std::strcmp(name,asset)) has=true; return true; }
                    af=s; return Skip(af);
                });
            });
            return Skip(f);
        });
        if(!read) return false;
        Semver v;
        // The link is opened in the browser: only this project's release pages.
        if(!tag_ok || !url_ok || !Parse(tag,v) || draft || (pre && !cur.pre[0]) || std::strncmp(url,kPages,sizeof(kPages)-1) || !has) return true;
        if(Newer(v,best)) { best=v; strcpy_s(out.latest,tag); strcpy_s(out.url,url); found=true; }
        return true;
    });
    return ok && found;
}
}
}
