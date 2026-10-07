OpenLink Server (Windows)

Files
  openlink-server.exe    runs and supervises the Halo Infinite LAN dedicated server
  openlink-control.dll   server-side control (loaded into the game server only)
  openlink-loader.exe    loads the DLL into the game server openlink-server.exe starts
  openlink-server.example.json, playlist.example.json

Setup
  1. Keep all files in one folder.
  2. Copy openlink-server.example.json to openlink-server.json and fill in name,
     description (optional, one line shown in the app) and public_port (your
     port forward). To let openlink-server ask your router to forward the port,
     set "auto_port_forward" to true (opt-in; UPnP or NAT-PMP; see the
     disclaimer below). No key is needed. register_key and
     public_host are only for listing a tunnel address (ask the directory
     operator for a key). Players see your server once the directory has
     reached its port, usually within a minute.
  3. Copy playlist.example.json to playlist.json and list your map/mode pairs
     (asset and version IDs from the game's content browser; "name" is what
     players see when they vote). A playlist is required: openlink-server will not
     start without a valid one. Limits: "id" and "name" at most 80 bytes
     (UTF-8 bytes, not characters). For a mode made for more than two
     teams, add "teams" to its entry: {"count": 4} (2-8 teams) or
     {"size": 2} (players per team; the server makes as many teams as the
     players need, at least two). Without it a team mode uses two teams.
  4. Check it: openlink-server.exe check-playlist
  5. Run openlink-server.exe from a normal (not administrator) terminal.
     Ctrl+C stops it and the game server. The playlist is read once at
     start: restart openlink-server after editing it.

The server always owns its lobby: no player becomes lobby leader, so nobody
gets Play, the map/mode menus or the pause-menu End Game. The server starts
every match itself, and matches end on their own limits.

What the example config does
  - "playlist": the server decides the map and mode of every match.
  - "team_balance": "even" (the default): in team modes the server evens
    out the teams before every match, moving as few players as it can, so
    friends on the same team stay together. "shuffle" deals random even
    teams every match; "off" lets players keep their own picks (an entry's
    "teams" is still applied). Players can change teams during a match. In
    free-for-all modes every player always gets their own team.
  - The third playlist entry has "teams": {"size": 2}: Fiesta Slayer in
    teams of 2 (8 players get 4 teams).
  - "vote": when the lobby gets its first player, and after every match,
    players vote in the OpenLink app between up to 4 random playlist entries
    (30 s). The winner starts 5 s later; with no votes, a random one.
    Without "vote", matches start in playlist order 10 s after a player
    joins; "auto_start": {"min_players": 2, "delay_seconds": 30} changes
    the player count and the wait.

The log should show "host control transport connected", "playlist voting on"
and, once someone joins, "vote open". The lobby screen may show another map
name until the match loads; the OpenLink app shows the real next match.

Notes
  - Each release supports one Halo build. After a Halo update openlink-server
    refuses to start (the log says why) until a release for the new build.
  - This modifies your own server process (never players' games). Hosting
    this way is at your own risk with respect to the game's terms.
  - With a tunnel pointed at 127.0.0.1:1343, keep "server_ip": "127.0.0.2".
  - Playing on the hosting PC: the OpenLink app switches to LAN broadcast by
    itself when it sees the server running on the same PC.
  - Check status: openlink-server.exe status
  - The log is openlink-server.log next to openlink-server.json. When you ask
    for help, run openlink-server.exe diagnostics and attach the file it
    writes (public IPs and keys are removed).

Disclaimer
  OpenLink is community software provided as is, without warranty. Hosting a
  server opens a port on your network to the internet, and you do so at your
  own risk. The OpenLink and unggoy developers are not responsible for changes
  you or openlink-server make to your router, firewall or network, including
  automatic port forwarding, or for any consequences of exposing your PC to
  the internet.

Full documentation: docs/HOST-CONTROL.md in the repository.
