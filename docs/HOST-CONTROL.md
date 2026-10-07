# Host control: DLL, server-owned selection and playlist rotation

The DLL/controller, managed loader/local administration, server-owned map/mode selection and playlist rotation are implemented and were tested live on build B002 (Steam 22709428, 2026-10-04, runs R016–R018). With a stock client as lobby leader, the server loaded the agent's pinned map/mode pairs and rotated them across consecutive matches. Requests carry map/mode asset IDs plus pinned version IDs, using the game's normal CMS flow and login. Since tested: natural match end by time, remote players (2026-10-04) and Forge maps. Not yet tested: match end by score limit, scripted Forge maps, long unattended runs.

## DLL transport

Source: `native/hostctl/`; Go library: `internal/hostctl/{wire,bridge}.go`. Build with Visual Studio 2017 or later C++ x64 tools (`native/hostctl/vcvars.cmd` finds them; set `VCVARSALL` to override). Releases build the same way in GitHub Actions and publish `OpenLink-Server-windows-amd64.zip` (`openlink-server.exe`, `openlink-control.dll`, `openlink-loader.exe`, example configs; see `packaging/host/`).

```powershell
.\native\hostctl\build.cmd 'C:\build\hostctl'
$env:HOSTCTL_TEST_DLL='C:\build\hostctl\openlink-control.dll'
$env:HOSTCTL_TEST_HARNESS='C:\build\hostctl\hostctl-harness.exe'
go test ./internal/hostctl -count=1
```

Tests load only our own DLL in our own harness. Config supplies controller PID, ephemeral loopback port and a per-launch secret. The DLL verifies connected endpoint ownership before sending its secret. Explicit Start/Stop/Wait run outside DllMain; module is pinned until process exit. Nonblocking I/O checks Stop between bounded readiness waits. Requests are fixed, bounded binary frames; no path/address execution API. Config version1 returns `NativePending` with zero evidence. Version2 explicitly opts into the B002 backend after authentication; unknown executables return `Unsupported`.

Native tests are skipped unless both artifact paths are configured. Enabled tests check idle connection, pending requests, controller EOF, wrong controller PID, malformed length, wrong secret, replay ID and idle Stop. Go also rejects stale/mismatched/unverified Selected/Applied responses. Managed tests invoke the real loader into an owned target and cover shutdown/duplicate/wrong-executable/original-handle binding. Transport success is not operational readiness.

## Managed control

A real server always loads `openlink-control.dll` with the build-pinned native backend, after PID-specific setupComplete, into servers openlink-server launches itself. By default the DLL is the one next to `openlink-server.exe`; `control_dll` in openlink-server.json (or `-control-dll <path>`) overrides it. `openlink-loader.exe` must sit next to the DLL; the program refuses to start if either is missing. Existing servers are never adopted, and `"manage": false` is rejected. `-simulate` loads nothing and ignores the playlist and match settings. Windows access rejection remains a boundary; the loader never adjusts privileges or bypasses protections. An actual Halo test requires separate scoped coordination under the parent workspace's AGENTS.md.

The helper inherits the original managed process handle, verifies its identity and requests Start/Stop outside DllMain. Configuration secret passes over stdin. Agent cancellation/controller EOF requests cleanup; failed cleanup is reported. Module remains pinned until server exit. No automatic reattachment to a partially loaded process. `-restart=false` permits a single managed launch for a scoped test; default managed supervision still restarts exited servers.

The loopback API retains the required `X-OpenLink-Admin: 1` header. `GET /host-control` returns backend status. `POST /host-control/select` accepts this shape (placeholder UUIDs below are examples, not known working content):

```json
{"map":{"asset_id":"00112233-4455-6677-8899-aabbccddeeff","version_id":"10112233-4455-6677-8899-aabbccddeeff"},"mode":{"asset_id":"20112233-4455-6677-8899-aabbccddeeff","version_id":"30112233-4455-6677-8899-aabbccddeeff"}}
```

The mode is a published game variant (UGC, native type 6), including officially published playlist modes. IDs must be canonical/nonzero and versions pinned; map type is preserved from initialized current content. JSON is strict/bounded4096bytes. `openlink-server select <selection.json>` submits the same request locally. A `selected` response establishes descriptor readback only. It does not mean a map was downloaded, a mode's saved settings were applied or a match started. Random playlists require verified loading/start/end and two actual fixed-pair tests first. Parent notes FN013/FN014 and test-card R004 record the next gates.

## Server-owned selection and playlist rotation

A lobby leader's client pushes its own lobby default (for example Bazaar/Slayer) into the server's selection. After a successful selection, the DLL locks entry 0 of the server's selection: any later write from game code keeps its other entries but gets the playlist's map/mode. `GET /host-control` reports `gates` with Locked (64) and Intercepted (128, the lock rewrote another writer). With a v2 DLL it also reports `state` (8 lobby, 11 starting, 9 in game, 10 end of game) and `matches` (matches started). The leader's lobby screen may still show its own map name. The server loads the selected content.

Every server needs a playlist in openlink-server.json; the program refuses to start without one:

```json
{"playlist": "playlist.json"}
```

```json
{
  "schema_version": 1,
  "selection": "shuffle_bag",
  "entries": [
    {"id": "interference-fiesta", "name": "Fiesta Slayer on Interference",
     "map":  {"asset_id": "70f884d7-6869-469d-b4d2-4219627e2d83", "version_id": "cc791b4b-054a-4653-9034-5dc13c809c54"},
     "mode": {"asset_id": "aca7bbf8-7a18-4aae-8785-1bd3f58275fd", "version_id": "3685f6b2-2860-4e98-9d13-513087edb465"}},
    {"id": "kusini-ctf", "name": "CTF: Arena on Kusini Bay",
     "map":  {"asset_id": "4eb7a3ac-81f7-4faa-acd8-ce6bbba667af", "version_id": "98a5391c-4a3a-4f04-bdc7-6db58cc27433"},
     "mode": {"asset_id": "8650f7e0-1f82-4d45-a127-32dd54df06e5", "version_id": "2cb07a58-190d-4fbc-a071-721147faa9e4"}},
    {"id": "interference-fiesta-2s", "name": "Fiesta Slayer on Interference, teams of 2",
     "map":  {"asset_id": "70f884d7-6869-469d-b4d2-4219627e2d83", "version_id": "cc791b4b-054a-4653-9034-5dc13c809c54"},
     "mode": {"asset_id": "aca7bbf8-7a18-4aae-8785-1bd3f58275fd", "version_id": "3685f6b2-2860-4e98-9d13-513087edb465"},
     "teams": {"size": 2}}
  ]
}
```

- `selection`: `shuffle_bag` (default) plays every entry once per cycle in random order, never repeating across a cycle boundary. `sequential` uses file order.
- Each entry has `enabled` (default true). Every mode is a published game variant (UGC), as in a custom game.
- Optional `teams`, for team modes: `{"count": 4}` (number of teams, 2–8) or `{"size": 4}` (players per team, 2–32), or both. Modes do not say how many teams they are made for (since 343's multi-team update every mode allows 8), so this tells the server's team balance how to split players. Without it a team mode uses two teams (Eagle and Cobra). With only `size`, the server makes as many teams as needed and spreads players evenly: 10 players with `"size": 3` get 4 teams of 3, 3, 2 and 2; there are always at least two teams, so `"size": 4` with only 4 players gives two teams of 2. The count or size applies to that one match: a manual selection (admin `select`) uses the default two teams. With both, `count` decides. It applies even with `"team_balance": "off"` (as `"even"`), so nobody stays on a team the entry does not have. FFA modes ignore it. Example: `"teams": {"size": 4}` on a "Multi-Team: Teams of 4" mode.
- Limits, checked when the agent starts (it refuses to start on any error) and by `openlink-server check-playlist [file]`:
  - `id`: required and unique; at most 80 bytes. A short readable slug such as `fiesta-slayer-interference` is best.
  - `name`: optional (players see the `id` without it); at most 80 UTF-8 bytes, not characters (é is 2 bytes, most other scripts 2–3, emoji 4). No control characters in either.
  - Map and mode `asset_id`/`version_id`: canonical, nonzero UUIDs.
  - With voting: at least 2 enabled entries, and the largest possible ballot (the `options` entries with the longest IDs and names, thumbnails included) must fit in 1200 bytes. `&`, `<` and `>` count 6 bytes each. IDs and names both at 80 bytes fit only just (1197 bytes with the default settings) and fail with `max_players` 0 or very long vote timers; with IDs of about 32 bytes, 80-byte names always fit.
  - `check-playlist` always applies the voting checks, so a playlist that passes works on any server.
- The playlist is read once when the agent starts; restart the agent after editing it.
- The agent selects the first entry as soon as the server's lobby is ready, and the next entry each time the server is back in its lobby after a match. A selection that fails three times is skipped.
- `GET /status` shows `host_control.rotation` with `current`, `pending`, `matches`, `selections` and `error`.
- The server list shows what each server is playing. Every heartbeat (every 2 s) carries a `match`: a phase (`lobby`, `voting`, `starting`, `in_game`, `post_game`) from the DLL's lifecycle state and the vote, plus the entry's ID, display name and map thumbnail reference when one applies. In the lobby, a rotation server reports the next entry; a voting server reports none until the vote picks one. The directory lists the report only if it passes the playlist limits (an invalid one is dropped, the heartbeat still counts). The app shows it under the server name, with the map thumbnail, refreshed with the list every 15 s.
- With a tunnel (for example playit) pointed at 127.0.0.1:1343 in proxy mode, set `"server_ip": "127.0.0.2"`. Otherwise the game server receives the tunnel traffic, the directory marks the host unreachable, and the OpenLink app blocks Join. With this setup, five players (four remote) played three matches that cycled a three-entry playlist (2026-10-04).

## Server-owned lobby and automatic start

In a stock LAN lobby the first player to join becomes lobby leader, and any player's Play or pause-menu End Game is applied by the server, whether or not that player leads. OpenLink Server always takes the lobby away from players; there is no setting to turn this off:

- No player becomes lobby owner or lobby leader, so nobody gets lobby options, map/mode menus, Play, or End Game and Restart Match in the pause menu (R025). Matches end on their own time or score limit, and the server starts each one itself: after the vote (`vote`), or with `auto_start`.
- On LAN, a player counts as lobby leader only when their Xbox user ID (XUID) equals the leader XUID the server sends to everyone. The game gives it to the first player who joins. OpenLink Server instead sets it to a placeholder no player has (default 2814749767106559, Xbox XUID format), and the control DLL keeps it there. `lobby_leader_xuid` sets another value; use it only for testing. The agent log line `lobby` shows `leader` and `leader_held`.
- As a backstop, the server also drops players' start and end-game requests (`blocked_start`, `blocked_end`).
- `team_balance` (default `"even"`): in team modes, when each match is prepared, the server spreads every player except observers evenly over the match's teams (two, or the playlist entry's `teams`). `"even"` keeps players on the team they are on where the counts allow and moves only as many as needed, so friends who picked the same team stay together; a team that does not exist in this match (for example Hades from an earlier Slayer match) is reset. `"shuffle"` deals random even teams every match. `"off"` keeps the game's own behaviour (players keep their picks). Players can still change teams during the match. In free-for-all modes the server always keeps every player on their own team, whatever `team_balance` says (with it, two players damaged and killed each other in an FFA King of the Hill mode, 2026-10-06; without it they could not, R024). The `lobby` log line shows each player's team bytes (`peer_teams`: requested/assigned/selected) and `team_fixes` (changes the server made). Even split, shuffle and entry `teams` with several players are not yet tested.
- `auto_start` (for example `{"min_players": 1, "delay_seconds": 30}`): once at least `min_players` players are connected (default 1) and have stayed for `delay_seconds` (default 10), the agent starts the match, as a leader's Play would. This repeats in the lobby after every match. A server without `vote` always uses it, with the defaults when it is not set.
- `GET /status` shows `host_control.lobby`: `lobby.connected` (connected players), `lobby.owner` (leader peer, -1 none), `lobby.start_mode` (1 once a start was requested), `blocked_start` and `blocked_end` (dropped player requests), `starts` and `error`. The agent log line `lobby` records each change.
- Like selection, this changes the running server process (a code patch and two table hooks, removed when control stops). Operators carry the terms-of-service risk of modifying their server. Clients are not modified.

## Server name in the in-game list

The game lists a LAN server under its PC name, read once at start-up into the server's beacon (O080). When the DLL connects, OpenLink Server sends the configured `name` (hostctl operation 8, `SetName`), and on the next engine tick the DLL replaces the name in the beacon object, after checking the object's fields. Every later beacon carries it, so players see it in **Custom Game → Create Match → Server**.

- The name is sanitized first: only printable ASCII is kept, spaces at the ends are trimmed, and it is cut to 47 characters (the beacon holds 48 UTF-16 units with the terminator). If nothing is left, the PC name stays. The log line `in-game server name set` shows the name used.
- The game shows it in capitals, and about 38 characters fit before the list cuts the name off (R022, one 47-character name).
- Not changed: the game's own `system_set_machine_name` override, which the beacon ignores.

## Playlist voting

With a playlist, proxy mode (the default) and the native backend, players can vote for the next match in the OpenLink app:

```json
{"playlist": "playlist.json", "vote": {"seconds": 30, "options": 4, "start_delay_seconds": 5}}
```

- A vote opens when the lobby gets its first player, and again each time a match ends. It offers up to `options` (1-4, default 4) entries drawn at random from the playlist, leaving out the one just played, unless the lobby emptied since: players who arrive at an empty server can get any entry. Give entries a `name`: the app shows it.
- Players vote in the app; each connected player has one vote and can change it until the vote closes after `seconds` (default 30; players have to switch from the game to the app). Most votes wins; ties and "no votes at all" are decided at random among the leading or offered entries.
- The winner is selected, and the match starts `start_delay_seconds` later (default 5). `vote` replaces `auto_start` (do not set both).
- If the lobby empties during a vote, it is cancelled; the next player gets a new one.
- When a vote opens, the app plays a short chime (players can turn it off in Settings), shows a Windows notification and flashes its taskbar button. Windows may hold notifications back while a game runs full screen (automatic do-not-disturb); the chime still plays.
- Vote cards show the map's thumbnail, computed from the entry's map asset and version IDs: the app loads `https://blobs-infiniteugc.svc.halowaypoint.com/ugcstorage/map/<asset>/<version>/images/thumbnail.jpg`, then `.png`, and shows no image if neither exists. Nothing to configure; servers send only the map reference and the app loads images from that host only.
- A player who hosts the server on the same PC needs LAN broadcast: the server owns the discovery port there, so the default loopback announcement never reaches the game. The app switches to it by itself for that session when it sees a server (UDP 1343 held by `HaloInfinite.exe` or `openlink-server.exe`) on the PC.
- Votes travel as small datagrams on the game port, through the same connection as the player's game traffic, so only players who are in the server can vote. The directory is not involved. `GET /status` shows `host_control.vote` (phase, options, counts, winner).

## Internal selected-option component

The DLL includes choice_adapter.cpp, a bounded conversion/set/cleanup helper for a future engine-thread backend. It currently has no transport binding and is exercised only by owned native fixtures. It borrows validated native choices and a private prepared variant; the real setter also mutates selector-specific native context. Build/role/context/ownership checks and CMS selection enumeration remain backend prerequisites. Run native/hostctl/test-choice.cmd with a private output directory to test control flow; these tests do not prove game application.

The internal saved_choices.cpp snapshot component now matches selected native tree indices to menu schema choices and copies paths/scalar records before conversion. It checks recorded engine thread and bounds through a supplied reader; no live reader is installed. ASCII paths longer than127 bytes or six tokens, unsupported kinds, duplicate/unmatched entries and malformed sources fail explicitly. Empty valid saved trees need no schema read. Native fixture tests and copied-choice interoperability pass; NativePending remains unchanged. Source consistency, CMS ownership, native binding and lifecycle remain backend requirements.

The lan_provider.cpp component selects a native descriptor pair through the authoritative LAN provider, preserving the other15 entries and requiring full readback. It includes explicit RFC/native GUID conversion. The opt-in game_b002 backend validates full executable hash/loaded bytes/table targets/LAN role before installing the exact server Tick slot. Its mailbox drains on the captured actual callback thread in lobby states6/8. Map type is preserved from initialized entry0; Prepare selects the published game variant (mode type 6). No lifecycle state writes or automatic start are implemented.

Explicit `initialize:true` in a custom selection JSON routes Initialize5, supplying API-derived map2/mode6. Current entries take precedence; absent Current uses valid Requested or zeroes for unselected entries. The authoritative native setter owns initialization and exact full-array readback is required. Omit initialize for ordinary changes that inherit current map type. Map2 native equivalence and actual first selection still need the separately scoped R007 test.

`Selected` (code7, gates55) means exact provider readback for that request, never content loading or match application. Status clears the selection gate; stored IDs/generation are historical. `Applied` (code6, content gate8) is reserved for future verified completion. Unknown/client/non-LAN executables fail closed. Owned backend fixtures pass; R003 demonstrated actual Halo callback/build/role/lobby gates, while R004/R006 did not establish descriptor selection. See the parent workspace's FN008/FN009/FN018 for shutdown/receipt/initialization limits and managed integration.
