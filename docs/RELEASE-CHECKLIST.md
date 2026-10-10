# Release checklist (first public alpha)

Status as of 2026-10-08 (latest release v0.7.6: bot backfill came in v0.7.5, server hardening, the watchdog, end-match, several servers per PC and the app leaving offline servers in v0.7.6; both lightly tested). Tick items as they are done and note the date and how it was checked. "Tested" means checked with the real game unless it says otherwise. Tests are grouped by what they need, so one session can cover a whole group.

## Done

Tested live (one player, one PC, unless noted):

- Player detection, server auto start, no lobby leader, the server blocks a player's Play / End Game, natural match end, last player leaving (R019).
- Server-held lobby leader (always on since 2026-10-06; the `server_owned` and `lobby_owner` settings are gone): players get no lobby options, map/mode menus or Play, and no End Game or Restart Match in the pause menu; holds through a full match cycle (R025, FN027). More players not needed (user decision).
- Playlist voting end to end with one voter, the chime, the Windows notification (held by Windows' do-not-disturb while gaming), taskbar flash (R021, user).
- Remote players on Windows and Linux/Proton played several matches through a tunnel (older test, before lobby control and voting).
- Server name in Halo's in-game list: shown in capitals, cut after about 38 characters (R022).
- Halo build check passes on the supported build, `openlink-server.log` is written, the listing carries the description (R023).
- Controller voting: View + D-pad counted a vote over Halo; the interactive overlay showed the full panel with View labels. Halo also moves its menu highlight with the D-pad; nothing is selected (R023, user).
- An unggoy-generated playlist passes `openlink-server check-playlist` (user).

Built and checked without the game (unit tests, demo build or a simulated server):

- Required, validated playlist; the agent refuses to start on any playlist error.
- Open directory registration with safeguards (probe before listing, rate limits, admin API, bans).
- Halo update check: refuses to start on another build or a modified exe, with a message naming both builds. The app says whether the player's Halo or the servers are behind.
- Diagnostics: `openlink-server diagnostics` and the app's Settings → Help → Copy diagnostics, with public IPs, keys and the user folder removed.
- Opt-in `auto_port_forward` (UPnP, then NAT-PMP) with the disclaimer in HOSTING.md and the server README.
- Server description in the listing and the app.
- The app picks LAN broadcast by itself when a server runs on the same PC.
- Passive overlay is the default on Windows.
- Overlay option names wrap onto two lines, with full-height thumbnails (seen in the demo build with names from a real playlist, user).
- An empty lobby resets the vote pool: the next vote can offer every entry.
- Security review fixes (2026-10-05, unit-tested; a short live start passed): per-IP and unanswered-session limits in the proxy, votes and player counts only from connections the game answered, directory DNS names listed as their resolved IP, bounded rate-limit records, JSON-only registration, a cap on unconfirmed listings, private hosts refused, admin key of 24+ characters and logged admin actions, duplicate IDs rejected in the app, the app's relay locked to this PC, https-only directories (http for LAN testing), admin API Host/Origin checks, UPnP trusts only the gateway, thumbnail size checks, update links limited to the GitHub releases page, a Content-Security-Policy, pinned GitHub Actions, `npm ci` builds and a fuller `.gitignore`.
- Merge to `master`, tagged CI releases, website update, unggoy playlist generator limits (user).

## 1. Tests you can run alone on your PC

Run a voting server with the local directory (as in R023) and join it with the OpenLink app.

### Joining and hosting on one PC

- [ ] Joining picks LAN broadcast by itself and the session bar says "Using LAN broadcast because a server runs on this PC".
- [ ] The app's bottom bar names the server exactly as the in-game list shows it.
- [ ] A name with non-ASCII characters (for example "Café ★ Night"): the in-game list shows it without them ("CAF  NIGHT").
- [ ] The server list shows what the server is playing in each phase: lobby, voting, starting, in game with the map name and thumbnail, match over. Check a voting server and a rotation-only one (`auto_start`, no `vote`).

### Voting

- [ ] Player count and voting still work through the proxy after the security changes (only connections the game answered count).
- [ ] A vote after a match (not only the first one in the lobby).
- [ ] Everyone leaves, then someone joins: the first vote can include the map just played (the log says "every playlist entry can be offered again").
- [ ] Thumbnails and long names in a live vote. Use an unggoy playlist with long map and mode names.
- [ ] Score-limit match end; so far only the time limit has ended a match.
- [ ] Empty lobby during "starting": the server withdraws the start when everyone leaves before the match begins.

### Vote overlay (Windows)

- [ ] **New default position and size** (2026-10-08): the panel now opens top left (over the lobby's map preview, which can show another map than the vote's) and is sized by the monitor's height (browser/overlay_scale.go: 25% above its design size at 1440 pixels high, in proportion elsewhere, never below 80% of the display-scaling size), so it takes the same share of the screen height on 1080p, 1440p and 4K. Looks right on a 3440x1440 ultrawide, before and after the height scaling (user, 2026-10-08, R031). Still to check: a 1080p and a 4K screen.
- [ ] **Passive** (the default): the panel shows by itself; Ctrl+Alt+1–4 vote while Halo has focus. Check whether Halo also reacts to Ctrl (crouch), Alt or the number keys.
- [ ] Passive with a controller connected: rows show both labels, for example `Ctrl+Alt+1 · View+↑`.
- [ ] **Interactive**: Ctrl+Alt+V opens the panel. Check whether Halo releases the mouse, any audio cut, pause or stutter, and that focus returns to Halo after voting or Esc.
- [ ] Voting by mouse click in the interactive panel.
- [ ] Shows only while Halo is in front; hides when you alt-tab away or OpenLink is in front.
- [ ] With Discord's overlay at the same time: both visible, and Discord's hotkey still works.
- [ ] A second monitor, and display scaling above 100% (only one screen at 100% was tested).
- [ ] No noticeable frame-rate cost while it is shown.
- [ ] A hotkey another app already uses: the warning appears in Settings and on the overlay.

### Host tools

- [ ] A broken playlist: OpenLink Server refuses to start and `openlink-server.log` says why.
- [ ] `openlink-server diagnostics` on your real setup: read the report and check nothing private is left.
- [ ] The app's Copy diagnostics after a real join: the events show the join, the session phases and your votes.
- [ ] Bans through proxy mode: `openlink-server ban <ip>` keeps that player out.
- [ ] Autostart: `openlink-server autostart enable`, log off and on; the server starts and the log file shows it.
- [ ] Long unattended run: several hours and many matches; note host CPU and memory.

### Mode families and match flow

These could quietly break the way FFA did (R024): each is a kind of match the server's selection, lifecycle and team code has not run yet (listed 2026-10-06).

- [ ] **Multi-round modes** (3-round CTF, Oddball, Strongholds with rounds, Last Spartan Standing): no vote opens and team balance does not run again between rounds; the next vote opens only after the whole match. Watch the lifecycle `state` in the `lobby` log lines at each round change.
- [ ] **Map or mode that cannot load**: a playlist entry with a made-up or deleted version ID. The server should skip to another entry (or report it) instead of hanging in loading; note what the log and the players see.
- [ ] **Crash recovery**: end the game server process (Task Manager) in the lobby and again mid-match. OpenLink Server should start it again, load the DLL, select an entry, hold the lobby leader and list the server again, and players should be able to rejoin.
- [ ] **Bot and AI modes** (a Fiesta mode with bots, a Firefight-style mode): match starts, bots spawn, the FFA guard and team balance leave bots alone.
- [ ] **Scripted Forge modes** (node-graph scripts that set teams, scores or rounds): the script still runs and is not undone by team balance.

### Bot backfill (released in v0.7.5; tested alone 2026-10-08, R030)

Set `"bot_backfill": {}` in openlink-server.json and `"bots": true` on the playlist entries to test; watch the `bots` log lines (HOST-CONTROL.md, Bot backfill).

- [x] **Probe** (R030, 2026-10-08): `supported=true`, `thread_role=1` (main thread). The BOT8 Fiesta mode reports its own FFA bots (`mode_spawns_bots=4`, `state=mode_bots`) and is left alone. Regular Fiesta Slayer and CTF Arena have bots enabled with no built-in bots and all four difficulties registered.
- [x] **One player, regular modes** (R030): Fiesta Slayer on Interference and CTF Arena on Kusini Bay each filled to `fill_to` 4 with 3 bots added one by one; CTF split 2 against 2 (player + 1 bot vs 2 bots); a whole CTF match played; nothing refused; when the player left, bots started leaving.
- [ ] **Bot difficulty**: Marine played poorly (user); ODST was used for the CTF match, how it felt not noted yet. Try `"spartan"`.
- [ ] **Navmesh check**: Forge maps with bot navigation read `navmesh=yes` (Interference 521 faces, Kusini Bay 3063; R030). Still to try: a 343 map, and a Forge map without bot navigation (`navmesh=none`, no bots added).

### Server hardening (released in v0.7.6, 2026-10-08; not run live yet)

For the first three, set `"lobby_leader_xuid"` to your own XUID so you get the leader's menus, and remove it afterwards.

- [ ] **Restart Match is dropped**: in a match, pause menu → Restart Match. The match carries on, and the log says `dropped a player's Restart Match request`. At start-up there must be no `Restart Match guard is not installed` warning.
- [ ] **Server name is put back**: rename the lobby from the leader's lobby options. The in-game server list keeps the configured name, and the log says `a player renamed the server; the name was put back`.
- [ ] **End match**: during a match, `openlink-server end-match` ends it (postgame, then the next vote); in the lobby it answers "no match is running".
- [ ] **Watchdog**: suspend the game server process for over 2 minutes (Resource Monitor → right-click → Suspend process). OpenLink Server restarts it, and the log says `restarting the game server` with the reason.
- [ ] **Diagnostics**: the `lobby` log line shows `player_mask` next to `mask`, and `peer_teams` lists the players in `player_mask`.
- [ ] **App leaves a server that went away**: join in the app, play, then stop OpenLink Server (Ctrl+C). Within about 15 s the app leaves by itself (no session bar) and shows "… went offline and is no longer in the server list, so OpenLink left it." Then restart only the game server (watchdog or Task Manager) while OpenLink Server keeps running: the app stays joined, since the listing remains, and you can rejoin once the server is back.

### Without Steam (experimental; needs a separate Windows server PC)

Server copy set up as in docs/HOSTING.md "Without Steam", using the CI-built `steam-free\steam_api64.dll` (a newer compiler than the DLL tested so far). A second copy of the game folder on the PC kept the player's game from starting (2026-10-09; it started again once the copy was removed), so the server copy must be on another PC.

- [x] **Idle lobby without Steam** (tester build, 2026-10-08/09): ready lobby, control DLL connected, beacons, listed and reachable; only the replacement Steam DLL loaded; no unsupported calls in about 5 minutes.
- [ ] **CI build starts** on the server PC with no Steam installed or running: ready lobby, listed, `game\steamfree-<pid>.jsonl` has no `unsupported_` records.
- [ ] **Join and play**: a player joins, votes and plays a whole match, then the next vote opens. If the game server stops with `0xe0534645`, keep the `steamfree-<pid>.jsonl` file: its last `unsupported_` record names the missing Steam call.
- [ ] **Long run**: several matches and an empty lobby in between, with no unsupported calls.

## 2. Tests that need a second player

**Group session plan** (for the larger lobby the user is gathering). Claude starts the directory and OpenLink Server and watches the logs; players only join with the app (memory: live test roles). Use one voting server with a curated playlist holding:
- a team Slayer mode;
- a multi-round mode (3-round CTF or Oddball);
- Infection;
- FFA Slayer;
- a minigame;
- Last Spartan Standing;
- a Big Team Battle–sized mode;
- an entry with `"bots": true` (Fiesta Slayer or CTF);
- an entry with `"teams": {"size": 2}`.

Use `team_balance` `"even"` and `bot_backfill` with `fill_to` about 8. Suggested order, so one sitting covers most of the list:

1. Everyone joins through the app (at least one remote, two from one home if possible): the name in the in-game list, the counts.
2. Votes with several voters: counts for everyone, a changed vote, a tie, a round where nobody votes; someone joins and someone leaves mid-vote.
3. The team mode: even split, friends stay together. During it, run **Rejoin** and **Late joiner**.
4. The multi-round mode: no vote or rebalance between rounds (section 1 item).
5. Infection (two rounds), then the FFA modes: damage between players.
6. Bot backfill: start the bots entry with few players, others join mid-match; bots leave from the larger team.
7. The `teams` size-2 entry, then the Big Team Battle mode with everyone.
8. During a match, the host runs `openlink-server end-match`. At the very end, stop OpenLink Server: every player's app should leave by itself (section 1 item).

Keep the logs in a new private/runtime/R0xx folder and note anything players saw that the log does not show.

- [ ] Both players join through the app and see the server in Custom Game → Create Match → Server, with the configured name, at least one of them remote over the internet.
- [ ] Voting with several voters: counts update for everyone, a tie, nobody votes (random pick), a player changes their vote.
- [ ] A player joins mid-vote and gets the ballot; a player leaves mid-vote.
- [ ] Two players from the same home (one public IP) are both counted and can both vote.
- [ ] Linux app (the CI build) on a real Linux or Proton machine: join, vote in the app panel, overlay settings hidden.
- [ ] **Infection**: the mode's own team script (one or more infected, survivors turning into infected) still works; team balance and the FFA guard do not move players back. Play at least two rounds.
- [ ] **Other FFA modes** with two players: FFA Slayer, a minigame and Last Spartan Standing; players damage and kill each other (only FFA King of the Hill is confirmed).
- [ ] **Late joiner mid-match**: the player gets a valid team (balanced in a team mode, their own team in FFA), can play, and gets the ballot at the end of the match.
- [ ] **Rejoin** (FN032; player A must stay in, or the match ends): in a team match, B leaves and rejoins, then ends the game in Task Manager and rejoins. Each time B is back on the same team (`a rejoining player was put back on their team`) with their score kept. Note `mask` and `player_mask` in the `lobby` lines after each leave and rejoin: they may differ, which is what the team code now handles.
- [ ] **Big lobby** with as many players as you can get, ideally a Big Team Battle–sized mode with vehicles: start, play, end, next vote.
- [ ] **Bot backfill with a joiner**: one player plays with bots, a second player joins mid-match; one bot leaves (`removed` counts up), and in a team mode it leaves from the larger team. The joiner sees the bots and can play normally.

## 3. Tests that need another network or setup

- [ ] A port-forwarded host (no tunnel), on a connection with a public IPv4 address. Your own router is behind carrier-grade NAT or a second router.
- [ ] `auto_port_forward` on a router with a public address: the directory reaches the server, and the forward is gone after OpenLink Server exits.
- [ ] Version notice: with a server on another Halo build listed, the app's notice names the side that is behind.
- [ ] **Several servers on one PC** (released in v0.7.6; HOSTING.md): two servers with different names (127.0.0.2:1344 and 127.0.0.3:1345). Each listing shows its own name and status; the log's `status` line shows `beacon_identified=true` and counts `other_server_beacons`. Note the CPU, memory and GPU each uses, idle and in a match.

## 4. Work still to do

- [x] **FFA modes: players cannot kill each other** (R024): scoreboard correct, team modes fine on the same build. With bots, FFA damage works on OpenLink (R026), and one player gets their own team (R027). Fixed by the server's FFA team guard (every player on their own team): two players damaged and killed each other in an FFA King of the Hill mode (user, 2026-10-06). Other FFA modes (Infection, FFA Slayer, minigames) not rechecked yet.
- [ ] **Team balance** (`team_balance`, default on): works for one player (R028: moved to Eagle at match prep, Hades from the previous match did not carry over, in-match changes still work). Now `"even"` (default, keeps current teams where possible), `"shuffle"` and `"off"`, plus the playlist entry `teams` (`count`/`size`) for multi-team modes; the new version is not yet run live. Check with two or more players: even split, a pair on the same team stays together under `"even"`, `"shuffle"` mixes teams, a `teams` entry (e.g. size 2) makes the expected number of teams.
- [ ] **Block Restart Match on the server** (user, 2026-10-08, reversing 2026-10-06: block what a modified client could send): released in v0.7.6. The DLL replaces the restart request's handler (client event 0x58, 142ef7394; FN033). Test in section 1.
- [ ] **Team code used connection slots** (A082, B14): the team rules read player records by connection slot, while the game uses player slots, and the two can differ after leaves and rejoins. Fixed in v0.7.6 (player mask session+0x18ac); rejoiners now also go back to their team. Tests in section 2 (Rejoin).
- [x] **Lobby map/mode picker**: fixed by the server-held lobby leader; Map and Mode Editor are greyed for every player (R025).
- [ ] **Bot backfill** (released in v0.7.5, FN029; works alone in regular modes, R030 2026-10-08; joiner, no-navmesh map and difficulty still to test): server `bot_backfill` plus per-entry `bots`, DLL op 11 BotPolicy and a hook on the game's own bot job. Tests in sections 1 and 2. Backfill works only in modes that enable bots and register bot difficulties (A075: the server cannot switch bots on late; the authoritative lobby-variant rewrite before launch is the only route for other modes and needs the probe first). Maps without navigation are detected (A074, unconfirmed live). Entries are opt-in until the probe confirms the navmesh check.
- [ ] **A player can rename the server** (static finding 2026-10-06, FN030, FN033): the game server applies the `EditLobbyName` network message (type 0x2e) from any player and copies it into the server name shown in the LAN server menu. Fixed in v0.7.6: the DLL puts the configured name back on every engine tick. Test in section 1.
- [ ] **Docs pass**:
  - [ ] README "Status and known limitations": lobby control, voting, the overlay and the Linux app now exist.
  - [ ] A playlist.json reference for hosts (limits below).
  - [ ] A player guide: install, join, voting, the overlay and controller voting, and that the app does not modify the game.
  - [ ] Privacy note: the directory sees host IP addresses; the app talks to the directory, GitHub (update check) and Halo Waypoint (map thumbnails); OpenLink Server talks to the directory and GitHub (update check, once at startup), and to your router only with `auto_port_forward`. Nothing asks for Xbox credentials.
- [ ] Smoke test of each release artifact on a clean PC: the OpenLink Server zip, the Windows app and the Linux app.
- [ ] **Production directory settings on Railway**: set `OPENLINK_ADMIN_KEY` (at least 24 characters, or the admin API stays off); keep `OPENLINK_REGISTER_KEY` (or the old `HICOMM_REGISTER_KEY`) as the trusted-host key for tunnels; set `OPENLINK_REQUIRE_KEY=1` only to keep the directory private until release. Check logging.
- [ ] **Live check of the safeguards**: a port-forwarded host appears within about a minute; a tunnel host with the key appears; a host with a closed port never appears and its log says so; an admin ban removes a listing.
- [ ] **`openlink-server end-match`** and the **watchdog** (released in v0.7.6; HOST-CONTROL.md): DLL op 12 EndMatch writes the end-game value through its setter (142e1d548). The watchdog restarts a frozen or stuck game server and can end overlong matches (`max_match_minutes`). Tests in section 1.
- [ ] **Dockerized host with Wine/Proton** (Linux servers). Not attempted yet; every step is unproven:
  - [ ] Halo Infinite starts in a container under Wine/Proton with no monitor (a virtual display; DX12 through vkd3d-proton needs a Vulkan GPU passed into the container)
  - [ ] sign-in and LAN hosting work inside the container through the game's normal flow
  - [ ] the agent's DLL loads and selects maps and modes under Wine
  - [ ] the agent's Windows-only parts (auto start and the managed game launch with the DLL) work, either by running the Windows agent under Wine beside the game or by giving them a Linux path
  - [ ] players join it from outside (port mapping) and play full matches
  - [ ] image layout: game files mounted rather than baked in (game files and account data must never go into a published image)

## Decisions

- What is in the alpha (to revisit later; user, 2026-10-05): the overlay is Windows-only and on by default in Passive mode; scripted Forge maps are untested.
- Controller voting uses a fixed View + D-pad combo; Halo's menu highlight moving with it is accepted (user, 2026-10-05).

## playlist.json limits (for the generator)

- `schema_version` 1; `selection` `shuffle_bag` (default) or `sequential`; at least one enabled entry.
- The host agent enforces these: it will not start with a playlist that breaks them. Run `openlink-server check-playlist playlist.json` on generated files.
- `id`: unique and case-sensitive. Use a readable slug such as `fiesta-slayer-interference`, at most about 32 bytes, from `a-z 0-9 -`. Over 80 bytes is an error. Build it from the mode and map so a regenerated playlist keeps the same IDs.
- `name`: at most **80 UTF-8 bytes** (not characters); more is an error, so trim it yourself: shorten the longer of mode and map with "…" (3 bytes). Players see the `id` if `name` is empty. No control characters (such as newlines). Avoid `&`, `<`, `>`: each costs 6 bytes on the wire.
- The overlay wraps a name onto two lines (about 65 characters) and ends it with "…" if it is longer; the app panel shows the whole name.
- `map` / `mode` `asset_id` and `version_id`: full 36-character UUIDs, not all zeros. Map thumbnails come from the map IDs; there is no field for them.
- Ballot size: 4 options in a 1200-byte message, thumbnails included; a playlist that could exceed it is rejected. The check assumes the worst case (longest entries, largest numbers). 80-byte IDs with 80-byte names come to 1197 bytes with default settings and fail with `max_players` 0 or very long vote timers; with IDs of about 32 bytes there is plenty of room.
- With voting: at least 2 enabled entries.
- Optional `teams` per entry, for team modes: `{"count": N}` (2-8 teams) and/or `{"size": N}` (2-32 players per team); an empty object is an error. Fill it only when the mode is made for more than two teams (for example from a "Teams of 4" description).
- The entry just played is not offered again straight away, so 5 or more entries keeps every ballot full. Once the lobby empties, the next vote can offer every entry again.

## Not needed for the alpha

- Code signing (deferred 2026-10-05; do it before a wider public launch). Plan: choose the licence (AGPL-3.0 likely), ship the unsigned alpha, then apply to SignPath Foundation (free for OSI-licensed projects; publisher shows as "SignPath Foundation") for the OpenLink app and directory. The server package (loader + DLL) may not pass their "circumvent security measures" review; Azure Artifact Signing ($9.99/month, individuals in the US/Canada) is the fallback. Until then: tell testers to expect the SmartScreen warning ("More info → Run anyway"), point to `SHA256SUMS`, and report antivirus false positives to the vendor.
- DLL port plan for a new Halo build: no game updates are expected, only playlist updates. The DLL and OpenLink Server refuse an unknown game build.
- Overlay on Linux (Wayland blocks global hotkeys and always-on-top windows).
- Names longer than 80 bytes.
- Sending `& < >` unescaped in ballots.
- Cosmetic: hide the thumbnail box until it loads.
- Offline preservation (a later phase).
- Blocking in-match team changes outside a playlist entry's team count (looked into 2026-10-06, left for now; the next match's balance fixes it anyway). The server sees each request (peer `requested` byte) before the game applies it. Option 1, simple: the DLL puts a player who moves to a team outside the count back on their previous team (Change Teams still lists all 8; the pick just snaps back). Option 2, cleaner but needs research: clear the "enabled" bit of team slots beyond the count in the mode data (lobby variant and loaded game variant), so the game itself rejects those teams and the menu probably lists only valid ones; the change must reach clients.
- Mid-match rebalance (idea, user 2026-10-06; next after the friend test passes, or after its issues are debugged). Decided: **always a vote, never an automatic move** (yes/no ballot through the app overlay, a new ballot type on the existing vote system, with a cooldown; players without the app cannot vote). Open decision: does the vote start by itself when the server sees teams differ by 2 or more (for about 30–60 s, since imbalance mostly comes from players leaving), or only when a player asks for it in the app, or either (a server config setting)? If the vote passes, move as few players as possible (most recent joiners or players who switched) and tell them through the overlay. First check with players what a server-made team change does mid-match (death/respawn, score, weapons, a carried flag).

## Future ideas (not started)

Ideas collected 2026-10-06 so they are not lost. None is decided or researched beyond the leads given; addresses are for Halo build B002 (see the field notes in the discovery kit). Ideas listed elsewhere in this file: mid-match rebalance and blocking team changes (above), the `end-match` admin command and the Docker host (section 4).

- **Bot backfill** (built 2026-10-06, see section 4; was: Static research done: FN029; bots can be added only in a match, the mode must have bots enabled, the game has its own backfill manager for team modes; next: a read-only DLL probe, then one bot add and remove. Open design questions: server setting, playlist entry setting or both; how to avoid maps and modes without bot navigation (navmesh), which cannot easily be told apart): with few players, fill empty slots with bots and remove one each time a human joins. Bots already work on an OpenLink server (R026, a bot Fiesta mode). Leads: the game's network messages `BotAdd`, `BotRemove`, `BotUpdateConfig`, `BotAck` (message table, `cand_RegisterNetMessageTypes`); find the server-side path that adds a bot and whether a bot's team and difficulty can be set.
- **Show the real next match in the game lobby** (user 2026-10-06: fine as it is for now, a stale map name is acceptable; the lobby-name fallback is not wanted, since the server name shows only in the LAN menu, not in game. Static research: FN030; each player's lobby copies the server's map and mode only if they are in content lists the client downloaded, so 343 modes show but playlist maps such as Interference stay stale; no server write fixes the map; fallback: set the server name to "NEXT: <mode> - <map>" after each selection; next: a live test with one classic 343 map and one Forge map): today the lobby shows the player's own default map and mode until the match loads (cosmetic, FN023). Leads: the `EditLobbyName` network message and the lobby UI model's `LobbyName`/`MapName`/`ModeName` (FN027); the server could set the lobby name to "Next: <mode> – <map>" or push the selected map and mode into the lobby state players see.
- **Moderation by Xbox user ID instead of IP**: bans that follow a player (IPs change, homes share one), friends-only servers (allow list), reserved slots, a clean in-game kick instead of dropping the connection, and later a vote-kick. Leads: the server reads each joiner's XUID (join record +0x48, FN027); network messages `join-refuse`, `player-refuse`, `player-remove`, `session-boot`.
- **Per-entry game settings**: score limit, time limit and similar options per playlist entry without publishing a new mode. Lead: the `VariantRuntimeOverridesUpdate` network message; find what it can override and whether the server can send it.
- **Faster pacing**: shorter post-game wait and pre-game countdown. Leads: session-state components `intermission-timer`, `ready-room-timer`, `countdown-timer`, `intro-timer` (name table at 1409cf3c0, FN028).
- **Player-requested votes from the app and overlay**: end the match, shuffle teams, rebalance, as yes/no ballots on the existing vote channel with a cooldown. Chat commands such as `!endgame` are not possible: the server never sees chat (FN028). The end-game switch is already known to the DLL (sim+0xb4c4b0).
- **In-game announcements** (parked by the user 2026-10-06): server-to-client network messages `server_output_message` (up to 192 bytes) and `NotificationDialogWindow` (20 bytes); what clients display for them is unknown (FN028).
- **Spectator slots** for casters and small tournaments: Allow Observers is the session byte sim+0xb4bb38 and the game accepts team 31 (O083, O084); check what an observer sees and whether they count toward the player limit.
- **Playlist entries with a player range** (no game research): offer an entry only when the lobby has at least or at most N players, for example Big Team Battle only with 12 or more.
- **Stuck-state watchdog**: built 2026-10-08 (section 4).
- **Match results in the directory and app** (after the main release, user 2026-10-08): recent matches and leaderboards from the scoreboard (score, kills, deaths, assists, rounds, teams) read at match end. Static research done (FN032 in the discovery kit): one engine stat store (*0x144ebd098) stays readable after the match, and byte 0x1445ca691 flips at match end; no stored winner (from rounds, then score) and no per-player medal count.
