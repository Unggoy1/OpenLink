# OpenLink Server for Linux: the Windows OpenLink Server package and the Halo
# Infinite LAN server under Wine, without Steam. The game files are not in the
# image; mount your own install at /halo. See README.md.
#
# The OpenLink Server package comes from OpenLink-Server-windows-amd64.zip in
# this folder if present, else from the GitHub release OPENLINK_VERSION.

FROM debian:trixie-slim AS shim
RUN apt-get update && apt-get install -y --no-install-recommends gcc libc6-dev && rm -rf /var/lib/apt/lists/*
COPY reuseaddr.c /src/
RUN gcc -O2 -Wall -Werror -shared -fPIC -o /src/openlink-reuseaddr.so /src/reuseaddr.c -ldl

FROM debian:trixie-slim AS package
ARG OPENLINK_VERSION=
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates curl unzip && rm -rf /var/lib/apt/lists/*
# The [p] makes the zip optional: COPY then needs only entrypoint.sh to exist.
COPY entrypoint.sh OpenLink-Server-windows-amd64.zi[p] /tmp/ctx/
RUN set -eu; cd /tmp/ctx; \
    if [ ! -f OpenLink-Server-windows-amd64.zip ]; then \
        if [ -z "$OPENLINK_VERSION" ]; then \
            echo "Set OPENLINK_VERSION (a release tag with OpenLink Server) or put OpenLink-Server-windows-amd64.zip next to the Dockerfile." >&2; exit 1; \
        fi; \
        base="https://github.com/Unggoy1/OpenLink/releases/download/$OPENLINK_VERSION"; \
        curl -fsSLO "$base/OpenLink-Server-windows-amd64.zip"; \
        curl -fsSL "$base/SHA256SUMS" | grep ' OpenLink-Server-windows-amd64.zip$' | sha256sum -c -; \
    fi; \
    unzip -q OpenLink-Server-windows-amd64.zip -d /opt/openlink; \
    test -f /opt/openlink/steam-free/steam_api64.dll || { echo "this OpenLink Server package has no steam-free/steam_api64.dll" >&2; exit 1; }

FROM debian:trixie-slim
ARG WINE_BRANCH=stable
RUN set -eu; \
    dpkg --add-architecture i386; \
    apt-get update; \
    apt-get install -y --no-install-recommends ca-certificates curl gnupg xvfb xauth procps; \
    mkdir -p /etc/apt/keyrings; \
    curl -fsSL https://dl.winehq.org/wine-builds/winehq.key | gpg --dearmor -o /etc/apt/keyrings/winehq-archive.key; \
    curl -fsSL -o /etc/apt/sources.list.d/winehq-trixie.sources https://dl.winehq.org/wine-builds/debian/dists/trixie/winehq-trixie.sources; \
    apt-get update; \
    apt-get install -y --install-recommends "winehq-$WINE_BRANCH"; \
    apt-get purge -y gnupg; apt-get autoremove -y; \
    rm -rf /var/lib/apt/lists/*
COPY --from=shim /src/openlink-reuseaddr.so /usr/local/lib/
COPY --from=package /opt/openlink /opt/openlink
COPY entrypoint.sh /usr/local/bin/openlink
RUN chmod 755 /usr/local/bin/openlink \
    && useradd --create-home --uid 1000 openlink \
    && mkdir -p /data /halo && chown openlink:openlink /data /halo
USER openlink
VOLUME ["/data"]
EXPOSE 1343/udp
HEALTHCHECK --interval=30s --timeout=5s --start-period=180s \
    CMD curl -fsS -H 'X-OpenLink-Admin: 1' http://127.0.0.1:7180/status >/dev/null || exit 1
ENTRYPOINT ["/usr/local/bin/openlink"]
