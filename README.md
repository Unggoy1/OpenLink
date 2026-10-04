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
- **`hi-connector`** runs on each player's PC. For the chosen server it replays that server's own beacon to the local game and forwards the game's traffic to the server. To the game, it looks like an ordinary LAN game.

The connector forwards game packets unchanged. They are encrypted by the game, and this project never reads, decrypts or alters them.

**What this project does not do:** it does not modify game files, touch game memory or interact with anti-cheat. It does not handle Xbox/Microsoft credentials: players sign in inside the game as usual, and the directory stores only what hosts send it (name, address, build, status, beacon).

## Requirements

- Halo Infinite on Steam, **the same game build on the server and on every player's PC**. The game refuses a version mismatch, and the tools check the build for you.
- Server host: Windows, a public IPv4 address, and **UDP 1343 forwarded** to the server machine.
- Players: Windows, or Linux with the game running under Proton. No port forwarding needed.

## Hosting a server

1. **Forward UDP 1343** on your router to the server machine, and allow it in the Windows firewall.
2. **Check reachability before involving the game.** Run the agent in simulation mode, then ask someone outside your network to probe it:
   ```
   hi-hostagent -simulate -directory https://DIRECTORY -name "My Server"
   hi-connector -directory https://DIRECTORY probe "My Server"      (run by the other person)
   ```
   `5/5 echoes` means your port forward works. `0/5` means UDP 1343 is not reaching you.
3. **Run the real server.** Stop the simulation, then:
   ```
   hi-hostagent -directory https://DIRECTORY -name "My Server" -region us-west
   ```
   The agent finds the game install, then starts `HaloInfinite.exe -server -console -lan -lan_sandbox RETAIL` in its own console window. Within a few seconds it logs `server owns UDP 1343` and `listening for server beacons`, and the server shows as joinable in the directory.

Useful agent options:

| Option | Use |
|---|---|
| `-install <folder>` | game folder, if it is not in a standard Steam library |
| `-public-host`, `-public-port` | the address players should use, if it differs from the address the directory sees (for example a DNS name, or a different external port). A host other than your own IP requires the directory's `-register-key` |
| `-bind-ip <ip>` | bind the server to one local IP. The game always uses port 1343, so one machine can run one server per IP address |
| `-manage=false` | do not start the server; watch one you started yourself |
| `-stop-server` | stop the server when the agent exits. By default it is left running |
| `-register-key` | key for directories that restrict registration |

If a server is already running on UDP 1343, the agent watches it instead of starting a second one. If a server it started never binds UDP 1343, the agent stops it rather than listing an unjoinable server.

## Playing

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

```
hi-directory -listen 127.0.0.1:8080 -register-key SECRET
```

Put it behind an HTTPS reverse proxy (Caddy, nginx and so on) and pass `-trust-proxy`, so it sees players' real addresses from `X-Forwarded-For`. Listings expire 45 s after the last heartbeat. Without the register key, a host can only list its own public IP; this stops the directory being used to point players' traffic at third parties. The directory keeps everything in memory, and a restart simply waits for hosts to register again.

| Endpoint | |
|---|---|
| `POST /v1/servers` | register (returns an ID and an update token) |
| `PUT /v1/servers/{id}` | heartbeat with status and latest beacon (token required) |
| `DELETE /v1/servers/{id}` | unregister (token required) |
| `GET /v1/servers[?build=…]` | list |
| `GET /v1/servers/{id}/beacon` | latest beacon |

## Building

Requires Go 1.22+. On Windows:

```
.\build.ps1
```

This runs the tests and writes `bin\*.exe`, plus `bin\linux-amd64\hi-connector` and `hi-directory`. The code uses only the Go standard library.

## Status and known limitations

- **Tested:** remote players on Windows and on Linux/Proton listed a hosted server through the connector and played several full matches in a row. In that test the host's UDP 1343 was exposed through a tunnel rather than a router port forward. A direct port-forwarded host should behave the same, but it has not been tested yet.
- **Lobby control:** the first player to join owns the lobby and picks the map and mode. There is no server-side map rotation yet.
- **Updates:** every game update requires hosts and players to update together. The directory filters servers by build.
- The player count is not reported yet. There is no graphical browser yet; it is command line only.
- Untested: many simultaneous players, long-running uptime, and host CPU and memory use.

## Development

- `go test ./...` runs the unit tests: directory (auth, validation, expiry, limits), UDP forwarder, beacon relay, BootstrapLog parsing, UDP port-owner lookup and the simulator.
- `hi-hostagent -simulate -loopback` keeps simulated beacons on 127.0.0.1.
- `HICOMM_DEV_PORTS=21343,27117` moves only the simulator and the connector to other ports, so they can be tested on a machine already running a server. The game itself always uses 1343 and 7117.
