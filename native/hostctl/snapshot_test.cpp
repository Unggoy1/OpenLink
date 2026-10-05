#define WIN32_LEAN_AND_MEAN
#include <windows.h>
#include "saved_choices.h"
#include "choice_adapter.h"
#include <cstdio>
#include <cstring>
#include <stdexcept>
#include <utility>

namespace {
int failures=0,read_count=0,convert_count=0,set_count=0,cleanup_count=0;
void check(bool pass,const char* what) { if(!pass) { ++failures; std::fprintf(stderr,"FAIL %s\n",what); } }
struct Memory {
    struct Block { uintptr_t base; std::vector<uint8_t> bytes; };
    std::vector<Block> blocks;
    uintptr_t next=0x1000;
    uintptr_t allocate(size_t size) { uintptr_t address=next; next+=(size+0xff)&~uintptr_t(0xff); blocks.push_back({address,std::vector<uint8_t>(size)}); return address; }
    bool copy(uintptr_t address,void* output,size_t size) const {
        ++read_count;
        for(const auto& b:blocks) if(address>=b.base && address-b.base<=b.bytes.size() && size<=b.bytes.size()-(address-b.base)) { std::memcpy(output,b.bytes.data()+(address-b.base),size); return true; }
        return false;
    }
    void write(uintptr_t address,const void* bytes,size_t size) {
        for(auto& b:blocks) if(address>=b.base && address-b.base<=b.bytes.size() && size<=b.bytes.size()-(address-b.base)) { std::memcpy(b.bytes.data()+(address-b.base),bytes,size); return; }
        throw std::runtime_error("fixture write outside block");
    }
    template<class T> void put(uintptr_t address,T value) { write(address,&value,sizeof(value)); }
    void string(uintptr_t address,const std::string& value) {
        uint8_t native[32]={}; uint64_t length=value.size(),capacity=15;
        if(length<=15) { std::memcpy(native,value.c_str(),value.size()+1); }
        else { uintptr_t heap=allocate(value.size()+1); write(heap,value.c_str(),value.size()+1); std::memcpy(native,&heap,8); capacity=length; }
        std::memcpy(native+16,&length,8); std::memcpy(native+24,&capacity,8); write(address,native,32);
    }
};
bool read(void* context,uintptr_t address,void* output,size_t size) { return static_cast<Memory*>(context)->copy(address,output,size); }
struct Fixture {
    Memory memory;
    uintptr_t tree,head,a,b,c,root,submenu,leaf_a,leaf_b,leaf_c;
    uintptr_t choices_a,choices_b,choices_c;
    Fixture() {
        tree=memory.allocate(24); head=memory.allocate(32);
        a=memory.allocate(0xa0); b=memory.allocate(0xa0); c=memory.allocate(0xa0);
        root=memory.allocate(32); submenu=memory.allocate(0xc0);
        leaf_a=memory.allocate(0xc0); leaf_b=memory.allocate(0xc0); leaf_c=memory.allocate(0xc0);
        choices_a=memory.allocate(0x70); choices_b=memory.allocate(0x70); choices_c=memory.allocate(0x70);
        memory.put(tree,head); memory.put<uint64_t>(tree+8,3);
        memory.put(head,a); memory.put(head+8,b); memory.put(head+16,c); memory.put<uint8_t>(head+0x19,1);
        node(a,head,b,head,"SETTING.A",0x123400000001ull);
        node(b,a,head,c,"setting.b",0);
        node(c,head,b,head,"nested.setting.c",1);
        vector(root+8,{leaf_a,submenu,leaf_b});
        memory.put<uint32_t>(submenu+8,0); vector(submenu+0x70,{leaf_c});
        leaf(leaf_a,"setting.a",choices_a); leaf(leaf_b,"setting.b",choices_b); leaf(leaf_c,"Nested.Setting.C",choices_c);
        memory.put<uint8_t>(choices_a+0x30,1); memory.put<uint8_t>(choices_a+0x68,2); memory.put<uint32_t>(choices_a+0x60,17);
        memory.put<uint8_t>(choices_b+0x30,0); memory.put<uint8_t>(choices_b+0x68,1);
        memory.put<uint8_t>(choices_c+0x30,1); memory.put<uint8_t>(choices_c+0x68,5);
    }
    void vector(uintptr_t at,std::initializer_list<uintptr_t> entries) { uintptr_t data=memory.allocate(entries.size()*8); memory.write(data,entries.begin(),entries.size()*8); memory.put(at,data); memory.put(at+8,data+entries.size()*8); memory.put(at+16,data+entries.size()*8); }
    void node(uintptr_t at,uintptr_t left,uintptr_t parent,uintptr_t right,const std::string& path,uint64_t index) { memory.put(at,left); memory.put(at+8,parent); memory.put(at+16,right); memory.string(at+0x20,path); memory.put(at+0x40,index); }
    void leaf(uintptr_t at,const std::string& path,uintptr_t choices) { memory.put<uint32_t>(at+8,1); memory.string(at+0x70,path); memory.put(at+0xa0,choices); memory.put(at+0xa8,choices+0x70); }
    hostctl::SnapshotResult snapshot(std::vector<hostctl::SavedChoice>& output) { return hostctl::SnapshotSavedChoices(root,tree,GetCurrentThreadId(),{&memory,read},output); }
};
void convert(const void* choice,void*,uint8_t* skip) { ++convert_count; auto p=static_cast<const uint8_t*>(choice); check(p[0x30]==2,"copied selected kind"); uint32_t value=0; std::memcpy(&value,p+0x28,4); check(value==17,"copied selected value"); *skip=0; }
uint8_t set(uint32_t selector,void*,const char* path,const void*) { ++set_count; check(selector==2 && std::strcmp(path,"setting.a")==0,"owned path passed"); return 1; }
void destroy(void*) { ++cleanup_count; }
template<class Mutate> void rejected(Mutate mutate,hostctl::SnapshotResult expected,const char* name) {
    Fixture f; mutate(f); std::vector<hostctl::SavedChoice> output(1);
    check(f.snapshot(output)==expected,name); check(output.empty(),"failed snapshot publishes no partial choices");
}
}
int main() {
    Fixture f; std::vector<hostctl::SavedChoice> output;
    check(f.snapshot(output)==hostctl::SnapshotResult::Ready,"nested schema and three-node tree");
    check(output.size()==3,"all saved paths matched");
    if(output.size()==3) {
        check(output[0].path=="setting.a" && output[0].selected_index==1,"case folding and low16 index");
        check(output[1].path=="Nested.Setting.C" && output[1].selected_index==1,"schema traversal order and heap path");
        check(output[2].path=="setting.b" && output[2].selected_index==0,"default choice retained");
        // Destroy/mutate source: snapshot is independent of borrowed source memory.
        f.memory.blocks.clear(); int variant=0;
        hostctl::ChoiceView view={output[0].choice.data(),1,output[0].path.c_str()};
        check(hostctl::ApplySelectedChoice(view,0,&variant,2,GetCurrentThreadId(),{convert,set,destroy})==hostctl::OptionResult::Set,"snapshot feeds selected-choice helper");
        check(convert_count==1 && set_count==1 && cleanup_count==1,"interop callbacks once");
    }
    rejected([](Fixture& x) { x.memory.put<uint64_t>(x.tree+8,4); },hostctl::SnapshotResult::Invalid,"stored count mismatch");
    rejected([](Fixture& x) { x.memory.put(x.head,x.b); },hostctl::SnapshotResult::Invalid,"wrong leftmost");
    rejected([](Fixture& x) { x.memory.put(x.head+16,x.b); },hostctl::SnapshotResult::Invalid,"wrong rightmost");
    rejected([](Fixture& x) { x.memory.put(x.b,x.b); },hostctl::SnapshotResult::Invalid,"tree cycle");
    rejected([](Fixture& x) { x.memory.put(x.a+8,x.c); },hostctl::SnapshotResult::Invalid,"broken parent");
    rejected([](Fixture& x) { x.memory.put(x.c+16,uintptr_t(0xdead000)); },hostctl::SnapshotResult::Invalid,"unreadable link");
    rejected([](Fixture& x) { x.memory.put<uint8_t>(x.c+0x19,1); },hostctl::SnapshotResult::Invalid,"unexpected sentinel");
    rejected([](Fixture& x) { x.memory.put<uint64_t>(x.tree+8,4097); },hostctl::SnapshotResult::Invalid,"tree count bound");
    rejected([](Fixture& x) { x.vector(x.submenu+0x70,{x.submenu}); },hostctl::SnapshotResult::Invalid,"schema cycle");
    rejected([](Fixture& x) { x.vector(x.root+8,{x.leaf_a,x.leaf_a}); },hostctl::SnapshotResult::Invalid,"schema duplicate ownership");
    rejected([](Fixture& x) { x.memory.put(x.root+16,uintptr_t(1)); },hostctl::SnapshotResult::Invalid,"reversed schema range");
    rejected([](Fixture& x) { x.memory.put<uint64_t>(x.a+0x40,2); },hostctl::SnapshotResult::Invalid,"selected index bounds");
    rejected([](Fixture& x) { x.memory.put<uint8_t>(x.choices_a+0x68,6); },hostctl::SnapshotResult::Unsupported,"unsupported native kind");
    rejected([](Fixture& x) { x.memory.string(x.a+0x20,"absent.path"); },hostctl::SnapshotResult::Invalid,"unmatched saved path");
    rejected([](Fixture& x) { x.memory.string(x.c+0x20,"setting.A"); },hostctl::SnapshotResult::Invalid,"duplicate normalized saved path");
    rejected([](Fixture& x) { x.memory.string(x.leaf_c+0x70,"SETTING.A"); },hostctl::SnapshotResult::Invalid,"duplicate normalized schema path");
    rejected([](Fixture& x) { x.memory.string(x.a+0x20,""); },hostctl::SnapshotResult::Invalid,"empty saved path");
    rejected([](Fixture& x) { x.memory.string(x.a+0x20,std::string(128,'a')); },hostctl::SnapshotResult::Unsupported,"long path");
    rejected([](Fixture& x) { x.memory.string(x.a+0x20,std::string("\xC3")); },hostctl::SnapshotResult::Unsupported,"unverified non-ASCII folding");
    rejected([](Fixture& x) { x.memory.string(x.a+0x20,"a.b.c.d.e.f.g"); },hostctl::SnapshotResult::Unsupported,"path token bound");
    rejected([](Fixture& x) { x.memory.put<uint64_t>(x.a+0x30,20); },hostctl::SnapshotResult::Invalid,"native string length exceeds capacity");
    rejected([](Fixture& x) { x.memory.put(x.leaf_a+0xa8,x.choices_a+0x39); },hostctl::SnapshotResult::Invalid,"choice range stride");
    rejected([](Fixture& x) { x.memory.put<uint32_t>(x.leaf_a+8,2); },hostctl::SnapshotResult::Invalid,"ignored schema kind cannot consume saved entry");
    rejected([](Fixture& x) { x.memory.put(x.leaf_a+0xa8,x.choices_a+uintptr_t(65537)*0x38); },hostctl::SnapshotResult::Invalid,"choice count bound");
    rejected([](Fixture& x) { x.memory.put(x.root+16,uintptr_t(0xfffffffffffffff8ull)); },hostctl::SnapshotResult::Invalid,"schema count bound");
    rejected([](Fixture& x) { x.memory.put<uint8_t>(x.head+0x19,0); },hostctl::SnapshotResult::Invalid,"header sentinel missing");
    rejected([](Fixture& x) { x.memory.put<uint8_t>(x.a+0x20+9,1); },hostctl::SnapshotResult::Invalid,"string terminator missing");
    rejected([](Fixture& x) {
        uintptr_t nodes[65]; for(auto& node:nodes) node=x.memory.allocate(0xa0);
        for(size_t i=0;i<65;++i) x.node(nodes[i],i+1<65 ? nodes[i+1] : x.head,i ? nodes[i-1] : x.head,x.head,"deep.setting",0);
        x.memory.put<uint64_t>(x.tree+8,65); x.memory.put(x.head,nodes[64]); x.memory.put(x.head+8,nodes[0]); x.memory.put(x.head+16,nodes[0]);
    },hostctl::SnapshotResult::Invalid,"native tree depth bound");
    rejected([](Fixture& x) {
        uintptr_t next=x.leaf_c;
        for(size_t i=0;i<65;++i) { auto menu=x.memory.allocate(0xc0); x.vector(menu+0x70,{next}); next=menu; }
        x.vector(x.root+8,{x.leaf_a,next,x.leaf_b});
    },hostctl::SnapshotResult::Invalid,"native schema depth bound");
    {
        Fixture extra; auto ignored=extra.memory.allocate(0xc0); extra.memory.put<uint32_t>(ignored+8,2);
        extra.vector(extra.root+8,{extra.leaf_a,extra.submenu,extra.leaf_b,ignored});
        check(extra.snapshot(output)==hostctl::SnapshotResult::Ready && output.size()==3,"unselected other schema kind ignored");
    }
    {
        Fixture stock; stock.memory.put<uint64_t>(stock.tree+8,0); stock.memory.put(stock.head,stock.head); stock.memory.put(stock.head+8,stock.head); stock.memory.put(stock.head+16,stock.head);
        check(stock.snapshot(output)==hostctl::SnapshotResult::Ready && output.empty(),"empty saved tree is valid");
    }
    {
        Fixture other; read_count=0;
        check(hostctl::SnapshotSavedChoices(other.root,other.tree,0,{&other.memory,read},output)==hostctl::SnapshotResult::WrongThread,"unrecorded engine thread");
        check(read_count==0 && output.empty(),"thread gate before memory reads");
        check(hostctl::SnapshotSavedChoices(other.root,other.tree,GetCurrentThreadId(),{&other.memory,nullptr},output)==hostctl::SnapshotResult::Invalid,"missing reader");
        hostctl::NativeReader throwing={nullptr,[](void*,uintptr_t,void*,size_t)->bool { throw std::runtime_error("read fixture failure"); }};
        check(hostctl::SnapshotSavedChoices(other.root,other.tree,GetCurrentThreadId(),throwing,output)==hostctl::SnapshotResult::Failed && output.empty(),"reader exception contains failure");
        check(hostctl::SnapshotSavedChoices(uintptr_t(0xfffffffffffffff8ull),other.tree,GetCurrentThreadId(),{&other.memory,read},output)==hostctl::SnapshotResult::Invalid && output.empty(),"address arithmetic overflow");
        DWORD owner=GetCurrentThreadId();
        HANDLE thread=CreateThread(nullptr,0,[](void* p)->DWORD {
            auto owner_id=*static_cast<DWORD*>(p); std::vector<hostctl::SavedChoice> result;
            return hostctl::SnapshotSavedChoices(0x1000,0x2000,owner_id,{nullptr,[](void*,uintptr_t,void*,size_t)->bool { return false; }},result)==hostctl::SnapshotResult::WrongThread ? 0 : 1;
        },&owner,0,nullptr);
        check(thread!=nullptr,"snapshot wrong-thread fixture created");
        if(thread) { check(WaitForSingleObject(thread,5000)==WAIT_OBJECT_0,"snapshot wrong-thread fixture exited"); DWORD code=1; GetExitCodeThread(thread,&code); check(code==0,"other thread rejected"); CloseHandle(thread); }
    }
    std::printf("saved choice snapshot fixture failures=%d\n",failures); return failures ? 1 : 0;
}
