#pragma once
#include <cstdint>
namespace hostctl {
// B002 1404e7340, including cold continuation142253876. Do not infer
// availability from lifecycle state: these are separate native enums.
inline bool SimulationAvailable(int32_t state) noexcept { return state>=4 && state<=7; }
}
