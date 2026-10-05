# Host control: preflight, DLL transport and selection backend

The DLL/controller, managed hostagent loader/local administration, server-owned map/mode selection and playlist rotation are implemented and were tested live on build B002 (Steam 22709428, 2026-10-04, runs R016–R018). With a stock client as lobby leader, the server loaded the agent's pinned map/mode pairs and rotated them across consecutive matches. Requests carry map/mode asset IDs plus pinned version IDs, using the game's normal CMS flow and login. Not yet tested: natural match end by time or score limit, several or remote players, Forge maps, engine (type 10) modes, long unattended runs.

## DLL transport

Source: `native/hostctl/`; Go library: `internal/hostctl/{wire,bridge}.go`. Build with the verified VS2017 MSVC x64 tools:

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

## Offline preflight

`hi-host-preflight` is the first offline diagnostic for that backend. It opens an executable read-only, checks its full SHA-256 against the B002 allowlist, validates PE layout, and checks seven candidate handler locations in executable, file-backed sections. It does not launch a game, inspect a process, load a DLL or change content. A verified location establishes matching bytes, not verified function semantics.

Build from the `community` directory with Go 1.27 or later:

```powershell
go build -o bin/hi-host-preflight.exe ./cmd/hi-host-preflight
```

Run the diagnostic against the installed executable:

```powershell
.\bin\hi-host-preflight.exe -exe 'D:\SteamLibrary\steamapps\common\Halo Infinite\game\HaloInfinite.exe'
$LASTEXITCODE
```

Reports are JSON on stdout. Paths containing spaces must be quoted. To save a report, redirect stdout to a private local location; the command itself does not save files. This tool is not included in the release build script yet.

| Exit | Meaning |
|---|---|
| 0 | The on-disk file and all candidate target bytes match the allowlist. |
| 3 | Structurally valid executable rejected: unknown hash, incompatible PE identity or failed target validation. |
| 2 | Invalid arguments, unreadable input, malformed PE or report output error; explanation on stderr. |

`-h` prints help and exits 0. `-exe` is the only input option. There are no launch, attach or process-control commands.

The initial allowlist is B002, Steam build 22709428, SHA-256 `5DA518EC21F5AB7BAEB021AEED2B9892EEB65320DC17D2682F8F55271C203DA8`, amd64 PE32+, preferred image base `0x140000000`. Target RVAs are offsets from that base, rather than absolute runtime addresses. Unknown builds are rejected; there is no operator override for hashes or offsets.

Even exit 0 always reports `control_ready: false` and these pending gates:

- `variant_consumer`: establish how prepared game variants reach the server's content objects.
- `engine_dispatch`: identify a suitable engine execution point for game-control calls.
- `server_lifecycle`: establish startup timing and verified match-end/lobby transitions.

File verification does not prove loaded-module identity, server role, server ownership, safe calls or successful map changes. The future DLL must perform its own loaded-module and role checks and use corroborated engine paths. First acceptance will be a fixed stock pair followed by a second fixed pair at a verified transition; random playlists and custom/Forge entries follow that milestone. See the approved design in the parent workspace's `research/HOST-CONTROL-PLAYLIST.md`.

## Internal selected-option component

The DLL includes choice_adapter.cpp, a bounded conversion/set/cleanup helper for a future engine-thread backend. It currently has no transport binding and is exercised only by owned native fixtures. It borrows validated native choices and a private prepared variant; the real setter also mutates selector-specific native context. Build/role/context/ownership checks and CMS selection enumeration remain backend prerequisites. Run native/hostctl/test-choice.cmd with a private output directory to test control flow; these tests do not prove game application.

The internal saved_choices.cpp snapshot component now matches selected native tree indices to menu schema choices and copies paths/scalar records before conversion. It checks recorded engine thread and bounds through a supplied reader; no live reader is installed. ASCII paths longer than127 bytes or six tokens, unsupported kinds, duplicate/unmatched entries and malformed sources fail explicitly. Empty valid saved trees need no schema read. Native fixture tests and copied-choice interoperability pass; NativePending remains unchanged. Source consistency, CMS ownership, native binding and lifecycle remain backend requirements.

The lan_provider.cpp component selects a native descriptor pair through the authoritative LAN provider, preserving the other15 entries and requiring full readback. It includes explicit RFC/native GUID conversion. The opt-in game_b002 backend validates full executable hash/loaded bytes/table targets/LAN role before installing the exact server Tick slot. Its mailbox drains on the captured actual callback thread in lobby states6/8. Map type is preserved from initialized entry0; Prepare selects mode6 and PrepareEngine mode10. No lifecycle state writes or automatic start are implemented.

Explicit `initialize:true` in a custom selection JSON routes Initialize5, supplying API-derived map2/mode6. Current entries take precedence; absent Current uses valid Requested or zeroes for unselected entries. The authoritative native setter owns initialization and exact full-array readback is required. Omit initialize for ordinary changes that inherit current map type. Map2 native equivalence and actual first selection still need the separately scoped R007 test.

`Selected` (code7, gates55) means exact provider readback for that request, never content loading or match application. Status clears the selection gate; stored IDs/generation are historical. `Applied` (code6, content gate8) is reserved for future verified completion. Unknown/client/non-LAN executables fail closed. Owned backend fixtures pass; R003 demonstrated actual Halo callback/build/role/lobby gates, while R004/R006 did not establish descriptor selection. See the parent workspace's FN008/FN009/FN018 for shutdown/receipt/initialization limits and managed integration.
