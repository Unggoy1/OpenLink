# Release checklist (first public alpha)

Status as of 2026-10-05. Tick items as they are done and note the date and how it was checked. "Tested" means checked with the real game unless it says otherwise. Tests are grouped by what they need, so one session can cover a whole group.

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

## 2. Tests that need a second player

- [ ] Both players join through the app and see the server in Custom Game → Create Match → Server, with the configured name, at least one of them remote over the internet.
- [ ] Voting with several voters: counts update for everyone, a tie, nobody votes (random pick), a player changes their vote.
- [ ] A player joins mid-vote and gets the ballot; a player leaves mid-vote.
- [ ] Two players from the same home (one public IP) are both counted and can both vote.
- [ ] Linux app (the CI build) on a real Linux or Proton machine: join, vote in the app panel, overlay settings hidden.
- [ ] **Infection**: the mode's own team script (one or more infected, survivors turning into infected) still works; team balance and the FFA guard do not move players back. Play at least two rounds.
- [ ] **Other FFA modes** with two players: FFA Slayer, a minigame and Last Spartan Standing; players damage and kill each other (only FFA King of the Hill is confirmed).
- [ ] **Late joiner mid-match**: the player gets a valid team (balanced in a team mode, their own team in FFA), can play, and gets the ballot at the end of the match.
- [ ] **Big lobby** with as many players as you can get, ideally a Big Team Battle–sized mode with vehicles: start, play, end, next vote.

## 3. Tests that need another network or setup

- [ ] A port-forwarded host (no tunnel), on a connection with a public IPv4 address. Your own router is behind carrier-grade NAT or a second router.
- [ ] `auto_port_forward` on a router with a public address: the directory reaches the server, and the forward is gone after OpenLink Server exits.
- [ ] Version notice: with a server on another Halo build listed, the app's notice names the side that is behind.
- [ ] Several servers on one PC: test it, or list it as unsupported for the alpha (the beacon issue in HOSTING.md).

## 4. Work still to do

- [x] **FFA modes: players cannot kill each other** (R024): scoreboard correct, team modes fine on the same build. With bots, FFA damage works on OpenLink (R026), and one player gets their own team (R027). Fixed by the server's FFA team guard (every player on their own team): two players damaged and killed each other in an FFA King of the Hill mode (user, 2026-10-06). Other FFA modes (Infection, FFA Slayer, minigames) not rechecked yet.
- [ ] **Team balance** (`team_balance`, default on): works for one player (R028: moved to Eagle at match prep, Hades from the previous match did not carry over, in-match changes still work). Now `"even"` (default, keeps current teams where possible), `"shuffle"` and `"off"`, plus the playlist entry `teams` (`count`/`size`) for multi-team modes; the new version is not yet run live. Check with two or more players: even split, a pair on the same team stays together under `"even"`, `"shuffle"` mixes teams, a `teams` entry (e.g. size 2) makes the expected number of teams.
- [x] **Block Restart Match on the server**: not needed (user, 2026-10-06). Players never get Restart Match because the server always holds the lobby leader (R025); the server would still apply a restart request (simulation event 0x58, FN026) if a client sent one.
- [x] **Lobby map/mode picker**: fixed by the server-held lobby leader; Map and Mode Editor are greyed for every player (R025).
- [ ] **A player can rename the server** (static finding 2026-10-06, FN030; not tested): the game server applies the `EditLobbyName` network message (type 0x2e) from any player and copies it into the server name shown in the LAN server menu. The stock game probably sends it only for the lobby leader (the server now), but a modified client could rename the server. Fix options: the DLL drops message 0x2e from players (like the start and end-game requests), or OpenLink Server re-applies its name regularly.
- [ ] **Docs pass**:
  - [ ] README "Status and known limitations": lobby control, voting, the overlay and the Linux app now exist.
  - [ ] A playlist.json reference for hosts (limits below).
  - [ ] A player guide: install, join, voting, the overlay and controller voting, and that the app does not modify the game.
  - [ ] Privacy note: the directory sees host IP addresses; the app talks to the directory, GitHub (update check) and Halo Waypoint (map thumbnails); OpenLink Server talks to your router only with `auto_port_forward`. Nothing asks for Xbox credentials.
- [ ] Smoke test of each release artifact on a clean PC: the OpenLink Server zip, the Windows app and the Linux app.
- [ ] **Production directory settings on Railway**: set `OPENLINK_ADMIN_KEY` (at least 24 characters, or the admin API stays off); keep `OPENLINK_REGISTER_KEY` (or the old `HICOMM_REGISTER_KEY`) as the trusted-host key for tunnels; set `OPENLINK_REQUIRE_KEY=1` only to keep the directory private until release. Check logging.
- [ ] **Live check of the safeguards**: a port-forwarded host appears within about a minute; a tunnel host with the key appears; a host with a closed port never appears and its log says so; an admin ban removes a listing.
- [ ] Idea, not decided: an `openlink-server end-match` admin command (the DLL knows the game's end-game switch), for a stuck or empty match.
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

- **Bot backfill** (user 2026-10-06: wanted; settle the design first. Static research done: FN029; bots can be added only in a match, the mode must have bots enabled, the game has its own backfill manager for team modes; next: a read-only DLL probe, then one bot add and remove. Open design questions: server setting, playlist entry setting or both; how to avoid maps and modes without bot navigation (navmesh), which cannot easily be told apart): with few players, fill empty slots with bots and remove one each time a human joins. Bots already work on an OpenLink server (R026, a bot Fiesta mode). Leads: the game's network messages `BotAdd`, `BotRemove`, `BotUpdateConfig`, `BotAck` (message table, `cand_RegisterNetMessageTypes`); find the server-side path that adds a bot and whether a bot's team and difficulty can be set.
- **Show the real next match in the game lobby** (user 2026-10-06: fine as it is for now, a stale map name is acceptable; the lobby-name fallback is not wanted, since the server name shows only in the LAN menu, not in game. Static research: FN030; each player's lobby copies the server's map and mode only if they are in content lists the client downloaded, so 343 modes show but playlist maps such as Interference stay stale; no server write fixes the map; fallback: set the server name to "NEXT: <mode> - <map>" after each selection; next: a live test with one classic 343 map and one Forge map): today the lobby shows the player's own default map and mode until the match loads (cosmetic, FN023). Leads: the `EditLobbyName` network message and the lobby UI model's `LobbyName`/`MapName`/`ModeName` (FN027); the server could set the lobby name to "Next: <mode> – <map>" or push the selected map and mode into the lobby state players see.
- **Moderation by Xbox user ID instead of IP**: bans that follow a player (IPs change, homes share one), friends-only servers (allow list), reserved slots, a clean in-game kick instead of dropping the connection, and later a vote-kick. Leads: the server reads each joiner's XUID (join record +0x48, FN027); network messages `join-refuse`, `player-refuse`, `player-remove`, `session-boot`.
- **Per-entry game settings**: score limit, time limit and similar options per playlist entry without publishing a new mode. Lead: the `VariantRuntimeOverridesUpdate` network message; find what it can override and whether the server can send it.
- **Faster pacing**: shorter post-game wait and pre-game countdown. Leads: session-state components `intermission-timer`, `ready-room-timer`, `countdown-timer`, `intro-timer` (name table at 1409cf3c0, FN028).
- **Player-requested votes from the app and overlay**: end the match, shuffle teams, rebalance, as yes/no ballots on the existing vote channel with a cooldown. Chat commands such as `!endgame` are not possible: the server never sees chat (FN028). The end-game switch is already known to the DLL (sim+0xb4c4b0).
- **In-game announcements** (parked by the user 2026-10-06): server-to-client network messages `server_output_message` (up to 192 bytes) and `NotificationDialogWindow` (20 bytes); what clients display for them is unknown (FN028).
- **Spectator slots** for casters and small tournaments: Allow Observers is the session byte sim+0xb4bb38 and the game accepts team 31 (O083, O084); check what an observer sees and whether they count toward the player limit.
- **Playlist entries with a player range** (no game research): offer an entry only when the lobby has at least or at most N players, for example Big Team Battle only with 12 or more.
- **Stuck-state watchdog** (no game research): restart the game server when it stays in loading or starting for too long, or when a match never ends.
