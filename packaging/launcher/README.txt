OpenLink Launcher (Windows)

Adds an OpenLink Servers browser inside Halo Infinite (Play > Custom Game) to find and join
community servers, vote for the next match and leave a server again.

WARNING: the launcher starts Halo Infinite without anti-cheat and loads openlink-launcher.dll
into your game, in memory. It never changes game files or anti-cheat files, services or
settings, but running the game this way is at your own risk with respect to the game's terms.
Starting Halo Infinite from Steam as usual afterwards is unaffected.

Files
  openlink-launcher.exe            finds and starts Halo Infinite, then adds OpenLink to it
  openlink-launcher.dll            OpenLink inside the game (browser, joins, voting)
  openlink-launcher.example.json   optional settings

Requirements
  - Windows 64-bit, Halo Infinite on Steam (the final PC build), Steam running and signed in.
  - Halo Infinite started from Steam (with anti-cheat) must not be running.

Start
  1. Keep the files together in a folder of your choice (not the game folder).
  2. Double-click openlink-launcher.exe. Its window closes by itself once OpenLink is
     running, or stays open and says what went wrong.
  3. Optional: run "openlink-launcher.exe -create-shortcut" once for a Desktop shortcut.

Playing
  - Play > Custom Game > OpenLink Servers lists the servers; select one and press Join.
  - Vote in the lobby: click a choice, press Alt+1 to Alt+4, or hold View and press the
    D-pad on a controller.
  - Back in a server's lobby asks "Leave Server?"; Okay leaves the server.

Settings
  All optional. To change one, copy openlink-launcher.example.json to openlink-launcher.json
  (next to the exe) and edit it: "directory" (the server list; https:// only),
  "vote_overlay" ("off" hides the vote panel), "vote_keys" (four keys, e.g.
  "Alt+1, Alt+2, Alt+3, Alt+4"), "lan_ip" (leave empty).

Updates
  At start the launcher checks for a newer release and offers to open its download page.

openlink-launcher.log next to the DLL records what OpenLink did (no credentials); attach it
when asking for help. Full documentation: native/launcher/README.md in the source repository.
