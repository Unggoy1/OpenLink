# OpenLink

**Community dedicated servers for Halo Infinite.** Run your own Halo Infinite server, list it in a community directory, and let players anywhere join it, using the game's own LAN server mode. Players' games are never modified.

Three parts:

- **OpenLink** (the app, `browser/`): players browse servers and join.
- **OpenLink Server** (`openlink-server.exe`, `openlink-control.dll`, `openlink-loader.exe`): hosts run a server from a playlist.
- **OpenLink Directory** (`openlink-directory`): the public server list.

> Unofficial fan project. Not affiliated with or endorsed by Microsoft, Xbox or Halo Studios. Halo is a trademark of Microsoft.

## How it works

Halo Infinite ships with a LAN server mode. A LAN server announces itself with a small broadcast "beacon" on the local network, and the game then connects to it on UDP port 1343. This project carries both of those over the internet:

```
 server host                         directory                        player PC
┌───────────────────────┐   beacons  ┌──────────┐   beacons   ┌──────────────────────────┐
│ Halo Infinite         │──────────▶ │ OpenLink │ ──────────▶ │ OpenLink app             │
│ LAN server  (UDP 1343)│            │ Directory│             │  replays beacon locally  │
│ OpenLink Server       │            └──────────┘             │  forwards UDP 1343 ──┐   │
└──────────▲────────────┘                                     │ Halo Infinite ◀──────┘   │
           └───────────── game traffic, UDP 1343 (port-forwarded) ──────────────────────┘
```

- **OpenLink Server** (`openlink-server.exe`) runs on the server machine. It starts the LAN server with the control DLL, picks each match from the playlist (or the players' vote), restarts the server if it exits, picks up the server's beacon and keeps the directory listing fresh.
- **OpenLink Directory** (`openlink-directory`) is the public server list. Hosts register and send heartbeats; players list servers and fetch beacons.
- **The OpenLink app** (`browser/`) runs on each player's PC: a desktop server list with a Join button. For the chosen server it replays that server's own beacon to the local game and forwards the game's traffic to the server. To the game, it looks like an ordinary LAN game.

The app forwards game packets unchanged. They are encrypted by the game, and this project never reads, decrypts or alters them.

**What this project does not do:** it never modifies players' games: the app only relays network traffic. On the host, OpenLink Server loads its control DLL into the host's own game server process (to pick maps and modes and run the lobby); it does not modify game files. Hosting this way is at the host's own risk with respect to the game's terms. It does not handle Xbox/Microsoft credentials: players sign in inside the game as usual, and the directory stores only what hosts send it (name, address, build, status, beacon).

## Requirements

- Halo Infinite on Steam, **the same game build on the server and on every player's PC**. The game refuses a version mismatch, and the tools check the build for you.
- Server host: Windows, a public IPv4 address, and **UDP 1343 forwarded** to the server machine.
- Players: Windows, or Linux with the game running under Proton. No port forwarding needed.

## Hosting a server

Full guide: **[docs/HOSTING.md](docs/HOSTING.md)**. In short:

1. Forward **UDP 1343** to the server PC and allow `openlink-server.exe` in the Windows firewall.
2. Check reachability with `openlink-server -simulate -directory https://DIRECTORY -name "My Server"`. The agent logs whether the directory could reach your port.
3. Copy `openlink-server.example.json` and `playlist.example.json` from the OpenLink Server zip to `openlink-server.json` and `playlist.json` and fill them in. **A playlist is required**: OpenLink Server refuses to start without a valid one (`openlink-server check-playlist` checks it). After that, `openlink-server` with no flags runs the server. `openlink-server autostart enable` starts it at logon.

By default OpenLink Server runs in **proxy mode**: the game server listens only on 127.0.0.1 and OpenLink Server fronts the public port. That gives player counts, ping and reachability checks, and `status` / `kick` / `ban` commands, with per-player rate limits.

## Playing

Open the OpenLink app, click **Join** on a server, then in Halo Infinite go to **Custom Game → Create Match → Server** and click the server's name, which the bar at the bottom of the app shows as the game lists it (in capitals). The bar also shows when you are connected. Keep the app open while you play. See [browser/README.md](browser/README.md).

- Windows is the main platform. A Linux build of the app (for the game under Proton) is produced by CI but has not been tested yet.
- If the server is not listed in the game, switch Settings to **LAN broadcast**. When the server runs on the same PC, the app does this by itself.

## Running a directory

The directory is a single small HTTP service. It carries no game traffic: players reach servers directly over UDP. It keeps everything in memory. A restart or redeploy clears the list, and servers register again within seconds. **Run exactly one instance.**

**Container (e.g. Railway).** The repo's `Dockerfile` builds a minimal image. The service listens on `$PORT` when that is set. Configure it with environment variables:

| Variable | Value |
|---|---|
| `OPENLINK_CLIENT_IP_HEADER` | the header your platform's proxy uses for the client address, e.g. `X-Forwarded-For` (the rightmost entry is used, because entries to its left can be forged by clients) |
| `OPENLINK_CLIENT_IP_HOPS` | with `X-Forwarded-For`: how many trusted proxies append to it (default 1). On Railway it is likely `2` (its edge has an outer layer whose address is appended last). Confirm with `/v1/whoami?debug=1` |
| `OPENLINK_REGISTER_KEY` | optional **trusted-host key**. Registration is open to everyone; hosts that send this key may also list an address other than their own IP, such as a tunnel |
| `OPENLINK_REQUIRE_KEY` | `1` makes the directory private: every host needs the register key |
| `OPENLINK_ADMIN_KEY` | enables the admin API below: at least 24 characters (use a long random string; a shorter key leaves the admin API off and the log says so). Keep it secret, and different from the register key. Every admin request is logged, including wrong keys |
| `OPENLINK_BANNED_IPS` | addresses or CIDR ranges that may not list servers, comma separated; these survive redeploys |
| `OPENLINK_BANNED_NAMES` | words not allowed in server names, descriptions, regions or match names, comma separated, any case |
| `OPENLINK_ALLOW_PRIVATE_HOSTS` | `1` lists hosts at private (LAN) and loopback addresses. Only for testing on a LAN (`-allow-private-hosts`); never in production |

The older `HICOMM_…` names of these variables still work, so an existing deployment keeps running; switch to the `OPENLINK_…` names when convenient.

After deploying, open `https://YOUR-DIRECTORY/v1/whoami` from home. It must show **your public IP**. `/v1/whoami?debug=1` also shows the forwarding headers that arrived, to work out the right header and hop count. If it shows a private or proxy address, the client-IP header is wrong, and hosts would be listed under the wrong address.

**Own server.** Run it behind an HTTPS reverse proxy (Caddy, nginx and so on):
```
openlink-directory -listen 127.0.0.1:8080 -client-ip-header X-Forwarded-For
```

**How listings are kept honest.** Anyone can list a server, so the directory checks instead of asking for keys:

- A server appears in the player list only after its game port has answered the directory's probe (within about a minute of starting). A listing that never answers is dropped after 5 minutes (`-confirm-within`). Only proxy-mode hosts answer probes, so OpenLink Server requires proxy mode to be listed.
- Without the register key, a host can only list its own public IP, or a DNS name that resolves to it (dynamic DNS); a name is listed as the address it resolved to, so repointing it later cannot redirect players. This stops the directory being used to point players' traffic at third parties. Private and loopback addresses are refused. The directory only probes listed endpoints, never addresses a caller supplies.
- Limits: 8 listings per IP (per /64 for IPv6), 12 registrations per IP per 10 minutes, 100 listings waiting for their first probe, 500 listings in total.
- Registrations and heartbeats must be sent as `application/json`, which browsers cannot do across sites without permission, so a web page cannot make its visitors register listings.
- Listings expire 45 s after the last heartbeat. A confirmed server that stops answering stays listed, shown as unreachable.

| Endpoint | |
|---|---|
| `POST /v1/servers` | register (returns an ID and an update token) |
| `PUT /v1/servers/{id}` | heartbeat with status and latest beacon (token required) |
| `GET /v1/servers/{id}` | the host's own listing, including whether players see it yet (token required) |
| `DELETE /v1/servers/{id}` | unregister (token required) |
| `GET /v1/servers[?build=…]` | list (confirmed servers only) |
| `GET /v1/servers/{id}/beacon` | latest beacon |
| `GET /v1/whoami` | the address the directory sees for the caller |
| `GET /healthz` | health check |

**Admin API** (only with `OPENLINK_ADMIN_KEY`; send it as `X-Admin-Key`). Bans added here last until the next restart or redeploy; put lasting ones in `OPENLINK_BANNED_IPS` / `OPENLINK_BANNED_NAMES`.

```
curl -H "X-Admin-Key: $KEY" https://YOUR-DIRECTORY/v1/admin/servers                     # every listing, with owner IP and whether it is shown
curl -X DELETE -H "X-Admin-Key: $KEY" https://YOUR-DIRECTORY/v1/admin/servers/ID          # remove one listing
curl -X POST -H "X-Admin-Key: $KEY" -d '{"ip":"203.0.113.7"}' https://YOUR-DIRECTORY/v1/admin/bans   # ban an IP or CIDR range (removes its listings)
curl -X POST -H "X-Admin-Key: $KEY" -d '{"name":"badword"}' https://YOUR-DIRECTORY/v1/admin/bans     # ban a word in server names
curl -H "X-Admin-Key: $KEY" https://YOUR-DIRECTORY/v1/admin/bans                       # list bans; DELETE with the same body to lift one
```

## Building

Requires Go 1.27+. On Windows:

```
.\build.ps1
```

This runs the tests and writes `bin\openlink-server.exe`, `bin\openlink-directory.exe` and `bin\linux-amd64\openlink-directory`. These use only the Go standard library. The player app is built separately in `browser/` (see its README).

## Status and known limitations

- **Tested:** remote players on Windows and on Linux/Proton listed a hosted server through the command-line connector (since retired in favour of the app) and played several full matches in a row. In that test the host's UDP 1343 was exposed through a tunnel rather than a router port forward. A direct port-forwarded host should behave the same, but it has not been tested yet.
- **Lobby control:** the first player to join owns the lobby and picks the map and mode. There is no server-side map rotation yet.
- **Updates:** every game update requires hosts and players to update together. The directory filters servers by build.
- Player counts, ping and reachability need proxy mode on the host (the default). The app's Linux build is untested.
- Proxy mode, bans and several servers on one PC are new and have not been tested with the real game yet.
- Untested: many simultaneous players, long-running uptime, and host CPU and memory use.

## Development

- `go test ./...` runs the unit tests: directory (auth, validation, expiry, limits), UDP forwarder, beacon relay, BootstrapLog parsing, UDP port-owner lookup and the simulator.
- `openlink-server -simulate -loopback` keeps simulated beacons on 127.0.0.1.
- `OPENLINK_DEV_PORTS=21343,27117` moves only the simulator and the app to other ports, so they can be tested on a machine already running a server. The game itself always uses 1343 and 7117.
