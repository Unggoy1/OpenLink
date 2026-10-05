#pragma once
#include <array>
#include <cstddef>
#include <cstdint>
#include <string>
#include <vector>

namespace hostctl {
// Internal engine-backend reader. No default live/process reader and no IPC
// addresses. The backend must retain a consistent, validated native source.
struct NativeReader {
    void* context;
    bool (*Copy)(void* context,uintptr_t address,void* destination,size_t size);
};
struct SavedChoice {
    std::string path;
    uint16_t selected_index=0;
    std::array<uint8_t,0x38> choice{};
};
enum class SnapshotResult { Ready,Invalid,WrongThread,Unsupported,Failed };

// B002 layout only, called from the recorded engine thread after build/role/
// source ownership checks. Output is DLL-owned and cleared on every failure.
// It contains selected scalar choice records in schema traversal order. No
// game calls, UI changes or server acknowledgement occur here. Non-ASCII and
// paths exceeding the bounded native setter's supported limits are rejected.
SnapshotResult SnapshotSavedChoices(uintptr_t schema_root,uintptr_t saved_tree,
    uint32_t engine_thread_id,const NativeReader& reader,
    std::vector<SavedChoice>& output) noexcept;
}
