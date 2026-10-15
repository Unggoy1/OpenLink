#pragma once
// In-game vote panel: the vote in the look of Halo's own menus, drawn by the launcher DLL in a
// small always-on-top window over Halo Infinite (see overlay.cpp).
#ifndef WIN32_LEAN_AND_MEAN
#define WIN32_LEAN_AND_MEAN
#endif
#include <windows.h>
#include "vote.h"
namespace clientjoin {
namespace overlay {
// Off: votes are still received but nothing is shown (openlink-launcher.json "vote_overlay").
void SetEnabled(bool on) noexcept;
// Where the overlay writes diagnostics (the launcher log); none by default.
void SetLog(void (*sink)(const char*)) noexcept;
// The server lobby screen is showing: the panel takes the place of its greyed-out host rows.
// Otherwise it floats at the top left. Set from the UI script, with the rows' rectangle and the
// start button's bottom in UI units when known (all 0 otherwise).
void SetLobbyBlock(bool on,float x0,float y0,float x1,float y1,float button_bottom) noexcept;
bool LobbyShowing() noexcept;
// A ballot or vote changed: re-evaluate soon. Starts the overlay thread on first use.
void Wake() noexcept;
// Development preview into a PNG: with a screenshot as background, the panel is drawn into
// `block` as on the lobby screen; without one, the floating panel is drawn alone, `block`
// giving its width and the lobby block's height (its scale).
bool RenderPreview(const vote::View& v,RECT block,bool controller,bool fetch_thumbs,const wchar_t* background,const wchar_t* path) noexcept;
}
}
