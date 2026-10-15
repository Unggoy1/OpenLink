# OpenLink Launcher (optional, experimental)

The OpenLink Launcher is an alternative to the OpenLink app. Instead of a separate server
list, it adds an **OpenLink Servers** item to Halo Infinite's **Play → Custom Game** menu: the
game's own Customs Browser, filled with the servers from the OpenLink Directory, with map art,
the current mode, player counts and each server's description. Pick a server and press Join.

> [!WARNING]
> The launcher starts Halo Infinite **without anti-cheat** and loads `openlink-launcher.dll`
> into your game, in memory. It never changes game files or anti-cheat files, services or
> settings, but running the game this way is at your own risk with respect to the game's terms.
> Use the OpenLink app if you want your game left untouched.

## Requirements

- Windows, 64-bit.
- Halo Infinite on Steam, the final PC build (6.10026.19225.0). The launcher checks the exact
  game executable and refuses anything else.
- Steam running and signed in. Sign in to Halo Infinite as usual; the launcher never sees your
  credentials.
- The normal Halo Infinite (started from Steam, with anti-cheat) must not be running.

## Install and start

1. Unzip `openlink-launcher.exe` and `openlink-launcher.dll` to a folder of your choice
   (not into the game folder). Keep the two files together.
2. Double-click `openlink-launcher.exe`. It finds Halo Infinite in your Steam libraries,
   starts it, waits for its window and adds OpenLink. Its window closes by itself when
   OpenLink is running, or stays open and explains what went wrong.
3. Optional: run `openlink-launcher.exe -create-shortcut` once for an **OpenLink** shortcut on
   your Desktop.

If Halo Infinite is already running without anti-cheat (for example from your own shortcut to
`game\HaloInfinite.exe`), the launcher adds OpenLink to it instead of starting another copy.

## Playing

- **Play → Custom Game → OpenLink Servers** lists the servers. The stock **Customs Browser**
  item keeps working as before.
- Select a server for its details (description, map, mode, players), then **Join Game**, or use
  **Join** straight from the grid.
- A server that cannot take players right now, or that needs a newer launcher, shows a short
  message instead of joining.
- When you leave a match you return to OpenLink Servers. **Back** goes to the Custom Game menu.
  When a match ends normally you stay in the server's lobby for the next one.
- **Voting:** on a server with playlist voting, the lobby shows a **Vote for the next match**
  panel in place of the greyed-out host options. Vote with **Ctrl+Alt+1** to **4**, or on a
  controller hold **View** and press the D-pad (up, right, down, left). You can change your
  vote until the timer ends; the panel then shows the next match for a few seconds. The
  lobby's map picture follows your vote, then the winner.
- The game shows **OFFLINE** in the corner when it runs without anti-cheat; that is expected.

## Settings

All settings are optional. Put them in `openlink-launcher.json` next to the DLL:

```json
{ "directory": "https://openlink-dir.unggoy.xyz", "lan_ip": "", "vote_overlay": "on" }
```

- `directory`: the OpenLink Directory to list servers from. The default is the public one.
- `lan_ip`: leave empty. Set it to this PC's LAN address only when an OpenLink Server runs on
  the same PC as your game (the server then owns the local game port, so the launcher announces
  servers on the LAN address instead).
- `vote_overlay`: `"off"` hides the vote panel (and its Ctrl+Alt keys). The lobby's map
  picture still follows the vote.

The launcher writes `openlink-launcher.log` next to the DLL: the start line (version, directory,
LAN address if set), the listing ids you join, join results, browser messages, where the vote
panel was placed and which map the lobby header shows. It holds no
credentials and is never trimmed; delete it whenever you like.

### Command-line options

| Option | Meaning |
|---|---|
| (none) | Find the game, start it (or use the running one) and add OpenLink |
| `-create-shortcut` | Put an OpenLink shortcut on your Desktop |
| `-where` | Show which Halo Infinite was found |
| `-game <path>` | Use this `game\HaloInfinite.exe` instead of the one in Steam |
| `-attach` | Wait for a Halo Infinite started another way |
| `-version` | Show the version |

## How it works

`openlink-launcher.exe` starts `game\HaloInfinite.exe` from the install folder, the same way a
direct shortcut does; started like this, the game runs without anti-cheat. The launcher checks
that anti-cheat is not running and that the executable is the exact supported build, then
loads `openlink-launcher.dll` into that player client and calls its `HiOpenLinkStart` export.
It refuses server processes and anything running with anti-cheat.

Inside the game, the DLL:

- checks the loaded game image against fixed function fingerprints before changing anything,
  and re-checks every 30 s that anti-cheat is still absent (if it appears, OpenLink stops
  listing servers and joining for the rest of that game session);
- runs a small script (`lua/browser.lua`) in the game's menu UI that adds the menu item and
  opens the Customs Browser in "OpenLink mode";
- while that mode is on, puts the directory's servers into the browser's results (the
  official search is skipped), shows server names, modes, descriptions and messages, and hides
  controls that do not apply (filters, XP notes);
- on Join, starts an in-process relay to the server's OpenLink Server proxy (like the app) and
  uses the game's own LAN join for the relay's local address. The lobby stays a LAN lobby, so
  official matchmaking stays blocked;
- after you leave an OpenLink match, opens the browser in place of the Create Match screen the
  game would otherwise show;
- receives the server's vote ballots on the relay connection (like the app) and draws the vote
  panel in a small always-on-top window over the game, placed from the lobby menu's own layout;
  your vote goes back to the server on the same connection;
- feeds the voted map into the lobby header's map lookup, so the game itself shows that map's
  picture, name and version (the game would otherwise keep showing its default map).

## Limitations

- One game build only (the final Steam build); Windows only.
- Voting uses the keyboard or a controller; the mouse cannot click the vote panel yet. The
  panel's placement has been checked on a 3440x1440 display only.
- Before anyone votes, the lobby header shows the game's default map.
- Map art, names and descriptions come from the game's content service, as in the stock
  browser.
- The browser shows up to 16 servers.
- One join at a time; a join that cannot start shows a message rather than retrying.

## Building and testing

Needs the Visual Studio C++ x64 tools (`native\hostctl\vcvars.cmd` finds them) and Windows
PowerShell. From the repository root:

```
native\launcher\build.cmd <output-folder>
native\launcher\test.cmd <output-folder>
```

`build.cmd` writes `openlink-launcher.exe` and `openlink-launcher.dll` (x64, static C runtime,
`/W4 /WX`). The version comes from `git describe`, like the Go programs. `lua\browser.lua` is
embedded at build time by `embed-lua.ps1`. `test.cmd` builds, then runs the unit tests (join
state machine, relay, server-list and ballot parsing, string IDs, detours) and loads the DLL
into a test process to check that it refuses to start outside the game. No game is needed for
the tests. It also builds `tests\overlay-preview.exe`, which draws the vote panel for sample
ballots into PNG files (`overlay-preview <folder> [-thumbs] [-lobby <screenshot> l t r b]`).

The launcher speaks the same protocol level to OpenLink Server as the app:
`kAppProtocol` in `relay.h` must equal `api.AppProtocol` (a Go test in `internal/api` checks).

License: AGPL-3.0, like the rest of the repository.
