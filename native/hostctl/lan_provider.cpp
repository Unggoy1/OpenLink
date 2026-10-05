#define WIN32_LEAN_AND_MEAN
#include <windows.h>
#include "lan_provider.h"
#include <cstring>

namespace hostctl {
void ConvertUuidLayout(const uint8_t* input,uint8_t* output) noexcept {
    const uint8_t converted[16]={input[3],input[2],input[1],input[0],input[5],input[4],input[7],input[6],
        input[8],input[9],input[10],input[11],input[12],input[13],input[14],input[15]};
    std::memcpy(output,converted,sizeof(converted));
}
namespace {
bool nonzero(const uint8_t* bytes) noexcept {
    uint8_t bits=0; for(unsigned i=0;i<16;++i) bits=uint8_t(bits|bytes[i]); return bits!=0;
}
bool valid(const ContentDescriptor& descriptor) noexcept {
    return nonzero(descriptor.asset) && nonzero(descriptor.version);
}
}
ProviderResult SelectLanContent(void* provider,uintptr_t expected_table,
    uint32_t engine_thread_id,const ContentSelection& selection,
    const ProviderFunctions& functions,bool initialize_if_absent) noexcept {
    static_assert(sizeof(uintptr_t)==8,"B002 x64 provider ABI");
    if(!engine_thread_id || GetCurrentThreadId()!=engine_thread_id) return ProviderResult::WrongThread;
    if(!provider || !expected_table || !functions.Authority || !functions.Current || !functions.Set ||
        !valid(selection.map) || !valid(selection.mode) ||
        (initialize_if_absent && (selection.map.type!=2 || selection.mode.type!=6 || !functions.Requested))) return ProviderResult::Invalid;
    uintptr_t observed_table=0; std::memcpy(&observed_table,provider,sizeof(observed_table));
    if(observed_table!=expected_table) return ProviderResult::Invalid;
    try {
        if(!functions.Authority(provider)) return ProviderResult::Denied;
        const auto current=functions.Current(provider);
        if(!current && !initialize_if_absent) return ProviderResult::Pending;
        const auto seed=current ? current : functions.Requested(provider);
        alignas(16) ContentArray desired={};
        if(seed) std::memcpy(&desired,seed,sizeof(desired));
        desired.entries[0]=selection;
        if(!functions.Set(provider,&desired)) return ProviderResult::Failed;
        const auto observed=functions.Current(provider);
        if(!observed || std::memcmp(observed,&desired,sizeof(desired))!=0) return ProviderResult::Failed;
        return ProviderResult::Selected;
    } catch(...) { return ProviderResult::Failed; }
}
}
