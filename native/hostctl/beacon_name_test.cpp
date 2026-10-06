#include "beacon_name.h"
#include <cstdio>
#include <cstring>

namespace {
// A beacon object as 142e92550 leaves it, with the PC name "DESKTOP-1".
void Fill(uint8_t* b) {
    std::memset(b,0,hostctl::kBeaconSnapshot);
    std::memset(b+0x08,0xff,4);
    const uint32_t magic=0x41bcd; std::memcpy(b+0x18,&magic,4); b[0x1c]=0xff;
    const uint16_t kind=0x34; std::memcpy(b+0x20,&kind,2);
    const char* pc="DESKTOP-1"; for(size_t i=0;pc[i];++i) b[hostctl::kBeaconName+2*i]=uint8_t(pc[i]);
    b[0x97]=1; b[0xa0]=1;
}
}

int main() {
    int failures=0;
    const auto check=[&](bool ok,const char* label) { if(!ok) { ++failures; std::fprintf(stderr,"FAIL: %s\n",label); } };
    uint8_t beacon[hostctl::kBeaconSnapshot];

    Fill(beacon);
    check(hostctl::CheckBeacon(beacon)==hostctl::BeaconCheck::Ok,"initialized beacon accepted");
    const uint16_t kind=0xd; std::memcpy(beacon+0x20,&kind,2);
    check(hostctl::CheckBeacon(beacon)==hostctl::BeaconCheck::Ok,"beacon kind 0xd accepted");
    Fill(beacon); beacon[0xa0]=0;
    check(hostctl::CheckBeacon(beacon)==hostctl::BeaconCheck::NotStarted,"beacon before start-up is not started");
    uint8_t zero[hostctl::kBeaconSnapshot]={};
    check(hostctl::CheckBeacon(zero)==hostctl::BeaconCheck::NotStarted,"zeroed global is not started");
    const size_t fields[]={0x08,0x18,0x1c,0x20,0xa0};
    for(const auto at:fields) {
        Fill(beacon); beacon[at]^=0x40;
        check(hostctl::CheckBeacon(beacon)==hostctl::BeaconCheck::Mismatch,"changed beacon field rejected");
    }
    Fill(beacon);
    for(size_t i=0;i<hostctl::kBeaconNameUnits;++i) beacon[hostctl::kBeaconName+2*i]='x';
    check(hostctl::CheckBeacon(beacon)==hostctl::BeaconCheck::Mismatch,"unterminated name rejected");

    uint16_t units[hostctl::kBeaconNameUnits];
    uint8_t name[hostctl::kBeaconNameUnits]={};
    std::memcpy(name,"Bob's Server #1 ~",17);
    check(hostctl::EncodeBeaconName(name,units),"printable name accepted");
    bool widened=true;
    for(size_t i=0;i<hostctl::kBeaconNameUnits;++i) widened=widened && units[i]==name[i];
    check(widened && units[16]=='~' && units[17]==0 && units[47]==0,"name widened and zero-filled");
    std::memset(name,'y',47); name[47]=0;
    check(hostctl::EncodeBeaconName(name,units) && units[46]=='y' && units[47]==0,"47 characters accepted");
    name[47]='y';
    check(!hostctl::EncodeBeaconName(name,units),"48 characters rejected");
    std::memset(name,0,sizeof(name));
    check(!hostctl::EncodeBeaconName(name,units),"empty name rejected");
    std::memcpy(name,"a\x01" "b",3);
    check(!hostctl::EncodeBeaconName(name,units),"control character rejected");
    std::memcpy(name,"a\xe9",2); name[2]=0;
    check(!hostctl::EncodeBeaconName(name,units),"non-ASCII byte rejected");
    std::memset(name,0,sizeof(name)); std::memcpy(name,"ab",2); name[5]='c';
    check(!hostctl::EncodeBeaconName(name,units),"nonzero padding rejected");

    std::printf("beacon name checks: failures=%d\n",failures);
    return failures?1:0;
}
