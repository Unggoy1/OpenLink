#pragma once
// In-game OpenLink server browser support. Keeps the directory list fresh on
// a background thread, registers each row's display text with the text hook, and
// provides the OpenLink.* Lua natives (Log, Count, Row, Join, Refresh, Status, SetBrowserMode, BrowserMode).
// Natives never take the backend mailbox lock; Join is handed to the engine tick.
#include <cstdint>
#include <cstddef>
namespace clientjoin {
namespace browser {
constexpr size_t kMaxRows=16;
struct Config { char directory[160]; char lan_ip[16]; };
uint32_t Configure(const Config&) noexcept;      // starts the fetch thread once; Windows error
// Registers the OpenLink table in the given lua_State (caller holds the UI manager lock).
bool RegisterNatives(uintptr_t image,uintptr_t L) noexcept;
bool Registered() noexcept;
bool RegisteredIn(uintptr_t L) noexcept;
// Engine tick: returns true and the listing id when Lua asked to join one.
bool TakeJoin(char (&listing)[40]) noexcept;
void JoinResult(uint32_t code) noexcept;           // reported back to Lua via OpenLink.Status
void SetActive(bool active) noexcept;              // false once OpenLink stopped (anti-cheat boundary)
const Config& Current() noexcept;
uint32_t Fetches() noexcept;
// The current match's map ("<asset>/<version>") of the listing last joined, if listed.
bool JoinedThumb(char (&thumb)[96]) noexcept;
// Called when the player leaves the server from the lobby (OpenLink.LeaveServer).
void SetLeaveHook(void (*hook)()) noexcept;
uint32_t FetchError() noexcept;                    // last directory fetch result (0 = ok)
bool Configured() noexcept;                        // Configure started the fetch thread
bool Fetched() noexcept;                           // a fetch has succeeded at least once
uint32_t RowCount() noexcept;
uint32_t Joins() noexcept;
void Log(const char* text) noexcept;
// Queue a join by listing id (from Lua or a browser join hook). nullptr when queued,
// otherwise a short reason.
const char* RequestJoin(const char* listing) noexcept;
// RequestJoin refusal for a listing whose OpenLink Server needs a newer launcher (app protocol).
constexpr const char* kNeedsUpdate="server needs a newer OpenLink Launcher";
// Copy of the current snapshot for the Customs Browser records; rev changes with it.
struct BrowserRow { char id[40]; char name[96]; char thumb[96]; char mode[96]; char match[96]; char phase[16]; char description[484]; int32_t players; bool joinable; };
size_t CopyRows(BrowserRow* out,size_t capacity,uint32_t& rev) noexcept;
}
}
