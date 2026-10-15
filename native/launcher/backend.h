#pragma once
#include "join_machine.h"
#include <windows.h>
namespace clientjoin {
enum Gate : uint32_t { Build=1,Client=2,NoEac=4,Hook=8,Ticking=16,Lan=32,WasArmed=64,GatePassed=128,ClientSession=256,PreGame=512,Fallback=1024,LobbyInitRequested=2048,
    BrowseMatch=4096,SelectionDispatched=8192,TargetSelected=16384,TargetPreset=32768 };
// Engine-tick view of the game and the join in progress (used inside the DLL).
struct Report {
    Code code=NotReady; uint32_t gates=0; Phase phase=Idle; DWORD error=0;
    int32_t mode=-1,kind=-1,init_mode=-1; uint32_t bypass=0,armed=0,passed=0;
    uint64_t front_ticks=0,pre_ticks=0;
    int32_t pre_state=-1,pre_target=-1;
    uint32_t text_entries=0,text_error=0;
};
// Installs the engine-tick hooks, the in-game browser and the Customs Browser hooks for the
// whole game process (called once from HiOpenLinkStart).
DWORD StartPersistent(const char* directory,const char* lan_ip) noexcept;
// Starts the in-process relay for a directory listing; once its beacons are advertised,
// the stock LAN selection of the relay's local address is queued.
struct RelayJoin { char directory[160]; char listing[40]; char lan_ip[16]; };
Code QueueOpenLink(const RelayJoin&) noexcept;
Code Queue(const Request&) noexcept;
// Browser directory/LAN address for the OpenLink.* natives and joins.
Code ConfigureBrowser(const char* directory,const char* lan_ip) noexcept;
// Registers "key<TAB>text" lines (UTF-8) as our UI strings and installs the string-list
// lookup detours once. Returns Ok, Invalid or Failure (report.text_error).
Code SetTexts(const char* lines,size_t length) noexcept;
}
