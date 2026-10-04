# OpenLink browser (player app)

A desktop server browser for players: list community servers, click **Join**, then pick the server in Halo Infinite under **Custom Games → Server**. It runs the same join logic as `hi-connector` (the shared `connect` package).

Built with [Wails](https://wails.io) v2 (Go back end, Svelte 5 + TypeScript front end, WebView2 on Windows). It is a separate Go module, so Wails' dependencies stay out of the server tools.

## Develop

Requires Go 1.27+, Node/npm, and the Wails CLI (`go install github.com/wailsapp/wails/v2/cmd/wails@latest`).

```
wails dev       # live reload; the UI is also served at http://localhost:34115
wails build     # writes build/bin/OpenLink.exe
```

Testing against a local directory and simulated host, without touching the game's ports:

```
set HICOMM_DEV_PORTS=21343,27117
set HICOMM_DIRECTORY=http://127.0.0.1:8080
wails dev
```

The type check is `npm run check` in `frontend/`.

## Behaviour

- Settings (directory address, advertise mode, game folder) are stored in `%AppData%\OpenLink\settings.json`. `HICOMM_DIRECTORY` sets the default directory.
- Only one copy runs at a time, because two would compete for UDP 1343. Closing the window leaves the server and frees the port.
- The session bar shows: contacting → ready ("open Custom Games → Server") → playing (traffic flowing). It warns when the server stops advertising or the directory is unreachable.
- Servers built for another game version are shown but cannot be joined.

## Linux

Linux builds need WebKitGTK and must be built on Linux (or in CI). Until then, Linux players can use the command-line `hi-connector`.
