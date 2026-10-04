# OpenLink

**Community dedicated servers for Halo Infinite.** Run your own Halo Infinite server, list it in a community directory, and let players anywhere join it, using the game's own LAN server mode. Nothing in the game is modified.

> Working name. The programs (`hi-hostagent`, `hi-directory`, `hi-connector`) keep their current names until the project name is final.

> Unofficial fan project. Not affiliated with or endorsed by Microsoft, Xbox or Halo Studios. Halo is a trademark of Microsoft.

## How it works

Halo Infinite ships with a LAN server mode. A LAN server announces itself with a small broadcast "beacon" on the local network, and the game then connects to it on UDP port 1343. This project carries both of those over the internet:

```
 server host                         directory                        player PC
┌───────────────────────┐   beacons  ┌──────────┐   beacons   ┌──────────────────────────┐
│ Halo Infinite         │──────────▶ │ hi-      │ ──────────▶ │ hi-connector             │
│ LAN server  (UDP 1343)│  (agent)   │ directory│             │  replays beacon locally  │
│ hi-hostagent          │            └──────────┘             │  forwards UDP 1343 ──┐   │
└──────────▲────────────┘                                     │ Halo Infinite ◀──────┘   │
           └───────────── game traffic, UDP 1343 (port-forwarded) ──────────────────────┘
```

- **`hi-hostagent`** runs on the server machine. It starts the LAN server, restarts it if it exits, picks up the server's beacon and keeps the directory listing fresh.
- **`hi-directory`** is the public server list. Hosts register and send heartbeats; players list servers and fetch beacons.
- **The player side runs on each player's PC.** For the chosen server it replays that server's own beacon to the local game and forwards the game's traffic to the server. To the game, it looks like an ordinary LAN game. It comes in two forms:
  - the **browser app** (`browser/`): a desktop server list with a Join button;
  - **`hi-connector`**: the same thing on the command line, also for Linux.

The connector forwards game packets unchanged. They are encrypted by the game, and this project never reads, decrypts or alters them.

**What this project does not do:** it does not modify game files, touch game memory or interact with anti-cheat. It does not handle Xbox/Microsoft credentials: players sign in inside the game as usual, and the directory stores only what hosts send it (name, address, build, status, beacon).

## Requirements

- Halo Infinite on Steam, **the same game build on the server and on every player's PC**. The game refuses a version mismatch, and the tools check the build for you.
- Server host: Windows, a public IPv4 address, and **UDP 1343 forwarded** to the server machine.
- Players: Windows, or Linux with the game running under Proton. No port forwarding needed.

## Hosting a server

Full guide: **[docs/HOSTING.md](docs/HOSTING.md)**. In short:

1. Forward **UDP 1343** to the server PC and allow `hi-hostagent.exe` in the Windows firewall.
2. Check reachability with `hi-hostagent -simulate -directory https://DIRECTORY -name "My Server"`. The agent logs whether the directory could reach your port.
3. Save your settings once with `hi-hostagent -directory https://DIRECTORY -name "My Server" -region us-west init-config`. After that, `hi-hostagent` with no flags runs the server. `hi-hostagent autostart enable` starts it at logon.

By default the agent runs in **proxy mode**: the game server listens only on 127.0.0.1 and the agent fronts the public port. That gives player counts, ping and reachability checks, and `status` / `kick` / `ban` commands, with per-player rate limits.

## Playing

**With the browser app (Windows):** open it, enter the directory address once in Settings, click **Join** on a server, then in Halo Infinite go to **Custom Games → Server** and pick the host's PC name. The bar at the bottom shows when you are connected. Keep the app open while you play. See [browser/README.md](browser/README.md).

**With the command line (Windows or Linux):**

```
hi-connector -directory https://DIRECTORY list
hi-connector -directory https://DIRECTORY join "My Server"
```

Keep the connector running, then start Halo Infinite and go to **Custom Games → Server**. The host appears under the server PC's name. Join it. Closing the connector disconnects you.

- The connector prints one status line every 5 s: beacon age (should stay at a few seconds) and the packets going to and coming from the server.
- Set `HICOMM_DIRECTORY` once instead of passing `-directory` every time.
- Do not run a LAN server on the same PC: the connector needs local UDP 1343.
- If the server is not listed, try `-advertise broadcast` (the default is `loopback`, which is the mode tested on both Windows and Linux).
- **Linux/Proton:** use the native Linux build of `hi-connector` (not under Wine). If your Steam library is not in a standard location, pass `-install "/path/to/steamapps/common/Halo Infinite"` so the build check works.

## Running a directory

The directory is a single small HTTP service. It carries no game traffic: players reach servers directly over UDP. It keeps everything in memory. A restart or redeploy clears the list, and host agents register again within seconds. **Run exactly one instance.**

**Container (e.g. Railway).** The repo's `Dockerfile` builds a minimal image. The service listens on `$PORT` when that is set. Configure it with environment variables:

| Variable | Value |
|---|---|
| `HICOMM_CLIENT_IP_HEADER` | the header your platform's proxy uses for the client address, e.g. `X-Forwarded-For` (the rightmost entry is used, because entries to its left can be forged by clients) |
| `HICOMM_CLIENT_IP_HOPS` | with `X-Forwarded-For`: how many trusted proxies append to it (default 1). On Railway it is likely `2` (its edge has an outer layer whose address is appended last). Confirm with `/v1/whoami?debug=1` |
| `HICOMM_REGISTER_KEY` | optional. When set, hosts need it to register, and only key holders may list an address other than their own IP |

After deploying, open `https://YOUR-DIRECTORY/v1/whoami` from home. It must show **your public IP**. `/v1/whoami?debug=1` also shows the forwarding headers that arrived, to work out the right header and hop count. If it shows a private or proxy address, the client-IP header is wrong, and hosts would be listed under the wrong address.

**Own server.** Run it behind an HTTPS reverse proxy (Caddy, nginx and so on):
```
hi-directory -listen 127.0.0.1:8080 -client-ip-header X-Forwarded-For
```

Listings expire 45 s after the last heartbeat. The directory also probes proxy-mode hosts about once a minute and lists them as reachable or unreachable. It only probes listed endpoints, never addresses a caller supplies. Without the register key, a host can only list its own public IP; this stops the directory being used to point players' traffic at third parties.

| Endpoint | |
|---|---|
| `POST /v1/servers` | register (returns an ID and an update token) |
| `PUT /v1/servers/{id}` | heartbeat with status and latest beacon (token required) |
| `DELETE /v1/servers/{id}` | unregister (token required) |
| `GET /v1/servers[?build=…]` | list |
| `GET /v1/servers/{id}/beacon` | latest beacon |
| `GET /v1/whoami` | the address the directory sees for the caller |
| `GET /healthz` | health check |

## Building

Requires Go 1.27+. On Windows:

```
.\build.ps1
```

This runs the tests and writes `bin\*.exe`, plus `bin\linux-amd64\hi-connector` and `hi-directory`. The code uses only the Go standard library.

## Status and known limitations

- **Tested:** remote players on Windows and on Linux/Proton listed a hosted server through the connector and played several full matches in a row. In that test the host's UDP 1343 was exposed through a tunnel rather than a router port forward. A direct port-forwarded host should behave the same, but it has not been tested yet.
- **Lobby control:** the first player to join owns the lobby and picks the map and mode. There is no server-side map rotation yet.
- **Updates:** every game update requires hosts and players to update together. The directory filters servers by build.
- Player counts, ping and reachability need proxy mode on the host (the default). The browser app is Windows-only for now; Linux players use `hi-connector`.
- Proxy mode, bans and several servers on one PC are new and have not been tested with the real game yet.
- Untested: many simultaneous players, long-running uptime, and host CPU and memory use.

## Development

- `go test ./...` runs the unit tests: directory (auth, validation, expiry, limits), UDP forwarder, beacon relay, BootstrapLog parsing, UDP port-owner lookup and the simulator.
- `hi-hostagent -simulate -loopback` keeps simulated beacons on 127.0.0.1.
- `HICOMM_DEV_PORTS=21343,27117` moves only the simulator and the connector to other ports, so they can be tested on a machine already running a server. The game itself always uses 1343 and 7117.
