#define WIN32_LEAN_AND_MEAN
#include <windows.h>
#include "choice_adapter.h"
#include <cstring>

namespace hostctl {
OptionResult ApplySelectedChoice(const ChoiceView& view,uint64_t index,void* variant,
    uint32_t selector,uint32_t engine_thread_id,const OptionFunctions& functions) noexcept {
    if(engine_thread_id==0 || GetCurrentThreadId()!=engine_thread_id) return OptionResult::WrongThread;
    // Original selected indices are uint16. Bound the view before pointer math.
    if(!view.choices || !view.path || !*view.path || !variant || selector>2 || view.count==0 || view.count>65536 || index>=view.count || !functions.Convert || !functions.Set || !functions.Destroy) return OptionResult::Invalid;
    const uint8_t* choice=view.choices+size_t(index)*0x38;
    if(choice[0x30]>5) return OptionResult::Unsupported;
    alignas(16) uint8_t optional_value[0x80]={};
    const uint64_t string_capacity=7;
    std::memcpy(optional_value+0x60,&string_capacity,sizeof(string_capacity));
    OptionResult result=OptionResult::Failed;
    try {
        uint8_t skip=0;
        functions.Convert(choice,optional_value,&skip);
        if(skip) result=OptionResult::Default;
        else if(functions.Set(selector,variant,view.path,optional_value)!=0) result=OptionResult::Set;
    } catch(...) {
        // Native C++ failures do not cross into the controller. Borrowed native
        // memory/build/ABI are the backend's responsibility; this is not an SEH
        // recovery mechanism for invalid game pointers or an unknown build.
        result=OptionResult::Failed;
    }
    try { functions.Destroy(optional_value); }
    catch(...) { result=OptionResult::Failed; }
    return result;
}
}
