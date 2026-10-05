#define WIN32_LEAN_AND_MEAN
#include <windows.h>
#include "choice_adapter.h"
#include <cstdio>
#include <cstring>
#include <stdexcept>

namespace {
int converts=0,sets=0,destroys=0,failures=0;
int behavior=0;
const void* expected_choice=nullptr;
void* expected_variant=nullptr;
const char* expected_path=nullptr;
void check(bool pass,const char* message) { if(!pass) { ++failures; std::fprintf(stderr,"FAIL %s\n",message); } }
void convert(const void* choice,void* value,uint8_t* skip) {
    ++converts;
    check(choice==expected_choice,"selected choice identity");
    auto bytes=static_cast<uint8_t*>(value);
    check(reinterpret_cast<uintptr_t>(bytes)%16==0,"native optional alignment");
    uint64_t capacity=0; std::memcpy(&capacity,bytes+0x60,8);
    check(capacity==7,"native optional string capacity");
    for(unsigned i=0;i<0x80;++i) if(i!=0x60) check(bytes[i]==0,"native optional initialized");
    if(behavior==3) throw std::runtime_error("conversion fixture failure");
    *skip=behavior==1 ? 1 : 0;
    bytes[0]=17;
}
uint8_t set(uint32_t selector,void* variant,const char* path,const void* value) {
    ++sets;
    check(selector==2 && variant==expected_variant && path==expected_path,"setter arguments");
    check(*static_cast<const uint8_t*>(value)==17,"converted value passed");
    if(behavior==4) throw std::runtime_error("setter fixture failure");
    return behavior==2 ? 0 : 1;
}
void destroy(void*) { ++destroys; if(behavior==5) throw std::runtime_error("cleanup fixture failure"); }
void reset(int b=0) { behavior=b; converts=sets=destroys=0; }
}

int main() {
    uint8_t choices[0x70]={}; choices[0x30]=1; choices[0x68]=2;
    int variant=0;
    hostctl::ChoiceView view={choices,2,"test.path"};
    hostctl::OptionFunctions functions={convert,set,destroy};
    expected_choice=choices+0x38; expected_variant=&variant; expected_path=view.path;
    const DWORD owner=GetCurrentThreadId();
    for(int b=0;b<=5;++b) {
        reset(b);
        auto result=hostctl::ApplySelectedChoice(view,1,&variant,2,owner,functions);
        auto expected=b==0 ? hostctl::OptionResult::Set : b==1 ? hostctl::OptionResult::Default : hostctl::OptionResult::Failed;
        check(result==expected,"result classification");
        check(converts==1 && destroys==1,"native value lifetime");
        check(sets==((b==1 || b==3) ? 0 : 1),"setter skip/exception control flow");
    }
    reset();
    check(hostctl::ApplySelectedChoice(view,2,&variant,2,owner,functions)==hostctl::OptionResult::Invalid,"choice bounds");
    check(hostctl::ApplySelectedChoice(view,1,&variant,3,owner,functions)==hostctl::OptionResult::Invalid,"selector bounds");
    check(hostctl::ApplySelectedChoice(view,1,nullptr,2,owner,functions)==hostctl::OptionResult::Invalid,"null variant");
    auto missing=functions; missing.Destroy=nullptr;
    check(hostctl::ApplySelectedChoice(view,1,&variant,2,owner,missing)==hostctl::OptionResult::Invalid,"missing cleanup binding");
    auto oversized=view; oversized.count=65537;
    check(hostctl::ApplySelectedChoice(oversized,1,&variant,2,owner,functions)==hostctl::OptionResult::Invalid,"oversized choice view");
    auto empty=view; empty.path="";
    check(hostctl::ApplySelectedChoice(empty,1,&variant,2,owner,functions)==hostctl::OptionResult::Invalid,"empty choice path");
    choices[0x68]=6;
    check(hostctl::ApplySelectedChoice(view,1,&variant,2,owner,functions)==hostctl::OptionResult::Unsupported,"unsupported value kind");
    choices[0x68]=2;
    check(hostctl::ApplySelectedChoice(view,1,&variant,2,0,functions)==hostctl::OptionResult::WrongThread,"unrecorded owner");
    HANDLE thread=CreateThread(nullptr,0,[](void* p)->DWORD {
        auto owner_id=*static_cast<DWORD*>(p);
        uint8_t choice[0x38]={}; choice[0x30]=1; int value=0;
        hostctl::ChoiceView other={choice,1,"test.path"}; hostctl::OptionFunctions f={convert,set,destroy};
        return hostctl::ApplySelectedChoice(other,0,&value,2,owner_id,f)==hostctl::OptionResult::WrongThread ? 0 : 1;
    },const_cast<DWORD*>(&owner),0,nullptr);
    check(thread!=nullptr,"test thread created");
    if(thread) { check(WaitForSingleObject(thread,5000)==WAIT_OBJECT_0,"test thread exited"); DWORD result=1; GetExitCodeThread(thread,&result); check(result==0,"wrong-thread operation rejected"); CloseHandle(thread); }
    check(converts==0 && sets==0 && destroys==0,"rejected inputs made no native calls");
    std::printf("choice adapter fixture failures=%d\n",failures);
    return failures ? 1 : 0;
}
