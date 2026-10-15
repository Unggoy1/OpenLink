// Vote hotkeys (OpenLink, AGPL-3.0); see hotkey.h.
#include "hotkey.h"
#include <cstdio>
#include <cstring>
#include <cctype>
namespace clientjoin {
namespace hotkey {
namespace {
constexpr uint32_t kAlt=0x1,kCtrl=0x2,kShift=0x4,kWin=0x8; // MOD_ALT, MOD_CONTROL, MOD_SHIFT, MOD_WIN
struct Named { const char* name; uint32_t vk; };
const Named kNamed[]={{"`",0xC0},{"Insert",0x2D},{"Delete",0x2E},{"Home",0x24},{"End",0x23},{"PageUp",0x21},{"PageDown",0x22},{"Pause",0x13}};
bool Same(const char* a,size_t n,const char* b) { return std::strlen(b)==n && !_strnicmp(a,b,n); }
// The key's virtual-key code and its spelling; false if unsupported.
bool KeyCode(const char* p,size_t n,uint32_t& vk,char (&name)[12]) {
    if(n==1 && std::isalnum(static_cast<unsigned char>(p[0]))) { const char c=char(std::toupper(static_cast<unsigned char>(p[0]))); vk=uint32_t(c); name[0]=c; name[1]=0; return true; }
    if(n==4 && !_strnicmp(p,"Num",3) && p[3]>='0' && p[3]<='9') { vk=0x60+uint32_t(p[3]-'0'); sprintf_s(name,"Num%c",p[3]); return true; }
    if((n==2 || n==3) && (p[0]=='F' || p[0]=='f')) {
        int f=0; for(size_t i=1;i<n;++i) { if(p[i]<'0' || p[i]>'9') return false; f=f*10+(p[i]-'0'); }
        if(f<1 || f>24 || (n==3 && p[1]=='0')) return false;
        vk=0x6F+uint32_t(f); sprintf_s(name,"F%d",f); return true;
    }
    for(const Named& k:kNamed) if(Same(p,n,k.name)) { vk=k.vk; strcpy_s(name,k.name); return true; }
    return false;
}
void Trim(const char*& p,size_t& n) { while(n && (*p==' ' || *p=='\t')) { ++p; --n; } while(n && (p[n-1]==' ' || p[n-1]=='\t')) --n; }
bool Fail(char* why,size_t capacity,const char* text,size_t n,const char* what) {
    if(why && capacity) sprintf_s(why,capacity,"\"%.*s\": %s",int(n<40 ? n : 40),text,what);
    return false;
}
bool ParseRange(const char* text,size_t length,Key& out,char* why,size_t why_capacity) {
    const char* p=text; size_t n=length; Trim(p,n);
    out={};
    char key[12]={}; bool have=false;
    while(n) {
        const char* plus=static_cast<const char*>(std::memchr(p,'+',n));
        const char* part=p; size_t m=plus ? size_t(plus-p) : n; Trim(part,m);
        if(plus) {
            if(Same(part,m,"ctrl") || Same(part,m,"control")) out.mods|=kCtrl;
            else if(Same(part,m,"alt")) out.mods|=kAlt;
            else if(Same(part,m,"shift")) out.mods|=kShift;
            else if(Same(part,m,"win") || Same(part,m,"meta")) out.mods|=kWin;
            else return Fail(why,why_capacity,text,length,"unknown modifier");
            n-=size_t(plus-p)+1; p=plus+1;
        } else {
            if(!KeyCode(part,m,out.vk,key)) return Fail(why,why_capacity,text,length,"unsupported key");
            have=true; n=0;
        }
    }
    if(!have) return Fail(why,why_capacity,text,length,"unsupported key");
    const bool alone_ok=!std::strcmp(key,"Pause") || (key[0]=='F' && key[1]);
    if(!(out.mods&(kCtrl|kAlt|kWin)) && !alone_ok) return Fail(why,why_capacity,text,length,"add Ctrl, Alt or Win (only F-keys and Pause can be used alone)");
    out.name[0]=0;
    if(out.mods&kCtrl) strcat_s(out.name,"Ctrl+");
    if(out.mods&kAlt) strcat_s(out.name,"Alt+");
    if(out.mods&kShift) strcat_s(out.name,"Shift+");
    if(out.mods&kWin) strcat_s(out.name,"Win+");
    strcat_s(out.name,key);
    return true;
}
}
bool Parse(const char* text,Key& out,char* why,size_t why_capacity) noexcept {
    return text && ParseRange(text,std::strlen(text),out,why,why_capacity);
}
bool ParseVoteKeys(const char* text,Key (&out)[kVoteKeys],char* why,size_t why_capacity) noexcept {
    if(!text) return false;
    const char* p=text; size_t i=0;
    for(;;) {
        const char* comma=std::strchr(p,',');
        const size_t n=comma ? size_t(comma-p) : std::strlen(p);
        if(i==kVoteKeys) { if(why && why_capacity) sprintf_s(why,why_capacity,"expected %zu vote keys",kVoteKeys); return false; }
        if(!ParseRange(p,n,out[i],why,why_capacity)) return false;
        for(size_t k=0;k<i;++k) if(!std::strcmp(out[k].name,out[i].name)) {
            if(why && why_capacity) sprintf_s(why,why_capacity,"each vote key must be different: %s",out[i].name);
            return false;
        }
        ++i;
        if(!comma) break;
        p=comma+1;
    }
    if(i!=kVoteKeys) { if(why && why_capacity) sprintf_s(why,why_capacity,"expected %zu vote keys",kVoteKeys); return false; }
    return true;
}
void DefaultVoteKeys(Key (&out)[kVoteKeys]) noexcept {
    for(size_t i=0;i<kVoteKeys;++i) { out[i]={kAlt,uint32_t('1'+i),{}}; sprintf_s(out[i].name,"Alt+%zu",i+1); }
}
}
}
