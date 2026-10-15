#pragma once
// Our own UI text for string IDs we own. The game's string-list lookups
// 1405c7204(tag,sid,out,cap,found) and 1405c7038(tag,sid,out[0x400],found) are
// detoured; IDs in our table return our UTF-16 text, all others pass through (O158).
#include <cstdint>
#include <cstddef>
namespace clientjoin {
namespace text {
// Game string-ID hash: lower-case A-Z, '-' and ' ' -> '_', '\n' -> '#', then
// murmur3_32 (x86, seed 0) over the normalized bytes (0x140748d64).
uint32_t StringId(const char* text,size_t length) noexcept;
// Adds or replaces key -> UTF-8 text; returns the ID. 0 on invalid input or a full table.
uint32_t Set(const char* key,const char* utf8) noexcept;
// Installs both detours once (image = loaded module base). Windows error code.
uint32_t Install(uintptr_t image) noexcept;
bool Installed() noexcept;
uint32_t Hits() noexcept;
// Replace existing game string IDs, only while the overlay is active.
bool SetOverlay(uint32_t sid,const char* utf8) noexcept;
void SetOverlayActive(bool on) noexcept;
// Computed overlay text, asked first while the overlay is active (true = handled).
void SetOverlayProvider(bool (*provider)(uint32_t sid,wchar_t* out,size_t capacity)) noexcept;
// Computed text asked first at all times, overlay or not (dialog text the script opens anywhere;
// keep it cheap: it sees every lookup).
void SetAlwaysProvider(bool (*provider)(uint32_t sid,wchar_t* out,size_t capacity)) noexcept;
}
}
