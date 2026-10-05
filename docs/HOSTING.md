# Hosting a server

This guide sets up a Halo Infinite community server that players anywhere can join with OpenLink.

## What you need

- Windows with Halo Infinite installed through Steam, on **the same game version** as your players.
- A router where you can forward a UDP port, and a public IPv4 address (not carrier-grade NAT).
- The host package from the latest release (`hi-hostagent.exe`, `hi-hostctl.dll`, `hostctl-loader.exe` and the example files).
- **A playlist.** Every OpenLink server runs from one: the server picks the map and mode of every match. The agent will not start without a valid playlist. To host without one, host an ordinary custom game instead of using OpenLink.

## 1. Forward the game port

On your router, forward **UDP 1343** to the PC that runs the server. Use a different external port if you like (for example, external 21343 → internal 1343) and pass it as `-public-port`.

Allow the agent through the Windows firewall (run once, as administrator):

```
netsh advfirewall firewall add rule name="OpenLink host agent" dir=in action=allow protocol=UDP localport=1343 program="C:\path\to\hi-hostagent.exe"
```

The game server itself does not need a firewall rule: in proxy mode (the default) it only listens on 127.0.0.1, and the agent forwards players to it.

## 2. Check reachability before using the game

```
hi-hostagent -simulate -directory https://openlink-dir.unggoy.xyz -name "My Server"
```

Within about a minute the agent logs either `the directory reached your server from the internet` or `the directory could NOT reach your server`. In the second case, re-check the port forward and the firewall rule. Stop the simulation (Ctrl+C) when it passes.

## 3. Set up the config and playlist, then run

1. Copy `hostagent.example.json` to `hostagent.json` next to the program and fill in `name`, `region`, `register_key` and `public_port`. It already sets the required `hostctl_dll`, `hostctl_native` and `playlist`.
2. Copy `playlist.example.json` to `playlist.json` and list your map/mode pairs (format and limits: [HOST-CONTROL.md](HOST-CONTROL.md)).
3. Check it, then run:

```
hi-hostagent check-playlist
hi-hostagent
```

Running `hi-hostagent` with no flags uses `hostagent.json`; flags override it. The agent checks the playlist before it starts anything and stops with an error if the file is missing or invalid: bad IDs, an `id` or `name` over 80 bytes, or (with voting) a ballot that would not fit. The playlist is read once at start, so restart the agent after editing it.

The agent starts the game server (`HaloInfinite.exe -server -console -lan -lan_sandbox RETAIL -bindip 127.0.0.1`) in its own console window, restarts it if it exits, and keeps the listing fresh. Leave both windows open.

### Start automatically

```
hi-hostagent autostart enable
```

This creates a Windows Task Scheduler task that starts the agent **when you log on**. `autostart status` and `autostart disable` check or remove it.

Why a logon task and not a Windows service: a service runs in a separate, non-interactive session under another account. The game server expects the normal user environment (your Steam install and profile), so it is not expected to work as a service. To run unattended, set Windows to log on automatically.

## Proxy mode (default)

The agent listens on the public port and passes each player's traffic to the server on 127.0.0.1. Packets are never read or changed. This gives you:

- **Player count** in the server list.
- **Reachability**: the directory periodically checks that your port answers, and players see your **ping**.
- **Admin**: see players, kick and ban by IP, a per-player packet limit (`max_pps`, default 500/s, about ten times normal play) and a connection limit (`max_players`, default 32).

To turn it off, use `-proxy=false`. The server then listens on the public port itself, and counts, ping and bans are not available.

## Admin commands

Run these in a second terminal while the agent is running:

```
hi-hostagent status              server state, players (IP:port, time connected), reachability
hi-hostagent kick 203.0.113.7    disconnect and keep them out for 10 minutes
hi-hostagent ban 203.0.113.7     permanent ban (add minutes for a temporary one)
hi-hostagent unban 203.0.113.7
hi-hostagent bans
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

Use one config file per server (`hi-hostagent -config serverB.json`) and forward both ports.

Known limitation: every LAN server on a PC broadcasts its beacon from the same address, so each agent may relay another server's beacon. Players still connect to the right server (the connection goes through that agent's port), but the in-game list could show another server's status. This has not been tested yet.

## Settings reference (`hostagent.json`)

| Key | Default | Meaning |
|---|---|---|
| `directory` | | directory URL |
| `register_key` | | directory registration key, if required (keep the file private) |
| `name`, `region` | PC name, empty | shown in the browser |
| `public_host` | (your public IP) | address players use, e.g. a DNS name (needs `register_key`) |
| `public_port` | 1343 | external UDP port |
| `install` | auto | game folder containing version.txt |
| `manage` | true | start and restart the server |
| `stop_server` | false | stop the server when the agent exits |
| `proxy` | true | proxy mode |
| `listen` | 0.0.0.0:1343 | proxy: public UDP address |
| `server_ip` | 127.0.0.1 | proxy: the server's private address |
| `max_players` | 32 | proxy: simultaneous player connections (0 = no limit) |
| `max_pps` | 500 | proxy: packets per second per player (0 = no limit) |
| `bind_ip` | | without proxy: the server's `-bindip` |
| `admin` | 127.0.0.1:7180 | local admin API ("" = off) |
| `bans_file` | bans.json | ban list |
