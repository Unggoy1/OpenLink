#pragma once
// Vote hotkeys, written like the retired OpenLink app's (overlay_keys.go): "Ctrl+Alt+1" with the
// modifiers Ctrl, Alt, Shift, Win and one key: A-Z, 0-9, Num0-Num9, F1-F24, `, Insert, Delete, Home,
// End, PageUp, PageDown, Pause. A key alone must be an F-key or Pause (anything else would be taken
// from every app and the game's own controls while a vote is open).
#include <cstdint>
#include <cstddef>
namespace clientjoin {
namespace hotkey {
struct Key { uint32_t mods; uint32_t vk; char name[32]; }; // RegisterHotKey MOD_* flags, virtual key, "Ctrl+Alt+1"
constexpr size_t kVoteKeys=4;
// One hotkey; on failure why says what is wrong.
bool Parse(const char* text,Key& out,char* why,size_t why_capacity) noexcept;
// The vote keys: four distinct hotkeys separated by commas.
bool ParseVoteKeys(const char* text,Key (&out)[kVoteKeys],char* why,size_t why_capacity) noexcept;
// The defaults: Alt+1 .. Alt+4.
void DefaultVoteKeys(Key (&out)[kVoteKeys]) noexcept;
}
}
