#!/bin/bash
# Starts OpenLink Server under Wine. Mounts: /halo = the Halo Infinite install
# (writable: the game server writes its cache and logs there), /data = config,
# playlist, bans, logs and the Wine prefix.
#   (no args)     run the server
#   simulate      run without the game (sends fake beacons): checks Wine,
#                 ports and the directory listing first
#   anything else is passed to openlink-server.exe, e.g. status, check-playlist
set -euo pipefail

OPENLINK=/opt/openlink
HALO=/halo
DATA=/data
STOCK_SHA256=473f5a312b56519f347741b63f3dea590946b96ea40ef3803d5f452c39af2f1e
export WINEPREFIX="${WINEPREFIX:-$DATA/wineprefix}"
export WINEDEBUG="${WINEDEBUG:--all}"
# No Mono/Gecko install prompts, no desktop menu entries.
export WINEDLLOVERRIDES="${WINEDLLOVERRIDES:-mscoree,mshtml=;winemenubuilder.exe=d}"

# Linux, unlike Windows, refuses a wildcard bind (0.0.0.0:1343) next to the
# game server's 127.0.0.x:1343 unless both sockets opt in, so the proxy
# listens on the container's own address. OPENLINK_LISTEN overrides it.
listen="${OPENLINK_LISTEN:-$(getent hosts "$(cat /etc/hostname)" | awk '{print $1; exit}'):${OPENLINK_PORT:-1343}}"
# Shares UDP 7117 so OpenLink Server can read the game server's beacons; set
# before any Wine process starts, because wineserver makes the bind() calls
# (see reuseaddr.c). OPENLINK_NO_REUSEADDR=1 turns it off for comparison.
if [ -z "${OPENLINK_NO_REUSEADDR:-}" ]; then
    export LD_PRELOAD=/usr/local/lib/openlink-reuseaddr.so${LD_PRELOAD:+:$LD_PRELOAD}
fi
server() { wine "$OPENLINK/openlink-server.exe" -config 'Z:\data\openlink-server.json' -install 'Z:\halo' "$@"; }

if [ ! -f "$DATA/openlink-server.json" ]; then
    cp -n "$OPENLINK/openlink-server.example.json" "$DATA/openlink-server.json"
    cp -n "$OPENLINK/playlist.example.json" "$DATA/playlist.json"
    echo "Created /data/openlink-server.json and /data/playlist.json from the examples."
    echo "Edit them (name, public_port, playlist), then start the container again."
    exit 2
fi

if [ ! -d "$WINEPREFIX" ]; then
    echo "Creating the Wine prefix in $WINEPREFIX (first start only)..."
    wineboot --init
    wineserver --wait
fi

mode="${1:-run}"
if [ "$mode" != run ] && [ "$mode" != simulate ]; then
    server "$@"
    exit $?
fi

if [ "$mode" = run ]; then
    if [ ! -f "$HALO/version.txt" ] || [ ! -f "$HALO/game/HaloInfinite.exe" ]; then
        echo "No Halo Infinite install at /halo (needs version.txt and game/HaloInfinite.exe)." >&2
        exit 2
    fi
    # The Steam-free steam_api64.dll lets the game server run without Steam.
    # The stock DLL is kept once as steam_api64.stock.dll.
    dll="$HALO/game/steam_api64.dll"
    want=$(sha256sum "$OPENLINK/steam-free/steam_api64.dll" | cut -d' ' -f1)
    have=$(sha256sum "$dll" | cut -d' ' -f1)
    if [ "$have" != "$want" ]; then
        if [ "$have" = "$STOCK_SHA256" ] && [ ! -f "$HALO/game/steam_api64.stock.dll" ]; then
            cp -p "$dll" "$HALO/game/steam_api64.stock.dll"
        elif [ "$have" != "$STOCK_SHA256" ]; then
            echo "warning: replacing an unknown game/steam_api64.dll ($have)" >&2
        fi
        cp "$OPENLINK/steam-free/steam_api64.dll" "$dll"
        echo "Installed the Steam-free steam_api64.dll into /halo/game."
    fi
fi

case "$mode" in
run) args=(-listen "$listen") ;;
simulate) args=(-simulate -listen "$listen") ;;
esac

# A virtual X display, in case Wine needs one for the game server's console.
Xvfb :99 -nolisten tcp -screen 0 640x480x24 >/dev/null 2>&1 &
xvfb=$!
export DISPLAY=:99
cd "$DATA"
wine "$OPENLINK/openlink-server.exe" -config 'Z:\data\openlink-server.json' -install 'Z:\halo' "${args[@]}" &
child=$!
# docker stop: Ctrl+C first (OpenLink Server then stops the game server), then
# end every Wine process after 20 s.
stop() {
    kill -INT "$child" 2>/dev/null || true
    for _ in $(seq 1 20); do kill -0 "$child" 2>/dev/null || break; sleep 1; done
    wineserver --kill 2>/dev/null || true
    kill "$xvfb" 2>/dev/null || true
}
trap stop TERM INT
status=0
wait "$child" || status=$?
wineserver --wait 2>/dev/null || true
kill "$xvfb" 2>/dev/null || true
exit "$status"
