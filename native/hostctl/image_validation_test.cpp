#define WIN32_LEAN_AND_MEAN
#include <windows.h>
#include "image_validation.h"
#include "simulation_state.h"
#include <cstdio>
#include <cstring>

int main() {
    const auto base=reinterpret_cast<uintptr_t>(GetModuleHandleW(nullptr));
    IMAGE_DOS_HEADER dos={}; IMAGE_NT_HEADERS64 nt={};
    std::memcpy(&dos,reinterpret_cast<const void*>(base),sizeof(dos));
    if(dos.e_lfanew<0 || dos.e_lfanew>0x100000) return 2;
    std::memcpy(&nt,reinterpret_cast<const void*>(base+uintptr_t(dos.e_lfanew)),sizeof(nt));
    int failures=0;
    const auto check=[&](bool ok,const char* label) { if(!ok) { ++failures; std::fprintf(stderr,"FAIL: %s\n",label); } };
    check(nt.OptionalHeader.ImageBase==base,"actual owned mapped header matches its module base");
    check(hostctl::ValidateLoadedHeader(base,dos,nt,0),"actual owned mapped module accepted");
    auto changed=nt;
    changed.OptionalHeader.ImageBase=base+0x10000;
    check(!hostctl::ValidateLoadedHeader(base,dos,changed,0),"wrong loaded base rejected");
    changed=nt; changed.FileHeader.Machine=IMAGE_FILE_MACHINE_I386;
    check(!hostctl::ValidateLoadedHeader(base,dos,changed,0),"wrong machine rejected");
    changed=nt; changed.OptionalHeader.Magic=IMAGE_NT_OPTIONAL_HDR32_MAGIC;
    check(!hostctl::ValidateLoadedHeader(base,dos,changed,0),"wrong optional header rejected");
    changed=nt; changed.Signature=0;
    check(!hostctl::ValidateLoadedHeader(base,dos,changed,0),"wrong NT signature rejected");
    auto changed_dos=dos; changed_dos.e_magic=0;
    check(!hostctl::ValidateLoadedHeader(base,changed_dos,nt,0),"wrong DOS signature rejected");
    changed_dos=dos; changed_dos.e_lfanew=-1;
    check(!hostctl::ValidateLoadedHeader(base,changed_dos,nt,0),"negative NT offset rejected");
    changed_dos=dos; changed_dos.e_lfanew=0x100001;
    check(!hostctl::ValidateLoadedHeader(base,changed_dos,nt,0),"excessive NT offset rejected");
    check(!hostctl::ValidateLoadedHeader(base,dos,nt,nt.OptionalHeader.SizeOfImage),"required address outside module rejected");
    check(!hostctl::ValidateLoadedHeader(0,dos,nt,0),"zero module rejected");
    check(hostctl::SimulationAvailable(7),"observed LAN enum7 admits provider binding");
    check(hostctl::SimulationAvailable(6),"cold enum6 admits provider binding");
    check(hostctl::SimulationAvailable(4) && hostctl::SimulationAvailable(5),"hot valid enums retained");
    const int32_t unavailable[]={INT32_MIN,-1,0,1,2,3,8,9,INT32_MAX};
    for(const auto state:unavailable) {
        check(!hostctl::SimulationAvailable(state),"unavailable simulation enum rejected");
    }
    std::printf("loaded header checks: failures=%d base=%p header=%llx\n",failures,
        reinterpret_cast<void*>(base),static_cast<unsigned long long>(nt.OptionalHeader.ImageBase));
    return failures?1:0;
}
