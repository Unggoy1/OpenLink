# Release checklist (first public alpha)

Status as of 2026-10-05. Tick items as they are done and note the date and how it was checked. "Tested" means checked with the real game unless it says otherwise.

## Already proven (one player, one PC)

- Player detection, server auto start, no lobby leader (`server_owned`), the server blocks a player's Play / End Game, natural match end, last player leaving.
- Playlist voting end to end with one voter, the chime, the Windows notification (held by Windows' do-not-disturb while gaming).
- Remote players on Windows and Linux/Proton played several matches through a **tunnel** (older test, before lobby control and voting).
- Vote overlay: only in the demo build without Halo (see the overlay section below).

## 1. Tests still needed

### Players and hosting

- [ ] **Two players on two PCs**, at least one remote over the internet:
  - [ ] both join through the app and see the server in Custom Game → Create Match → Server
  - [ ] `lobby_owner: first_player` gives the first player the controls, and only them
  - [ ] `server_owned`: no player can start or end a match
  - [ ] voting with several voters: counts update for everyone, a tie, nobody votes (random pick), a player changes their vote
  - [ ] a player joins mid-vote and gets the ballot; a player leaves mid-vote
- [ ] **A port-forwarded host** (no tunnel). Only the tunnel setup has been tested.
- [ ] **Score-limit match end**; so far only the natural (time) end has been seen.
- [ ] **Empty lobby**: the server withdraws a start when everyone leaves (lobby4/5 builds).
- [ ] **Long unattended run**: several hours and many matches, with host CPU and memory noted.
- [ ] **Bans** through proxy mode with the real game.
- [ ] **Several servers on one PC**: test it, or list it as unsupported for the alpha (the beacon issue in HOSTING.md).
- [ ] **Thumbnails and long names in a live vote** (so far only seen in the demo build). Use a playlist from the unggoy generator with long map and mode names.
- [ ] **What a server is playing, in the server list** (built 2026-10-05; unit-tested and seen in the demo build only). With a real server and the new directory deployed, check each phase: lobby, voting, starting, in game with the map name and thumbnail, match over. Check both a voting server and a rotation-only one.
- [x] **Server name in the in-game list** (built 2026-10-05, R022, same PC): OpenLink Server sends `name` to the DLL, which writes it into the server's beacon. Halo showed it in capitals, cut after about 38 characters. Still to see: the name from another PC; non-ASCII dropped in a live run (unit-tested only).
- [x] **A playlist made by the unggoy generator**: `openlink-server check-playlist` accepts an exported playlist under the stricter checks (user, 2026-10-05).

### Player app

- [x] **Taskbar flash** when a vote opens: user judged it good (2026-10-05).
- [ ] **Linux app** (the CI build) on a real Linux or Proton machine: join, vote in the app panel, overlay settings hidden.

### Vote overlay (Windows, opt-in)

- [ ] Shows over Halo in borderless, in the lobby, after the match and during a match.
- [ ] Appears only while Halo is the window in front, and hides when you alt-tab away or OpenLink is in front.
- [ ] **Passive**: the hotkeys work while Halo has focus. Check whether Halo also reacts to Ctrl (crouch), Alt or the number key.
- [ ] **Interactive**: the hotkey opens the panel. Check whether Halo releases the mouse, any audio cut, pause or stutter, and that focus returns to Halo after voting or Esc.
- [ ] Voting by mouse click in the interactive panel (not tested even in the demo).
- [ ] With Discord's overlay at the same time: both visible, and Discord's hotkey still works.
- [ ] A second monitor, and display scaling above 100% (only one screen at 100% was tested).
- [ ] No noticeable frame-rate cost while it is shown.
- [ ] A hotkey another app already uses: the warning appears in Settings and on the overlay.

## 2. Work still to do

- [x] **Merge** into `master`: done as part of the release (user, 2026-10-05). Work continues on `vote-overlay` so the directory on Railway is not redeployed for every change.
- [x] **CI release run**: several tagged releases have built (user, 2026-10-05). Still worth a smoke test of each artifact on a clean PC before the alpha: the OpenLink Server zip (openlink-server.exe, openlink-control.dll, openlink-loader.exe), the Windows app and the Linux app.
- [ ] **Docs pass**:
  - [ ] HOSTING.md: port forwarding, proxy mode, playlist, voting, `server_owned` / `first_player`, same-PC testing needs Broadcast mode, ToS-risk note.
  - [ ] README "Status and known limitations" is out of date: lobby control and voting now exist, and there is a Linux app build.
  - [ ] A playlist.json reference (limits below).
  - [ ] Player-facing: install, join, voting, the overlay as an experimental option, and that the app does not modify the game.
- [x] **Unggoy playlist generator** follows the limits below: live on unggoy.xyz (user, 2026-10-05).
- [x] **Required, validated playlist** (built 2026-10-05, unit-tested; not yet run against the real game). Every real server needs a playlist; the agent checks it before starting anything and refuses to start on any error. `openlink-server check-playlist [file]` runs the same checks. Errors: IDs or names over 80 bytes, control characters, voting with fewer than 2 entries, and any possible ballot over 1200 bytes with thumbnails. The playlist is read once at start.
  - [ ] Live check: the agent starts the server with a valid playlist and refuses a broken one.
- [ ] **Dockerized host with Wine/Proton** (Linux servers). Not attempted yet; every step is unproven:
  - [ ] Halo Infinite starts in a container under Wine/Proton with no monitor (a virtual display; DX12 through vkd3d-proton needs a Vulkan GPU passed into the container)
  - [ ] sign-in and LAN hosting work inside the container through the game's normal flow
  - [ ] the agent's DLL loads and selects maps and modes under Wine
  - [ ] the agent's Windows-only parts (auto start and the managed game launch with the DLL) work, either by running the Windows agent under Wine beside the game or by giving them a Linux path
  - [ ] players join it from outside (port mapping) and play full matches
  - [ ] image layout: game files mounted rather than baked in (game files and account data must never go into a published image)
- [x] **Website** update (user, 2026-10-05).
- [x] **Open registration with safeguards** (built 2026-10-05, unit-tested and run locally with a simulated server; not yet deployed): anyone can list a server; players see it only after its port answers the probe (unconfirmed listings dropped after 5 min); the register key became an optional trusted-host key (tunnels and other addresses); dynamic-DNS names allowed without a key; 12 registrations per IP per 10 min; admin API (`OPENLINK_ADMIN_KEY`) to list, remove and ban; lasting bans via `OPENLINK_BANNED_IPS` / `OPENLINK_BANNED_NAMES`; OpenLink Server requires proxy mode when listed.
- [ ] **Production directory settings on Railway**: set `OPENLINK_ADMIN_KEY`; keep `OPENLINK_REGISTER_KEY` (or the old `HICOMM_REGISTER_KEY`) as the trusted-host key for tunnels; set `OPENLINK_REQUIRE_KEY=1` only to keep the directory private until release. Check logging.
- [ ] **Live check of the safeguards** after deploying: a port-forwarded host appears within about a minute; a tunnel host with the key appears; a host with a closed port never appears and its log says so; an admin ban removes a listing.
- [ ] **Privacy note**: the directory sees host IP addresses; the app talks to the directory, GitHub (update check) and Halo Waypoint (map thumbnails). It never asks for Xbox credentials.
- [x] **Decide what is in the alpha** (to revisit later; user, 2026-10-05). Current view: overlay = experimental and Windows-only; scripted Forge maps = untested.

## playlist.json limits (for the generator)

- `schema_version` 1; `selection` `shuffle_bag` (default) or `sequential`; at least one enabled entry.
- The host agent enforces these: it will not start with a playlist that breaks them. Run `openlink-server check-playlist playlist.json` on generated files.
- `id`: unique and case-sensitive. Use a readable slug such as `fiesta-slayer-interference`, at most about 32 bytes, from `a-z 0-9 -`. Over 80 bytes is an error. Build it from the mode and map so a regenerated playlist keeps the same IDs.
- `name`: at most **80 UTF-8 bytes** (not characters); more is an error, so trim it yourself: shorten the longer of mode and map with "…" (3 bytes). Players see the `id` if `name` is empty. No control characters (such as newlines). Avoid `&`, `<`, `>`: each costs 6 bytes on the wire.
- The overlay shows about 30 characters of a name on one line; the app panel shows the whole name.
- `map` / `mode` `asset_id` and `version_id`: full 36-character UUIDs, not all zeros. Map thumbnails come from the map IDs; there is no field for them.
- Ballot size: 4 options in a 1200-byte message, thumbnails included; a playlist that could exceed it is rejected. The check assumes the worst case (longest entries, largest numbers). 80-byte IDs with 80-byte names come to 1197 bytes with default settings and fail with `max_players` 0 or very long vote timers; with IDs of about 32 bytes there is plenty of room.
- With voting: at least 2 enabled entries.
- The entry just played is never offered again straight away, so 5 or more entries keeps every ballot full.

## Not needed for the alpha

- Code signing (deferred 2026-10-05; do it before a wider public launch). Plan: choose the licence (AGPL-3.0 likely), ship the unsigned alpha, then apply to SignPath Foundation (free for OSI-licensed projects; publisher shows as "SignPath Foundation") for the OpenLink app and directory. The server package (loader + DLL) may not pass their "circumvent security measures" review; Azure Artifact Signing ($9.99/month, individuals in the US/Canada) is the fallback. Until then: tell testers to expect the SmartScreen warning ("More info → Run anyway"), point to `SHA256SUMS`, and report antivirus false positives to the vendor.

- DLL port plan for a new Halo build: no game updates are expected, only playlist updates. Note that the DLL refuses an unknown game build.
- Overlay on Linux (Wayland blocks global hotkeys and always-on-top windows), and overlay voting with a controller.
- Overlay names on two lines; names longer than 80 bytes.
- Sending `& < >` unescaped in ballots.
- Cosmetic: hide the thumbnail box until it loads; auto-pick Broadcast mode when the app sees a server on the same PC.
- Offline preservation (a later phase).
