#pragma once
#include <cstdint>

namespace hostctl {
struct ContentDescriptor {
    uint32_t type;
    uint8_t asset[16];
    uint8_t version[16];
};
struct ContentSelection { ContentDescriptor map,mode; };
struct ContentArray { ContentSelection entries[16]; };
static_assert(sizeof(ContentDescriptor)==36,"B002 descriptor layout");
static_assert(sizeof(ContentSelection)==0x48,"B002 provider entry layout");
static_assert(sizeof(ContentArray)==0x480,"B002 provider array layout");

// Borrowed, engine-owned bindings. The backend must validate loaded build,
// dedicated LAN role, lobby boundary, provider lifetime and exact native targets.
// These pointers/table/thread ID/descriptors are never accepted from IPC.
struct ProviderFunctions {
    uint8_t (*Authority)(void* provider);
    const ContentArray* (*Current)(void* provider);
    uint8_t (*Set)(void* provider,const ContentArray* contents);
    const ContentArray* (*Requested)(void* provider)=nullptr;
};
enum class ProviderResult { Selected,Invalid,WrongThread,Denied,Pending,Failed };

// Swap the first u32/u16/u16 between RFC UUID and Windows GUID byte orders;
// remaining bytes are unchanged. Input/output are sixteen accessible bytes,
// possibly aliased. Native formatter14098db34 corroborates this representation.
void ConvertUuidLayout(const uint8_t* input,uint8_t* output) noexcept;

// B002: final table143d44d80, Current+a0=1404e5cec, Set+b0=142e1d478.
// Inputs already contain native GUID bytes and native-valid descriptor types.
// Copies all entries, changes entry0, requires authoritative acceptance and full
// readback. Selected proves only provider selection, never completed CMS parsing
// or a started match. Failures after Set do not imply rollback. C++ exceptions
// are caught; arbitrary invalid pointers/SEH faults are not made safe here.
// Explicit first selection requires map2/mode6 and a validated Requested
// binding. If Current is absent, preserve valid Requested or seed zeroes;
// native Set owns initialization. These types are API-derived pending runtime proof.
ProviderResult SelectLanContent(void* provider,uintptr_t expected_table,
    uint32_t engine_thread_id,const ContentSelection& selection,
    const ProviderFunctions& functions,bool initialize_if_absent=false) noexcept;
}
