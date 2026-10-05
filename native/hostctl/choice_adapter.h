#pragma once
#include <cstddef>
#include <cstdint>

namespace hostctl {
// Borrowed views/bindings from a build/role/lobby-validated engine backend.
// No IPC request can supply native pointers or function bindings. This helper
// changes a prepared private variant and the selector-specific native context
// tree. The backend must validate that context too. No server acknowledgement.
struct ChoiceView {
    const uint8_t* choices;
    size_t count;
    const char* path;
};
struct OptionFunctions {
    void (*Convert)(const void* choice,void* optional_value,uint8_t* skip);
    uint8_t (*Set)(uint32_t selector,void* variant,const char* path,const void* optional_value);
    void (*Destroy)(void* optional_value);
};
enum class OptionResult { Set,Default,Invalid,WrongThread,Unsupported,Failed };

// B002 evidence: choice stride0x38/type+0x30; optional value0x80 with native
// string capacity7 at+0x60. The future backend must validate its loaded build,
// keep these borrowed native objects alive, and bind only corroborated targets.
// It must obtain engine_thread_id from its real engine callback, never IPC.
OptionResult ApplySelectedChoice(const ChoiceView& view,uint64_t index,void* variant,
    uint32_t selector,uint32_t engine_thread_id,const OptionFunctions& functions) noexcept;
}
