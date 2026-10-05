# Host control: DLL, server-owned selection and playlist rotation

The DLL/controller, managed hostagent loader/local administration, server-owned map/mode selection and playlist rotation are implemented and were tested live on build B002 (Steam 22709428, 2026-10-04, runs R016–R018). With a stock client as lobby leader, the server loaded the agent's pinned map/mode pairs and rotated them across consecutive matches. Requests carry map/mode asset IDs plus pinned version IDs, using the game's normal CMS flow and login. Not yet tested: natural match end by time or score limit, several or remote players, Forge maps, engine (type 10) modes, long unattended runs.

## DLL transport

Source: `native/hostctl/`; Go library: `internal/hostctl/{wire,bridge}.go`. Build with Visual Studio 2017 or later C++ x64 tools (`native/hostctl/vcvars.cmd` finds them; set `VCVARSALL` to override). Releases build the same way in GitHub Actions and publish `OpenLink-host-windows-amd64.zip` (agent, DLL, loader, example configs; see `packaging/host/`).

```powershell
.\native\hostctl\build.cmd 'C:\build\hostctl'
$env:HOSTCTL_TEST_DLL='C:\build\hostctl\hi-hostctl.dll'
$env:HOSTCTL_TEST_HARNESS='C:\build\hostctl\hostctl-harness.exe'
go test ./internal/hostctl -count=1
```

Tests load only our own DLL in our own harness. Config supplies controller PID, ephemeral loopback port and a per-launch secret. The DLL verifies connected endpoint ownership before sending its secret. Explicit Start/Stop/Wait run outside DllMain; module is pinned until process exit. Nonblocking I/O checks Stop between bounded readiness waits. Requests are fixed, bounded binary frames; no path/address execution API. Config version1 returns `NativePending` with zero evidence. Version2 explicitly opts into the B002 backend after authentication; unknown executables return `Unsupported`.

Native tests are skipped unless both artifact paths are configured. Enabled tests check idle connection, pending requests, controller EOF, wrong controller PID, malformed length, wrong secret, replay ID and idle Stop. Go also rejects stale/mismatched/unverified Selected/Applied responses. Managed tests invoke the real loader into an owned target and cover shutdown/duplicate/wrong-executable/original-handle binding. Transport success is not operational readiness.

## Managed hostagent control

`hostctl_dll` in hostagent.json (or `-hostctl-dll <absolute-path>`) explicitly enables loading for servers this agent launches, after PID-specific setupComplete. Place `hostctl-loader.exe` alongside the DLL. `hostctl_native: true` (or `-hostctl-native`) separately enables the build-pinned native backend. Defaults load nothing; existing servers are never adopted. Native/simulation/unmanaged combinations reject. Windows access rejection remains a boundary; the loader never adjusts privileges or bypasses protections. An actual Halo test requires separate scoped coordination under the parent workspace's AGENTS.md.

The helper inherits the original managed process handle, verifies its identity and requests Start/Stop outside DllMain. Configuration secret passes over stdin. Agent cancellation/controller EOF requests cleanup; failed cleanup is reported. Module remains pinned until server exit. No automatic reattachment to a partially loaded process. `-restart=false` permits a single managed launch for a scoped test; default managed supervision still restarts exited servers.

The loopback API retains the required `X-OpenLink-Admin: 1` header. `GET /host-control` returns backend status. `POST /host-control/select` accepts this shape (placeholder UUIDs below are examples, not known working content):

```json
{"map":{"asset_id":"00112233-4455-6677-8899-aabbccddeeff","version_id":"10112233-4455-6677-8899-aabbccddeeff"},"mode":{"asset_id":"20112233-4455-6677-8899-aabbccddeeff","version_id":"30112233-4455-6677-8899-aabbccddeeff"},"mode_kind":"custom"}
```

Mode kind is `custom` (published UGC variant, native6) or `engine` (base EngineGameVariants resource, native10). An officially published playlist mode can use the UGC path; publication alone does not determine this type. IDs must be canonical/nonzero and versions pinned; map type is preserved from initialized current content. JSON is strict/bounded4096bytes. `hi-hostagent select <selection.json>` submits the same request locally. A `selected` response establishes descriptor readback only. It does not mean a map was downloaded, a mode's saved settings were applied or a match started. Random playlists require verified loading/start/end and two actual fixed-pair tests first. Parent notes FN013/FN014 and test-card R004 record the next gates.

## Server-owned selection and playlist rotation

A lobby leader's client pushes its own lobby default (for example Bazaar/Slayer) into the server's selection. After a successful selection, the DLL locks entry 0 of the server's selection: any later write from game code keeps its other entries but gets the hostagent's map/mode. `GET /host-control` reports `gates` with Locked (64) and Intercepted (128, the lock rewrote another writer). With a v2 DLL it also reports `state` (8 lobby, 11 starting, 9 in game, 10 end of game) and `matches` (matches started). The leader's lobby screen may still show its own map name. The server loads the selected content.

To rotate automatically, add a playlist to hostagent.json (requires `hostctl_native`):

```json
{"hostctl_dll": "C:/path/hi-hostctl.dll", "hostctl_native": true, "playlist": "playlist.json"}
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
     "mode": {"asset_id": "8650f7e0-1f82-4d45-a127-32dd54df06e5", "version_id": "2cb07a58-190d-4fbc-a071-721147faa9e4"}}
  ]
}
```

- `selection`: `shuffle_bag` (default) plays every entry once per cycle in random order, never repeating across a cycle boundary. `sequential` uses file order.
- Each entry has `mode_kind` `custom` (default, UGC game variant) or `engine`, and `enabled` (default true). The first match must use a `custom` entry, because it initializes the server's selection.
- The agent selects the first entry as soon as the server's lobby is ready, and the next entry each time the server is back in its lobby after a match. A selection that fails three times is skipped.
- `GET /status` shows `host_control.rotation` with `current`, `pending`, `matches`, `selections` and `error`.
- With a tunnel (for example playit) pointed at 127.0.0.1:1343 in proxy mode, set `"server_ip": "127.0.0.2"`. Otherwise the game server receives the tunnel traffic, the directory marks the host unreachable, and the OpenLink app blocks Join. With this setup, five players (four remote) played three matches that cycled a three-entry playlist (2026-10-04).

## Server-owned lobby and automatic start

By default the first player to join becomes lobby leader, and any player's Play or pause-menu End Game is applied by the server, whether or not that player leads. With a v3 DLL (`hostctl_native`), two options hand the lobby to the server:

```json
{"server_owned": true, "auto_start": {"min_players": 1, "delay_seconds": 30}}
```

- `server_owned`: no player becomes lobby leader, and the server drops players' start and end-game requests. Players still see Play and End Game, but they do nothing (Play shows a local 3-2-1 countdown, then nothing happens). Matches still end on their own time or score limit.
- `lobby_owner` (with `server_owned`): `"none"` (default) means no player becomes lobby leader, so every player who joins on their own sees Play and End Game (they do nothing). `"first_player"` keeps the game's own leader: the first joiner, handed to the longest-present player when they leave. Only that player sees the (inert) buttons; the others see none. Not yet tested with two players.
- `auto_start`: once at least `min_players` players are connected (default 1) and have stayed for `delay_seconds` (default 10), the agent starts the match, as a leader's Play would. This repeats in the lobby after every match. Without `server_owned`, a player's Play can still start the match earlier.
- `GET /status` shows `host_control.lobby`: `lobby.connected` (connected players), `lobby.owner` (leader peer, -1 none), `lobby.start_mode` (1 once a start was requested), `blocked_start` and `blocked_end` (dropped player requests), `starts` and `error`. The agent log line `lobby` records each change.
- Like selection, this changes the running server process (a code patch and two table hooks, removed when control stops). Operators carry the terms-of-service risk of modifying their server. Clients are not modified.

## Playlist voting

With a playlist, proxy mode (the default) and the native backend, players can vote for the next match in the OpenLink app:

```json
{"playlist": "playlist.json", "server_owned": true, "vote": {"seconds": 30, "options": 4, "start_delay_seconds": 5}}
```

- A vote opens when the lobby gets its first player, and again each time a match ends. It offers up to `options` (1-4, default 4) entries drawn at random from the playlist, leaving out the one just played. Give entries a `name`: the app shows it.
- Players vote in the app; each connected player has one vote and can change it until the vote closes after `seconds` (default 30; players have to switch from the game to the app). Most votes wins; ties and "no votes at all" are decided at random among the leading or offered entries.
- The winner is selected, and the match starts `start_delay_seconds` later (default 5). `vote` replaces `auto_start` (do not set both).
- If the lobby empties during a vote, it is cancelled; the next player gets a new one.
- When a vote opens, the app plays a short chime (players can turn it off in Settings), shows a Windows notification and flashes its taskbar button. Windows may hold notifications back while a game runs full screen (automatic do-not-disturb); the chime still plays.
- Vote cards show the map's thumbnail, computed from the entry's map asset and version IDs: the app loads `https://blobs-infiniteugc.svc.halowaypoint.com/ugcstorage/map/<asset>/<version>/images/thumbnail.jpg`, then `.png`, and shows no image if neither exists. Nothing to configure; servers send only the map reference and the app loads images from that host only.
- A player who hosts the server on the same PC must set the app to **LAN broadcast** in Settings: the server owns the discovery port there, so the default loopback announcement never reaches the game.
- Votes travel as small datagrams on the game port, through the same connection as the player's game traffic, so only players who are in the server can vote. The directory is not involved. `GET /status` shows `host_control.vote` (phase, options, counts, winner).

## Internal selected-option component

The DLL includes choice_adapter.cpp, a bounded conversion/set/cleanup helper for a future engine-thread backend. It currently has no transport binding and is exercised only by owned native fixtures. It borrows validated native choices and a private prepared variant; the real setter also mutates selector-specific native context. Build/role/context/ownership checks and CMS selection enumeration remain backend prerequisites. Run native/hostctl/test-choice.cmd with a private output directory to test control flow; these tests do not prove game application.

The internal saved_choices.cpp snapshot component now matches selected native tree indices to menu schema choices and copies paths/scalar records before conversion. It checks recorded engine thread and bounds through a supplied reader; no live reader is installed. ASCII paths longer than127 bytes or six tokens, unsupported kinds, duplicate/unmatched entries and malformed sources fail explicitly. Empty valid saved trees need no schema read. Native fixture tests and copied-choice interoperability pass; NativePending remains unchanged. Source consistency, CMS ownership, native binding and lifecycle remain backend requirements.

The lan_provider.cpp component selects a native descriptor pair through the authoritative LAN provider, preserving the other15 entries and requiring full readback. It includes explicit RFC/native GUID conversion. The opt-in game_b002 backend validates full executable hash/loaded bytes/table targets/LAN role before installing the exact server Tick slot. Its mailbox drains on the captured actual callback thread in lobby states6/8. Map type is preserved from initialized entry0; Prepare selects mode6 and PrepareEngine mode10. No lifecycle state writes or automatic start are implemented.

Explicit `initialize:true` in a custom selection JSON routes Initialize5, supplying API-derived map2/mode6. Current entries take precedence; absent Current uses valid Requested or zeroes for unselected entries. The authoritative native setter owns initialization and exact full-array readback is required. Omit initialize for ordinary changes that inherit current map type. Map2 native equivalence and actual first selection still need the separately scoped R007 test.

`Selected` (code7, gates55) means exact provider readback for that request, never content loading or match application. Status clears the selection gate; stored IDs/generation are historical. `Applied` (code6, content gate8) is reserved for future verified completion. Unknown/client/non-LAN executables fail closed. Owned backend fixtures pass; R003 demonstrated actual Halo callback/build/role/lobby gates, while R004/R006 did not establish descriptor selection. See the parent workspace's FN008/FN009/FN018 for shutdown/receipt/initialization limits and managed integration.
