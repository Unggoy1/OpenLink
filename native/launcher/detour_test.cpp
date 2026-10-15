// Detour mechanics test: a generated function with a known RIP-free prologue.
#define WIN32_LEAN_AND_MEAN
#include <windows.h>
#include "detour.h"
#include "cbrowser.h"
#include <cstdio>
#include <cstdlib>
#include <cstring>
using Fn=uintptr_t (*)(uintptr_t);
Fn original=nullptr;
uintptr_t Handler(uintptr_t x) { return original(x)*10; }
void check(bool ok,const char* why) { if(!ok) { std::fprintf(stderr,"FAIL: %s\n",why); std::exit(1); } }
int main() {
    // Mov [rsp+8],rbx ; lea rax,[rcx+1] ; ret  (first 5 bytes are one RIP-free instruction)
    static const uint8_t code[]={0x48,0x89,0x5c,0x24,0x08,0x48,0x8d,0x41,0x01,0xc3,0xcc,0xcc,0xcc,0xcc,0xcc,0xcc};
    auto* page=static_cast<uint8_t*>(VirtualAlloc(nullptr,0x1000,MEM_COMMIT|MEM_RESERVE,PAGE_EXECUTE_READWRITE));
    check(page!=nullptr,"allocate test function");
    std::memcpy(page,code,sizeof(code));
    const Fn target=reinterpret_cast<Fn>(page);
    check(target(4)==5,"test function before detour");
    static const uint8_t wrong[]={0x90,0x90,0x90,0x90,0x90};
    uint32_t e=0;
    check(!clientjoin::detour::Install(uintptr_t(page),0,wrong,5,5,reinterpret_cast<const void*>(&Handler),&e) && e==ERROR_BAD_EXE_FORMAT,"prologue mismatch refused");
    original=reinterpret_cast<Fn>(clientjoin::detour::Install(uintptr_t(page),0,code,16,5,reinterpret_cast<const void*>(&Handler),&e));
    check(original!=nullptr && !e,"detour installed");
    check(target(4)==50,"handler runs and the trampoline reaches the original");
    check(original(7)==8,"trampoline alone is the original");
    check(clientjoin::cbrowser::TestParseGuid(),"GUID text to Windows GUID layout");
    std::puts("detour tests passed");
}
