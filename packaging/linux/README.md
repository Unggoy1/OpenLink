# OpenLink Server on Linux (experimental, not run yet)

A Docker image that runs OpenLink Server and the Halo Infinite LAN server under
[Wine](https://www.winehq.org/), with no Steam client: the image installs the
Steam-free `steam_api64.dll` (native/steamfree) into the game files. Nothing
here has run on Linux yet. Work through the test plan below and keep what each
step asks for.

## How it works

- One container runs everything in one Wine prefix: `openlink-server.exe`,
  `openlink-loader.exe`, `openlink-control.dll` and the game server, exactly
  as on Windows.
- The game files are **not** in the image. Mount your own Halo Infinite folder
  at `/halo`. It must be writable: the game server writes its cache and logs
  there, and on start the container swaps in the Steam-free
  `game/steam_api64.dll` (the original is kept as `steam_api64.stock.dll`).
  Use a copy dedicated to the server.
- `./data` holds `openlink-server.json`, `playlist.json`, `bans.json`, the log
  and the Wine prefix.
- Players reach the container on one UDP port (`PUBLIC_PORT`, container port
  1343). Nothing else needs to be open.
- Two Linux differences are handled in `entrypoint.sh`:
  - The proxy listens on the container's own address instead of `0.0.0.0`,
    since Linux does not let a wildcard socket share 1343 with the game
    server's `127.0.0.x:1343`.
  - `reuseaddr.c` (preloaded into Wine) lets OpenLink Server share UDP 7117
    with the game server to read its beacons. On Windows that works on its
    own; on Linux both sockets must opt in.

## Requirements

- Linux x86-64 with Docker and Docker Compose v2. No GPU: the game server
  renders nothing.
- A Halo Infinite install of the same build as the players (copy one from a
  Windows PC, or download it with steamcmd and your own Steam account:
  `steamcmd +@sSteamCmdForcePlatformType windows +force_install_dir /srv/halo +login <you> +app_update 1240440 validate +quit`).
- The OpenLink Server package with `steam-free/steam_api64.dll`: put
  `OpenLink-Server-windows-amd64.zip` in this folder, or build with
  `OPENLINK_VERSION=<release tag>` to download it from GitHub.

## Test plan

Run each step in this folder. Stop at the first step that fails, and send what
that step lists.

**1. Build.** `docker compose build` (the variable below is only needed later).

```
export HALO_INSTALL=/srv/halo PUBLIC_PORT=1343
docker compose build
```

Send if it fails: the build output.

**2. Config.** `docker compose run --rm openlink` creates
`data/openlink-server.json` and `data/playlist.json` and exits. Edit them:
`name`, `public_port` (= `PUBLIC_PORT`), `directory` (your test directory) and
a playlist. `public_host` can stay empty if the directory sees this server's
public IP. Make `./data` and the Halo folder writable by UID 1000
(`sudo chown -R 1000:1000 data /srv/halo`).

**3. Wine without the game.**

```
docker compose run --rm --service-ports openlink simulate
```

Expect `simulation running` in the output and the server in the directory's
list with reachability ok (fake beacons, nobody can join). Ctrl+C ends it.
Send if it fails: the output and `data/openlink-server.log`.

**4. The game server.**

```
docker compose up -d
docker compose logs -f
```

Within a few minutes expect, in order: `server started`, `server owns UDP 1343`,
`host control transport connected`, `listening for server beacons`, then the
directory listing. `docker compose exec openlink openlink status` should show
`"status": "ready"`, `host_control.connected: true` and `beacons_captured`
rising. Send in any case:
- `data/openlink-server.log` and the `status` output
- `/srv/halo/BootstrapLog.txt`
- `/srv/halo/game/steamfree-*.jsonl` (the Steam-free DLL's call log; an
  `unsupported_` record names a missing Steam call, exit code `0xe0534645`)
- `docker stats --no-stream` (CPU and memory)

If the game server exits at once, run it again with
`WINEDEBUG=+seh,+loaddll docker compose up` and send the first 300 lines after
`server started`.

**5. Beacons, if step 4 shows `cannot listen for beacons`, or
`beacons_captured` stays 0.** Note whether the error stays, and send
`docker compose exec openlink cat /proc/net/udp` (port 7117 is `1BCD`, 1343 is
`053F`: it shows which addresses the game server and OpenLink Server bound). For comparison, run once with the 7117 sharing off
(`OPENLINK_NO_REUSEADDR=1` in the service's `environment`): it should then fail
the same way, which shows the preload is what makes it work.

**6. Join and play.** From a player PC, join the server with OpenLink Launcher,
vote and play one whole match, then let the next vote open. Send the log,
`status` and the `steamfree-*.jsonl` file, plus what the players saw.

**7. Stop.** `docker compose down` should end the game server within 30 s
(`docker compose logs` shows OpenLink Server stopping it).

## Options

- `WINE_BRANCH` build arg: `stable` (default), `staging` or `devel` WineHQ
  packages, for comparison if the game server fails under stable.
- `OPENLINK_LISTEN`: the proxy's listen address inside the container (default:
  the container's address, port 1343).
- One container per server. For a second server, copy this folder and use
  another `PUBLIC_PORT`; each one needs its own writable Halo folder.
- Admin commands: `docker compose exec openlink openlink <status|kick|ban|end-match ...>`.
