#define WIN32_LEAN_AND_MEAN
#define NOMINMAX
#include <windows.h>
#include "saved_choices.h"
#include <cstring>
#include <limits>
#include <unordered_map>
#include <unordered_set>
#include <utility>

namespace hostctl {
namespace {
static_assert(sizeof(uintptr_t)==8,"B002 native layouts require x64");
constexpr size_t MaxNodes=4096,MaxDepth=64,MaxChoices=65536,MaxPath=127;
uintptr_t add(uintptr_t address,size_t offset) {
    if(!address || address>std::numeric_limits<uintptr_t>::max()-offset) throw SnapshotResult::Invalid;
    return address+offset;
}
bool aligned(uintptr_t address) { return address && address%8==0; }
template<class T> T field(const uint8_t* bytes,size_t offset) { T value{}; std::memcpy(&value,bytes+offset,sizeof(value)); return value; }
struct Parameter { std::string key,path; uintptr_t choices; size_t count; };
class Snapshot {
    const NativeReader& reader_;
    std::unordered_set<uintptr_t> tree_seen_,schema_seen_;
    std::unordered_set<std::string> schema_keys_;
    std::vector<uintptr_t> inorder_;
    std::unordered_map<std::string,uint16_t> entries_;
    std::vector<Parameter> parameters_;
    uintptr_t head_=0;
    size_t declared_count_=0;
    void copy(uintptr_t address,void* destination,size_t size) {
        if(!address || !size || address>std::numeric_limits<uintptr_t>::max()-(size-1) ||
           !reader_.Copy(reader_.context,address,destination,size)) throw SnapshotResult::Invalid;
    }
    template<class T> T get(uintptr_t address) { T value{}; copy(address,&value,sizeof(value)); return value; }
    std::string path(uintptr_t address) {
        uint8_t native[32]; copy(address,native,sizeof(native));
        const auto length=field<uint64_t>(native,16),capacity=field<uint64_t>(native,24);
        if(!length || length>capacity) throw SnapshotResult::Invalid;
        if(length>MaxPath) throw SnapshotResult::Unsupported;
        char bytes[MaxPath+1]={};
        if(capacity<=15) {
            if(capacity!=15 || length>15) throw SnapshotResult::Invalid;
            std::memcpy(bytes,native,static_cast<size_t>(length)+1);
        } else { copy(field<uintptr_t>(native,0),bytes,static_cast<size_t>(length)+1); }
        if(bytes[length]!=0) throw SnapshotResult::Invalid;
        size_t tokens=0; bool token=false;
        for(size_t i=0;i<length;++i) {
            const auto ch=static_cast<unsigned char>(bytes[i]);
            if(ch>=128) throw SnapshotResult::Unsupported;
            if(ch<32 || ch==127) throw SnapshotResult::Invalid;
            if(ch=='.' || ch=='[' || ch==']') token=false;
            else if(!token) { ++tokens; token=true; }
        }
        if(!tokens) throw SnapshotResult::Invalid;
        if(tokens>6) throw SnapshotResult::Unsupported;
        return std::string(bytes,static_cast<size_t>(length));
    }
    static std::string key(std::string value) {
        // ASCII subset corroborates native CRT tolower without assuming locale.
        for(auto& ch:value) if(ch>='A' && ch<='Z') ch=static_cast<char>(ch-'A'+'a');
        return value;
    }
    void tree(uintptr_t node,uintptr_t parent,size_t depth) {
        if(node==head_) return;
        if(!aligned(node) || depth>=MaxDepth || tree_seen_.size()>=declared_count_ || !tree_seen_.insert(node).second) throw SnapshotResult::Invalid;
        uint8_t bytes[0x48]; copy(node,bytes,sizeof(bytes));
        if(bytes[0x19]!=0 || field<uintptr_t>(bytes,8)!=parent) throw SnapshotResult::Invalid;
        const auto left=field<uintptr_t>(bytes,0),right=field<uintptr_t>(bytes,16);
        tree(left,node,depth+1);
        inorder_.push_back(node);
        const auto normalized=key(path(add(node,0x20)));
        const auto index=static_cast<uint16_t>(field<uint64_t>(bytes,0x40)&0xffff);
        if(!entries_.emplace(normalized,index).second) throw SnapshotResult::Invalid;
        tree(right,node,depth+1);
    }
    void schema(uintptr_t vector,size_t depth) {
        if(depth>=MaxDepth) throw SnapshotResult::Invalid;
        uintptr_t range[2]; copy(vector,range,sizeof(range));
        if(!range[0] && !range[1]) return;
        if(!aligned(range[0]) || range[1]<range[0] || (range[1]-range[0])%8!=0) throw SnapshotResult::Invalid;
        const auto count=(range[1]-range[0])/8;
        if(count>MaxNodes) throw SnapshotResult::Invalid;
        for(size_t i=0;i<count;++i) {
            const auto node=get<uintptr_t>(add(range[0],i*8));
            if(!aligned(node) || schema_seen_.size()>=MaxNodes || !schema_seen_.insert(node).second) throw SnapshotResult::Invalid;
            const auto type=get<uint32_t>(add(node,8));
            if(type==0) { schema(add(node,0x70),depth+1); }
            else if(type==1) {
                auto name=path(add(node,0x70)); auto normalized=key(name);
                if(!schema_keys_.insert(normalized).second) throw SnapshotResult::Invalid;
                uintptr_t choices[2]; copy(add(node,0xa0),choices,sizeof(choices));
                if(choices[1]<choices[0] || (choices[1]-choices[0])%0x38!=0) throw SnapshotResult::Invalid;
                const auto choice_count=(choices[1]-choices[0])/0x38;
                if(choice_count>MaxChoices || (choice_count && !aligned(choices[0]))) throw SnapshotResult::Invalid;
                parameters_.push_back({std::move(normalized),std::move(name),choices[0],choice_count});
            }
            // Other schema node kinds are ignored by the observed native caller.
        }
    }
public:
    explicit Snapshot(const NativeReader& reader):reader_(reader) {}
    void build(uintptr_t schema_root,uintptr_t saved_tree,std::vector<SavedChoice>& output) {
        uintptr_t tree_state[2]; copy(saved_tree,tree_state,sizeof(tree_state));
        head_=tree_state[0]; declared_count_=tree_state[1];
        if(!aligned(head_) || declared_count_>MaxNodes) throw SnapshotResult::Invalid;
        uint8_t header[0x1a]; copy(head_,header,sizeof(header));
        if(header[0x19]!=1) throw SnapshotResult::Invalid;
        const auto minimum=field<uintptr_t>(header,0),root=field<uintptr_t>(header,8),maximum=field<uintptr_t>(header,16);
        if(!declared_count_) {
            if(minimum!=head_ || root!=head_ || maximum!=head_) throw SnapshotResult::Invalid;
            return;
        }
        tree(root,head_,0);
        if(inorder_.size()!=declared_count_ || inorder_.front()!=minimum || inorder_.back()!=maximum) throw SnapshotResult::Invalid;
        schema(add(schema_root,8),0);
        for(const auto& p:parameters_) {
            auto found=entries_.find(p.key); if(found==entries_.end()) continue;
            if(found->second>=p.count) throw SnapshotResult::Invalid;
            SavedChoice selected; selected.path=p.path; selected.selected_index=found->second;
            copy(add(p.choices,static_cast<size_t>(found->second)*0x38),selected.choice.data(),selected.choice.size());
            if(selected.choice[0x30]>5) throw SnapshotResult::Unsupported;
            output.push_back(std::move(selected)); entries_.erase(found);
        }
        if(!entries_.empty()) throw SnapshotResult::Invalid;
    }
};
}
SnapshotResult SnapshotSavedChoices(uintptr_t schema_root,uintptr_t saved_tree,
    uint32_t engine_thread_id,const NativeReader& reader,std::vector<SavedChoice>& output) noexcept {
    output.clear();
    if(!engine_thread_id || GetCurrentThreadId()!=engine_thread_id) return SnapshotResult::WrongThread;
    if(!aligned(schema_root) || !aligned(saved_tree) || !reader.Copy) return SnapshotResult::Invalid;
    try {
        std::vector<SavedChoice> pending;
        Snapshot(reader).build(schema_root,saved_tree,pending);
        output.swap(pending); return SnapshotResult::Ready;
    } catch(SnapshotResult result) { return result; }
      catch(...) { return SnapshotResult::Failed; }
}
}
