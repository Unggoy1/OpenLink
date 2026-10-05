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
  - [ ] both join through the app and see the server in Custom Games → Server
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
- [ ] **Thumbnails in a live vote** (so far only seen in the demo build).
- [ ] **What a server is playing, in the server list** (built 2026-10-05; unit-tested and seen in the demo build only). With a real server and the new directory deployed, check each phase: lobby, voting, starting, in game with the map name and thumbnail, match over. Check both a voting server and a rotation-only one.
- [ ] **A playlist made by the unggoy generator**: it loads, and long names and thumbnails show correctly (limits below).

### Player app

- [x] **Old app against a voting server**: not needed (user, 2026-10-05). Only 4 testers have the app and the first public release is still ahead, so everyone starts on a voting-capable version.
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

- [ ] **Merge** the `host-lobby-control` and `vote-overlay` work into `master`.
- [ ] **CI release run**: tag a pre-release (for example `v0.1.0-alpha.1`). Smoke-test every artifact on a clean PC: the host zip (agent and DLL), the Windows app and the Linux app.
- [ ] **App update path**: make sure the 4 current testers update before testing on voting servers. The first public release ships with voting, so no compatibility work is needed.
- [ ] **Docs pass**:
  - [ ] HOSTING.md: port forwarding, proxy mode, playlist, voting, `server_owned` / `first_player`, same-PC testing needs Broadcast mode, ToS-risk note.
  - [ ] README "Status and known limitations" is out of date: lobby control and voting now exist, and there is a Linux app build.
  - [ ] A playlist.json reference (limits below).
  - [ ] Player-facing: install, join, voting, the overlay as an experimental option, and that the app does not modify the game.
- [ ] **Unggoy playlist generator** follows the limits below.
- [x] **Required, validated playlist** (built 2026-10-05, unit-tested; not yet run against the real game). Every real server needs a playlist; the agent checks it before starting anything and refuses to start on any error. `hi-hostagent check-playlist [file]` runs the same checks. Errors: IDs or names over 80 bytes, control characters, voting with fewer than 2 entries or no custom mode, and any possible ballot over 1200 bytes with thumbnails. The playlist is read once at start.
  - [ ] Live check: the agent starts the server with a valid playlist and refuses a broken one.
- [ ] **Dockerized host with Wine/Proton** (Linux servers). Not attempted yet; every step is unproven:
  - [ ] Halo Infinite starts in a container under Wine/Proton with no monitor (a virtual display; DX12 through vkd3d-proton needs a Vulkan GPU passed into the container)
  - [ ] sign-in and LAN hosting work inside the container through the game's normal flow
  - [ ] the agent's DLL loads and selects maps and modes under Wine
  - [ ] the agent's Windows-only parts (auto start and the managed game launch with the DLL) work, either by running the Windows agent under Wine beside the game or by giving them a Linux path
  - [ ] players join it from outside (port mapping) and play full matches
  - [ ] image layout: game files mounted rather than baked in (game files and account data must never go into a published image)
- [ ] **Website** update (the brief is in the discovery kit: research/sanitized/site-brief-2026-10-05/).
- [ ] **Unsigned executables**: Windows SmartScreen and some antivirus will warn. Either sign the builds or explain the warning on the download page.
- [ ] **Production directory** (openlink-dir.unggoy.xyz): confirm the registration-key policy, request limits, logging and a way to remove abusive listings.
- [ ] **Privacy note**: the directory sees host IP addresses; the app talks to the directory, GitHub (update check) and Halo Waypoint (map thumbnails). It never asks for Xbox credentials.
- [ ] **Decide what is in the alpha** and say so in the docs: overlay = experimental and Windows-only; scripted Forge maps = untested; engine game variants = not supported.

## playlist.json limits (for the generator)

- `schema_version` 1; `selection` `shuffle_bag` (default) or `sequential`; at least one enabled entry.
- The host agent enforces these: it will not start with a playlist that breaks them. Run `hi-hostagent check-playlist playlist.json` on generated files.
- `id`: unique and case-sensitive. Use a readable slug such as `fiesta-slayer-interference`, at most about 32 bytes, from `a-z 0-9 -`. Over 80 bytes is an error. Build it from the mode and map so a regenerated playlist keeps the same IDs.
- `name`: at most **80 UTF-8 bytes** (not characters); more is an error, so trim it yourself: shorten the longer of mode and map with "…" (3 bytes). Players see the `id` if `name` is empty. No control characters (such as newlines). Avoid `&`, `<`, `>`: each costs 6 bytes on the wire.
- The overlay shows about 30 characters of a name on one line; the app panel shows the whole name.
- `map` / `mode` `asset_id` and `version_id`: full 36-character UUIDs, not all zeros. Map thumbnails come from the map IDs; there is no field for them.
- Ballot size: 4 options in a 1200-byte message, thumbnails included; a playlist that could exceed it is rejected. The check assumes the worst case (longest entries, largest numbers). 80-byte IDs with 80-byte names come to 1197 bytes with default settings and fail with `max_players` 0 or very long vote timers; with IDs of about 32 bytes there is plenty of room.
- With voting: at least 2 enabled entries and at least one `custom` mode.
- The entry just played is never offered again straight away, so 5 or more entries keeps every ballot full.

## Not needed for the alpha

- DLL port plan for a new Halo build: no game updates are expected, only playlist updates. Note that the DLL refuses an unknown game build.
- Overlay on Linux (Wayland blocks global hotkeys and always-on-top windows), and overlay voting with a controller.
- Overlay names on two lines; names longer than 80 bytes (old apps reject them, so ship the app change first).
- Sending `& < >` unescaped in ballots.
- Cosmetic: hide the thumbnail box until it loads; auto-pick Broadcast mode when the app sees a server on the same PC.
- Offline preservation (a later phase).
