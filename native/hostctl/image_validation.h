#pragma once
#include <windows.h>
#include <cstdint>

namespace hostctl {
// Header identity for a loaded module. File/build identity is checked separately
// using the exact SHA256 before any B002 addresses or hook are used.
inline bool ValidateLoadedHeader(uintptr_t base,const IMAGE_DOS_HEADER& dos,
                                const IMAGE_NT_HEADERS64& nt,uintptr_t required_end) noexcept {
    return base && dos.e_magic==IMAGE_DOS_SIGNATURE && dos.e_lfanew>=0 && dos.e_lfanew<=0x100000 &&
        nt.Signature==IMAGE_NT_SIGNATURE && nt.FileHeader.Machine==IMAGE_FILE_MACHINE_AMD64 &&
        nt.OptionalHeader.Magic==IMAGE_NT_OPTIONAL_HDR64_MAGIC && nt.OptionalHeader.ImageBase==base &&
        nt.OptionalHeader.SizeOfImage>required_end;
}
}
