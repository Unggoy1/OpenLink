OpenLink host package (Windows)

Files
  hi-hostagent.exe     runs and supervises the Halo Infinite LAN dedicated server
  hi-hostctl.dll       server-side map/mode control (loaded into the server only)
  hostctl-loader.exe   loads the DLL into the server the agent starts
  hostagent.example.json, playlist.example.json

Setup
  1. Keep all files in one folder.
  2. Copy hostagent.example.json to hostagent.json and fill in name,
     register_key and public_host/public_port (your port forward or tunnel).
  3. Copy playlist.example.json to playlist.json and list your map/mode pairs
     (asset and version IDs from the game's content browser).
  4. Run hi-hostagent.exe from a normal (not administrator) terminal.
     Ctrl+C stops the agent and the server.

The log should show "host control transport connected", "playlist loaded" and
"rotation selected". Someone in the lobby still presses Play; the server
decides the map and mode. The lobby screen may show another map name until
the match loads.

Notes
  - The DLL supports one game build only. After a Halo update it refuses to
    load until a new release supports the new build.
  - With a tunnel pointed at 127.0.0.1:1343, keep "server_ip": "127.0.0.2".
  - Check status: hi-hostagent.exe status
Full documentation: docs/HOST-CONTROL.md in the repository.
