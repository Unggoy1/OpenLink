#pragma once
// Reusable inline detours for B002 functions whose first `relocate` bytes are
// RIP-free whole instructions (checked against `expected`). The function start gets a
// 5-byte jmp rel32 (one 8-byte CMPXCHG) to a near-image stub that jumps to the handler;
// the returned trampoline runs the relocated bytes and continues at function+relocate.
#include <cstdint>
#include <cstddef>
namespace clientjoin {
namespace detour {
// Returns the trampoline (call it to run the original), or nullptr with *error set.
void* Install(uintptr_t image,uintptr_t rva,const uint8_t* expected,size_t expected_length,size_t relocate,const void* handler,uint32_t* error) noexcept;
}
}
