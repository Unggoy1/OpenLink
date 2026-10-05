#pragma once
#include <cstddef>
#include <cstdint>
#include <cstring>

namespace hostctl {
// B002 LAN beacon object, global 144dbfef0 (O080, A055). 142e92550 fills it once
// at LAN-server net start-up: +0x08 broadcast address 255.255.255.255, +0x18
// 0x41bcd, +0x1c 0xff, +0x20 u16 0xd or 0x34, +0x24 the PC name (UTF-16, 48 units
// incl. terminator, from GetComputerNameA), +0xa0 1 (beacon on). 142e9954c
// re-serializes from +0x20 on every beacon, so writing +0x24 renames the server
// from the next beacon. The 16 bytes at +0x84 are not checked: at runtime they
// differ from 143b458e0, which the decompiler shows as their source (R022).
constexpr size_t kBeaconSnapshot=0xa1,kBeaconName=0x24,kBeaconNameUnits=48;

// Widens a set-name payload: 1-47 printable ASCII bytes, then zeros to 48 bytes.
inline bool EncodeBeaconName(const uint8_t* ascii,uint16_t* units) noexcept {
    size_t n=0;
    while(n<kBeaconNameUnits && ascii[n]) {
        if(ascii[n]<0x20 || ascii[n]>0x7e) return false;
        ++n;
    }
    if(n==0 || n==kBeaconNameUnits) return false;
    for(size_t i=n;i<kBeaconNameUnits;++i) if(ascii[i]) return false;
    for(size_t i=0;i<kBeaconNameUnits;++i) units[i]=ascii[i];
    return true;
}

enum class BeaconCheck { Ok, NotStarted, Mismatch };
// Checks a snapshot of the beacon object's first kBeaconSnapshot bytes against
// the fields 142e92550 writes, and that the current name is terminated.
inline BeaconCheck CheckBeacon(const uint8_t* beacon) noexcept {
    if(beacon[0xa0]==0) return BeaconCheck::NotStarted;
    uint32_t broadcast=0,magic=0; uint16_t kind=0;
    std::memcpy(&broadcast,beacon+0x08,4); std::memcpy(&magic,beacon+0x18,4); std::memcpy(&kind,beacon+0x20,2);
    if(beacon[0xa0]!=1 || broadcast!=0xffffffffu || magic!=0x41bcdu || beacon[0x1c]!=0xff || (kind!=0xd && kind!=0x34))
        return BeaconCheck::Mismatch;
    for(size_t i=0;i<kBeaconNameUnits;++i) {
        uint16_t unit=0; std::memcpy(&unit,beacon+kBeaconName+2*i,2);
        if(!unit) return BeaconCheck::Ok;
    }
    return BeaconCheck::Mismatch;
}
}
