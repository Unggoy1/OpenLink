#pragma once
// The OpenLink server lobby's map header (picture, name, version) for the map that will really
// be played. Halo's lobby keeps showing the player's previous map when the server's map is not
// in their content list (FN069); this feeds the real map into the header update so the game's
// own code fills in the name, version and picture.
#include <cstdint>
namespace clientjoin {
namespace mapheader {
// Replaces the map read of the lobby header update (0x1414f1b90) and starts the thread that picks
// the map to show (Tick, four times a second). Windows error code.
uint32_t Install(uintptr_t image,void (*log)(const char*)) noexcept;
// One pick; called only by that thread.
void Tick() noexcept;
}
}
