# Hosting a server

This guide sets up a Halo Infinite community server that players anywhere can join with OpenLink.

## What you need

- Windows with Halo Infinite installed through Steam, on **the same game version** as your players. Each OpenLink Server release supports one Halo build: after a Halo update it refuses to start (and says so in its log) until an OpenLink Server update for the new build is out.
- A router where you can forward a UDP port, and a public IPv4 address (not carrier-grade NAT).
- The host package from the latest release (`openlink-server.exe`, `openlink-control.dll`, `openlink-loader.exe` and the example files).
- **A playlist.** Every OpenLink server runs from one: the server picks the map and mode of every match. The agent will not start without a valid playlist. To host without one, host an ordinary custom game instead of using OpenLink.

## 1. Forward the game port

On your router, forward **UDP 1343** to the PC that runs the server. Use a different external port if you like (for example, external 21343 → internal 1343) and pass it as `-public-port`.

Allow OpenLink Server through the Windows firewall (run once, as administrator):

```
netsh advfirewall firewall add rule name="OpenLink Server" dir=in action=allow protocol=UDP localport=1343 program="C:\path\to\openlink-server.exe"
```

The game server itself does not need a firewall rule: in proxy mode (the default) it only listens on 127.0.0.1, and OpenLink Server forwards players to it.

### Automatic port forwarding (opt-in)

Instead of forwarding the port by hand, you can let OpenLink Server ask your router to do it while it runs. Add this to `openlink-server.json`:

```json
"auto_port_forward": true
```

It is **off unless you turn it on**. When on, OpenLink Server asks the router with UPnP, or NAT-PMP if UPnP does not answer, to forward `public_port` (UDP) to this PC, renews the forward every half hour and removes it when OpenLink Server exits. The log says which method worked, or why none did (often UPnP is switched off in the router; then forward the port by hand as above). It also warns when the router itself has no public address (carrier-grade NAT or a second router in front of it): then no forward on your router can make the server reachable. You still need the firewall rule above.

UPnP lets any program on your network open ports on your router without asking you. Many people keep it switched off for that reason, and you do not need it if you forward the port by hand.

### Disclaimer

OpenLink is community software provided as is, without warranty. Hosting a server opens a port on your network to the internet, and you do so at your own risk. The OpenLink and unggoy developers are not responsible for changes you or OpenLink Server make to your router, firewall or network, including automatic port forwarding, or for any consequences of exposing your PC to the internet.

## 2. Check reachability before using the game

```
openlink-server -simulate -directory https://openlink-dir.unggoy.xyz -name "My Server"
```

Within about a minute openlink-server logs either `the directory reached your server from the internet` or `the directory could NOT reach your server`. In the second case, re-check the port forward and the firewall rule. Stop the simulation (Ctrl+C) when it passes.

## 3. Set up the config and playlist, then run

1. Copy `openlink-server.example.json` to `openlink-server.json` next to the program and fill in `name`, `description` (optional), `region` and `public_port` (`register_key` is only for listing a tunnel address; see the settings below). It already points `playlist` at `playlist.json`. Keep `openlink-control.dll` and `openlink-loader.exe` in the same folder: the server finds them there (the program refuses to start if they are missing).
2. Copy `playlist.example.json` to `playlist.json` and list your map/mode pairs (format and limits: [HOST-CONTROL.md](HOST-CONTROL.md)).
3. Check it, then run:

```
openlink-server check-playlist
openlink-server
```

Running `openlink-server` with no flags uses `openlink-server.json`; flags override it. It checks the playlist before it starts anything and stops with an error if the file is missing or invalid: bad IDs, an `id` or `name` over 80 bytes, or (with voting) a ballot that would not fit. The playlist is read once at start, so restart OpenLink Server after editing it.

OpenLink Server starts the game server (`HaloInfinite.exe -server -console -lan -lan_sandbox RETAIL -bindip 127.0.0.1`) in its own console window, restarts it if it exits, and keeps the listing fresh. Leave both windows open. Players see your server once the directory has reached its port, usually within a minute; the log says `your server is now in the server list`.

## Logs and asking for help

OpenLink Server writes its log to `openlink-server.log` next to `openlink-server.json` (moved to `openlink-server.log.1` once it passes 5 MB), so it is kept even when OpenLink Server runs from autostart without a window. Reasons it refuses to start, such as a missing DLL, a bad playlist or a Halo update, are logged there too.

When you ask for help, run:

```
openlink-server diagnostics
```

It writes `openlink-diagnostics-<date>-<time>.txt` next to the log: versions, your settings, the game build check, the control files, the playlist check, the running server's status, autostart and the end of the log. Public IP addresses, keys and your Windows user folder are removed. Look it over, then attach it.

### Start automatically

```
openlink-server autostart enable
```

This creates a Windows Task Scheduler task that starts OpenLink Server **when you log on**. `autostart status` and `autostart disable` check or remove it.

Why a logon task and not a Windows service: a service runs in a separate, non-interactive session under another account. The game server expects the normal user environment (your Steam install and profile), so it is not expected to work as a service. To run unattended, set Windows to log on automatically.

## Proxy mode (default)

OpenLink Server listens on the public port and passes each player's traffic to the server on 127.0.0.1. Packets are never read or changed. This gives you:

- **Player count** in the server list.
- **Reachability**: the directory periodically checks that your port answers, and players see your **ping**.
- **Admin**: see players, kick and ban by IP, a per-player packet limit (`max_pps`, default 500/s, about ten times normal play) and a connection limit (`max_players`, default 32).

Proxy mode is required for a listed server: the directory only shows servers whose port has answered its probe, and only proxy mode answers. `"proxy": false` is accepted only with `"directory": ""` (an unlisted server for your own LAN).

## Admin commands

Run these in a second terminal while OpenLink Server is running:

```
openlink-server status              server state, players (IP:port, time connected), reachability
openlink-server kick 203.0.113.7    disconnect and keep them out for 10 minutes
openlink-server ban 203.0.113.7     permanent ban (add minutes for a temporary one)
openlink-server unban 203.0.113.7
openlink-server bans
```

Bans are stored in `bans.json` next to the config. Players are identified by IP address only. The game's own player names are inside its encrypted traffic, which OpenLink never reads.

The admin API listens on 127.0.0.1:7180 (setting `admin`) and only accepts requests from this PC.

## Several servers on one PC (experimental)

Each server needs its own private address and public port:

| | server A | server B |
|---|---|---|
| `server_ip` | 127.0.0.1 | 127.0.0.2 |
| `listen` | 0.0.0.0:1343 | 0.0.0.0:1344 |
| `public_port` | 1343 | 1344 |
| `admin` | 127.0.0.1:7180 | 127.0.0.1:7181 |

Use one config file per server (`openlink-server -config serverB.json`) and forward both ports.

Known limitation: every LAN server on a PC broadcasts its beacon from the same address, so each OpenLink Server may relay another server's beacon. Players still connect to the right server (the connection goes through that agent's port), but the in-game list could show another server's status. This has not been tested yet.

## Settings reference (`openlink-server.json`)

| Key | Default | Meaning |
|---|---|---|
| `directory` | https://openlink-dir.unggoy.xyz | directory URL; `""` = not listed |
| `register_key` | | trusted-host key from the directory operator; only needed to list a tunnel or other address (`public_host`). Keep the file private |
| `name`, `region` | PC name, empty | `name` (1-48 characters) is shown in the OpenLink app and in Halo's in-game server list (Custom Game → Create Match → Server). In game only printable ASCII is kept (other characters are dropped), at most 47 characters are sent, the game shows them in capitals and about 38 fit before it cuts the name off. If nothing printable is left, the game shows the PC name |
| `description` | | one line (up to 120 characters) shown under the name in the OpenLink app, for example "Casual BTB, be nice" |
| `public_host` | (your public IP) | address players use. A DNS name pointing to your own IP (dynamic DNS) works as is; a tunnel or any other address needs `register_key` |
| `public_port` | 1343 | external UDP port |
| `install` | auto | game folder containing version.txt |
| `manage` | true | start and restart the server (must stay true) |
| `stop_server` | false | stop the game server when openlink-server exits |
| `proxy` | true | proxy mode |
| `auto_port_forward` | false | opt-in: ask the router (UPnP, then NAT-PMP) to forward `public_port` to this PC while running; see [Automatic port forwarding](#automatic-port-forwarding-opt-in) |
| `listen` | 0.0.0.0:1343 | proxy: public UDP address |
| `server_ip` | 127.0.0.1 | proxy: the server's private address |
| `max_players` | 32 | proxy: simultaneous player connections (0 = no limit) |
| `max_pps` | 500 | proxy: packets per second per player (0 = no limit) |
| `bind_ip` | | without proxy: the server's `-bindip` |
| `admin` | 127.0.0.1:7180 | local admin API ("" = off) |
| `bans_file` | bans.json | ban list |
