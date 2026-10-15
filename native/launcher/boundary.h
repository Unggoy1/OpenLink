#pragma once
#include <windows.h>
namespace clientjoin {
DWORD CheckNoEac(DWORD pid) noexcept;
bool ValidateFile(const wchar_t* path) noexcept;
DWORD CheckTarget(HANDLE process,const wchar_t* expected) noexcept;
}
