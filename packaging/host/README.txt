OpenLink Server (Windows)

Files
  openlink-server.exe    runs and supervises the Halo Infinite LAN dedicated server
  openlink-control.dll   server-side control (loaded into the game server only)
  openlink-loader.exe    loads the DLL into the game server openlink-server.exe starts
  openlink-server.example.json, playlist.example.json

Setup
  1. Keep all files in one folder.
  2. Copy openlink-server.example.json to openlink-server.json and fill in name
     and public_port (your port forward). No key is needed. register_key and
     public_host are only for listing a tunnel address (ask the directory
     operator for a key). Players see your server once the directory has
     reached its port, usually within a minute.
  3. Copy playlist.example.json to playlist.json and list your map/mode pairs
     (asset and version IDs from the game's content browser; "name" is what
     players see when they vote). A playlist is required: openlink-server will not
     start without a valid one. Limits: "id" and "name" at most 80 bytes
     (UTF-8 bytes, not characters).
  4. Check it: openlink-server.exe check-playlist
  5. Run openlink-server.exe from a normal (not administrator) terminal.
     Ctrl+C stops it and the game server. The playlist is read once at
     start: restart openlink-server after editing it.

What the example config does
  - "playlist": the server decides the map and mode of every match.
  - "server_owned": no player becomes lobby leader, and the server ignores
    players' Play and pause-menu End Game. Matches end on their own limits.
  - "vote": when the lobby gets its first player, and after every match,
    players vote in the OpenLink app between up to 4 random playlist entries
    (30 s). The winner starts 5 s later; with no votes, a random one.
    Remove "vote" and add "auto_start": {"min_players": 1, "delay_seconds": 30}
    to start matches without voting, in playlist order.

The log should show "host control transport connected", "playlist voting on"
and, once someone joins, "vote open". The lobby screen may show another map
name until the match loads; the OpenLink app shows the real next match.

Notes
  - The DLL supports one game build only. After a Halo update it refuses to
    load until a new release supports the new build.
  - This modifies your own server process (never players' games). Hosting
    this way is at your own risk with respect to the game's terms.
  - With a tunnel pointed at 127.0.0.1:1343, keep "server_ip": "127.0.0.2".
  - Playing on the hosting PC: set the OpenLink app to "LAN broadcast" in
    Settings, or your game will not see the server through the app.
  - Check status: openlink-server.exe status
Full documentation: docs/HOST-CONTROL.md in the repository.
