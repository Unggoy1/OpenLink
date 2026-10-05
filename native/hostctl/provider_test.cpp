#define WIN32_LEAN_AND_MEAN
#include <windows.h>
#include "lan_provider.h"
#include <cstdio>
#include <cstring>
#include <stdexcept>

namespace {
int failures=0,checks=0,reads=0,sets=0,behavior=0;
bool absent=false,pending=false;
constexpr uintptr_t table_id=0x143d44d80;
struct Fixture { uintptr_t table=table_id; hostctl::ContentArray contents={}; } fixture;
hostctl::ContentArray before={},submitted={};
void check(bool ok,const char* message) { if(!ok) { ++failures; std::fprintf(stderr,"FAIL %s\n",message); } }
uint8_t authority(void* provider) { ++checks; check(provider==&fixture,"authority receiver"); if(behavior==7) throw std::runtime_error("authority"); return behavior==1 ? 0 : 1; }
const hostctl::ContentArray* current(void* provider) { ++reads; check(provider==&fixture,"getter receiver"); if(behavior==8) throw std::runtime_error("getter"); return absent || behavior==2 || (behavior==6 && sets) ? nullptr : &fixture.contents; }
const hostctl::ContentArray* requested(void* provider) { check(provider==&fixture,"requested receiver"); return pending ? &before : nullptr; }
uint8_t set(void* provider,const hostctl::ContentArray* contents) {
    ++sets; check(provider==&fixture,"setter receiver");
    check(reinterpret_cast<uintptr_t>(contents)%16==0,"submission alignment");
    submitted=*contents;
    if(behavior==3) return 0;
    if(behavior==9) throw std::runtime_error("setter");
    fixture.contents=*contents; absent=false;
    if(behavior==10) throw std::runtime_error("notification after mutation");
    if(behavior==4) fixture.contents.entries[0].mode.version[9]^=1;
    if(behavior==5) fixture.contents.entries[15].map.asset[2]^=1;
    return 1;
}
void reset(int b=0) { checks=reads=sets=0; behavior=b; fixture.table=table_id; fixture.contents=before; submitted={}; absent=pending=false; }
}
int main() {
    static_assert(sizeof(hostctl::ContentDescriptor)==36,"descriptor ABI");
    static_assert(sizeof(hostctl::ContentSelection)==72,"entry ABI");
    static_assert(sizeof(hostctl::ContentArray)==0x480,"array ABI");
    const uint8_t rfc[16]={0x00,0x11,0x22,0x33,0x44,0x55,0x66,0x77,0x88,0x99,0xaa,0xbb,0xcc,0xdd,0xee,0xff};
    const uint8_t guid[16]={0x33,0x22,0x11,0x00,0x55,0x44,0x77,0x66,0x88,0x99,0xaa,0xbb,0xcc,0xdd,0xee,0xff};
    uint8_t converted[16]; hostctl::ConvertUuidLayout(rfc,converted); check(std::memcmp(converted,guid,16)==0,"known native GUID vector");
    hostctl::ConvertUuidLayout(converted,converted); check(std::memcmp(converted,rfc,16)==0,"in-place inverse conversion");
    auto bytes=reinterpret_cast<uint8_t*>(&before); for(size_t i=0;i<sizeof(before);++i) bytes[i]=uint8_t((i%251)+1);
    hostctl::ContentSelection selected={}; selected.map.type=4; selected.mode.type=6; // fixture type4 is not an asserted map enum
    std::memcpy(selected.map.asset,guid,16); std::memcpy(selected.map.version,rfc,16);
    std::memcpy(selected.mode.asset,rfc,16); std::memcpy(selected.mode.version,guid,16);
    const DWORD owner=GetCurrentThreadId(); hostctl::ProviderFunctions functions={authority,current,set};
    for(int b=0;b<=10;++b) {
        reset(b); auto result=hostctl::SelectLanContent(&fixture,table_id,owner,selected,functions);
        const auto wanted=b==0 ? hostctl::ProviderResult::Selected : b==1 ? hostctl::ProviderResult::Denied : b==2 ? hostctl::ProviderResult::Pending : hostctl::ProviderResult::Failed;
        check(result==wanted,"native outcome and readback");
        if(b==0) {
            check(std::memcmp(&submitted.entries[0],&selected,sizeof(selected))==0,"selected pair copied exactly");
            check(std::memcmp(&submitted.entries[1],&before.entries[1],sizeof(before)-sizeof(selected))==0,"other fifteen entries preserved");
            check(checks==1 && reads==2 && sets==1,"authority submission readback ordering");
        }
        if(b==1 || b==2 || b==7 || b==8) check(sets==0,"early failure has no setter");
        if(b==10) check(std::memcmp(&fixture.contents.entries[0],&selected,sizeof(selected))==0,"failed notification does not claim rollback");
    }
    reset(); check(hostctl::SelectLanContent(reinterpret_cast<void*>(1),table_id,owner+1,selected,functions)==hostctl::ProviderResult::WrongThread,"thread checked before pointer");
    check(checks==0 && sets==0 && reads==0,"wrong thread no native callbacks");
    reset(); fixture.table=0; check(hostctl::SelectLanContent(&fixture,table_id,owner,selected,functions)==hostctl::ProviderResult::Invalid,"wrong provider table"); check(checks==0,"table before authority");
    reset(); auto invalid=selected; std::memset(invalid.mode.version,0,16);
    check(hostctl::SelectLanContent(&fixture,table_id,owner,invalid,functions)==hostctl::ProviderResult::Invalid,"zero version rejected"); check(checks==0 && sets==0,"invalid ID no callbacks");
    reset(); auto missing=functions; missing.Set=nullptr;
    check(hostctl::SelectLanContent(&fixture,table_id,owner,selected,missing)==hostctl::ProviderResult::Invalid,"missing binding");
    check(hostctl::SelectLanContent(nullptr,table_id,owner,selected,functions)==hostctl::ProviderResult::Invalid,"null provider");
    check(hostctl::SelectLanContent(&fixture,0,owner,selected,functions)==hostctl::ProviderResult::Invalid,"missing expected table");
    check(hostctl::SelectLanContent(&fixture,table_id,0,selected,functions)==hostctl::ProviderResult::WrongThread,"unrecorded owner thread");
    auto initial=selected; initial.map.type=2;
    auto initial_functions=functions; initial_functions.Requested=requested;
    for(int b=0;b<=6;++b) {
        reset(b); absent=true; pending=true;
        const auto result=hostctl::SelectLanContent(&fixture,table_id,owner,initial,initial_functions,true);
        const auto wanted=b==0 || b==2 ? hostctl::ProviderResult::Selected : b==1 ? hostctl::ProviderResult::Denied : hostctl::ProviderResult::Failed;
        // behavior2 deliberately keeps Current null even after successful Set.
        check(result==(b==2 ? hostctl::ProviderResult::Failed : wanted),"initialization failure/readback gates");
        if(b==0) check(std::memcmp(&submitted.entries[1],&before.entries[1],sizeof(before)-sizeof(initial))==0,"requested fifteen entries preserved");
        if(b==1) check(sets==0,"initial authority denial has no setter");
    }
    reset(); absent=true;
    check(hostctl::SelectLanContent(&fixture,table_id,owner,initial,initial_functions,true)==hostctl::ProviderResult::Selected,"empty first selection native initialization");
    hostctl::ContentArray empty={};
    check(std::memcmp(&submitted.entries[1],&empty.entries[1],sizeof(empty)-sizeof(initial))==0,"unselected entries remain zero");
    reset(); pending=true; fixture.contents.entries[15].mode.type=987;
    check(hostctl::SelectLanContent(&fixture,table_id,owner,initial,initial_functions,true)==hostctl::ProviderResult::Selected && submitted.entries[15].mode.type==987,"current takes precedence over requested");
    reset(); absent=true;
    check(hostctl::SelectLanContent(&fixture,table_id,owner,initial,functions,true)==hostctl::ProviderResult::Invalid && sets==0,"initialization requires requested binding");
    for(uint32_t type:{0u,4u,6u,10u}) { reset(); auto bad=initial; bad.map.type=type;
        check(hostctl::SelectLanContent(&fixture,table_id,owner,bad,initial_functions,true)==hostctl::ProviderResult::Invalid && checks==0,"initial map type bound to two"); }
    reset(); auto bad=initial; bad.mode.type=10;
    check(hostctl::SelectLanContent(&fixture,table_id,owner,bad,initial_functions,true)==hostctl::ProviderResult::Invalid && checks==0,"initial engine mode rejected");
    std::printf("provider tests failures=%d\n",failures); return failures ? 1 : 0;
}
