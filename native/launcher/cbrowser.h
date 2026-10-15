#pragma once
// "OpenLink mode" for the stock Customs Browser (FN060/O163). While the mode is
// on, the official MPSD search is skipped, the browser's global result vector holds our
// directory servers as lobby records (handle "openlink:<listing id>"), and joins of those
// records go to the OpenLink relay join. Mode off pushes an empty vector.
#include <cstdint>
namespace clientjoin {
namespace cbrowser {
uint32_t Install(uintptr_t image) noexcept; // installs the hooks once; Windows error
bool SetMode(bool on) noexcept;             // false when the hooks are unavailable
void Tick() noexcept;                       // engine tick: push or clear records
bool Mode() noexcept;
void Disable() noexcept;                    // boundary failure: mode off for good, queries unblocked
// A refused join wants the player told (UI tick opens the dialog via Lua).
bool TakeNotice() noexcept;
// "<asset>/<version>" (lower-case UUIDs) to the game's GUID byte layout.
bool ParseThumbRef(const char* ref,uint8_t (&asset)[16],uint8_t (&version)[16]) noexcept;
void RequeueNotice() noexcept;              // the UI was busy: try again next tick
// The next ~15 s the leave dialog's text is the "Leave server?" confirmation (OpenLink.LeaveNotice).
void LeaveNotice() noexcept;
}
}
namespace clientjoin { namespace cbrowser { bool TestParseGuid() noexcept; } }
