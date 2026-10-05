# OpenLink browser (player app)

A desktop server browser for players: list community servers, click **Join**, then pick the server in Halo Infinite under **Custom Games → Server**. The join logic lives in the shared `connect` package.

Built with [Wails](https://wails.io) v2 (Go back end, Svelte 5 + TypeScript front end, WebView2 on Windows). It is a separate Go module, so Wails' dependencies stay out of the server tools.

## Develop

Requires Go 1.27+, Node/npm, and the Wails CLI (`go install github.com/wailsapp/wails/v2/cmd/wails@latest`).

```
wails dev       # live reload; the UI is also served at http://localhost:34115
wails build     # writes build/bin/OpenLink.exe
```

Testing against a local directory and simulated host, without touching the game's ports:

```
set OPENLINK_DEV_PORTS=21343,27117
set OPENLINK_DIRECTORY=http://127.0.0.1:8080
wails dev
```

The type check is `npm run check` in `frontend/`.

## Behaviour

- Settings (directory address, advertise mode, game folder) are stored in `%AppData%\OpenLink\settings.json`. `OPENLINK_DIRECTORY` sets the default directory.
- Only one copy runs at a time, because two would compete for UDP 1343. Closing the window leaves the server and frees the port.
- The session bar shows: contacting → ready ("open Custom Games → Server") → playing (traffic flowing). It warns when the server stops advertising or the directory is unreachable.
- Servers built for another game version are shown but cannot be joined.

## In-game vote overlay (Windows only)

Off by default; voting normally happens in the app's vote panel. Settings → In-game vote overlay offers:

- **Passive**: when a vote opens, a panel appears over the game by itself. It never takes focus or the mouse. Vote with one hotkey per choice (default Ctrl+Alt+1–4).
- **Interactive**: a one-line hint appears; the open hotkey (default Ctrl+Alt+V) brings up the panel with focus. Vote with 1–4, the arrows and Enter, or the mouse. Voting or Esc returns focus to the game.

How it works (`overlay_windows.go`): a small always-on-top window drawn with GDI on its own thread, the same approach as Discord's current overlay. Nothing is injected into Halo, so OpenLink can start before or after the game. It needs Halo borderless or windowed, which are Halo Infinite's only modes. It shows only while `HaloInfinite.exe` is the foreground app and a vote is open (the demo build shows it over any app but OpenLink). Hotkeys are registered only while a vote is open, and Settings warns when another app already holds one. The thread and window are created the first time a vote opens with the overlay on; map thumbnails are fetched from the same Halo Waypoint URLs the app uses.

Preview without the game: `wails build -tags demo`, run it, then switch to another app; the demo vote repeats every 45 s.

## Linux

Linux builds need WebKitGTK and must be built on Linux (or in CI). The CI build has not been tested on a Linux machine yet. The command-line connector that Linux players used before was retired on 2026-10-05.
