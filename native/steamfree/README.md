# Steam-free steam_api64.dll (experimental)

A replacement `steam_api64.dll` that lets the Halo Infinite LAN server
(`game\HaloInfinite.exe -server ...`, build B002) run with no Steam client on
the PC. It goes in a **separate server copy** of the game. It is never for a
player's install: it refuses to initialize in a process without `-server`.

Status: **tentatively working.** A real B002 server reached a ready OpenLink
lobby with Steam closed (native control connected, LAN beacons, directory
listing and reachability probe), and stayed idle for several minutes. Not
tested yet: players joining, matches, repeated matches, and Linux/Proton.
Xbox/Halo sign-in and services are unchanged.

## What it implements

Only the Steam calls a B002 server process makes, found by tracing it:

- `SteamAPI_Init` / `Shutdown` / `RestartAppIfNecessary`, handles, the
  three-word `SteamInternal_ContextInit` cells, and callback
  registration/unregistration. `SteamAPI_RunCallbacks` delivers no events.
- `SteamUtils009`: `SetWarningMessageHook`.
- `STEAMAPPS_INTERFACE_VERSION008`: language (`english`), empty launch command
  line and query parameters, and `BIsSubscribedApp`. Only the free base app
  (1240440) is reported; the optional HD and campaign entries stay off. It
  implements no ownership or purchase service.
- `STEAMHTMLSURFACE_INTERFACE_VERSION_005`: `Shutdown`.

Every other export, interface or vtable slot logs its name and stops the
process with exception `0xe0534645`, instead of returning a guessed value.
That includes `SteamUser020` (auth tickets) and asynchronous call results. The
DLL creates no tickets and has no Steam IPC; it imports only KERNEL32 and SHELL32.

## Diagnostics

The DLL writes `steamfree-<pid>.jsonl` next to itself: API, interface and
method names, callback and app IDs, timing. No credentials or payloads. At most
100000 records. When a server stops with `0xe0534645`, the last
`unsupported_*` record names the missing call.

## Files

| File | Purpose |
|---|---|
| `core.cpp` | the implementation |
| `exports-b002.tsv` | the 995 exports (ordinal, kind, name) of B002's `steam_api64.dll`, SHA-256 473F5A31…2F1E |
| `generate.ps1` | writes `exports.def`, `unsupported.asm` (one named stop per unimplemented export and slot) and `generated.h` from the table |
| `build.cmd OUT` | generates, assembles and links `OUT\steam_api64.dll` (/MT) |
| `test.cmd OUT` | builds, then runs `tests\run.ps1`: export parity with the table, and `tests\harness.cpp` for ABI, string and buffer bounds, context cache, eight threads, callback flags, non-server refusal and every unsupported-call stop. It never launches Halo. |

The game gets no more updates, so the table is final. The release workflow
builds and tests the DLL and ships it in the OpenLink Server zip as
`steam-free\steam_api64.dll`.
