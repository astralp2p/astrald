# Pure-Go SQLite makes CGO_ENABLED=0 work; the ./ prefix matters — `go build .`
# at the repo root builds an empty stub. The build flags are the Makefile
# `build` target's: -trimpath keeps the build directory out of the binary, and
# -s -w drop the symbol table and DWARF. One binary, one recipe.
FROM golang:1.25 AS build
WORKDIR /src
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/astrald ./cmd/astrald \
 && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/astral-query ./cmd/astral-query

FROM alpine:3.22

# astrald holds the node key and needs no privilege, so it runs as a non-root
# user. The uid is fixed rather than assigned, because a fresh named volume takes
# the ownership of its mount point in the image: owning both directories here is
# what makes the volume land on uid 1500 instead of root. /run/astrald is the
# shared mount a consuming stack uses for the apphost socket.
RUN addgroup -g 1500 -S astrald \
 && adduser -u 1500 -S -G astrald -h /var/lib/astrald astrald \
 && mkdir -p /var/lib/astrald /run/astrald \
 && chown -R astrald:astrald /var/lib/astrald /run/astrald

COPY --from=build /out/astrald /out/astral-query /usr/local/bin/

USER 1500:1500

# The root directory holds config, identity, and data; the identity is the key at
# <root>/config/node_key, so a discarded volume is a new node.
VOLUME /var/lib/astrald

# EXPOSE names the ports the image's own defaults bind beyond loopback, because
# that is what `docker run -P` publishes. 1791/tcp node links, 1792/udp KCP
# transport, 8624/tcp the apphost HTTP API, which mod/apphost binds on
# tcp:0.0.0.0:8624. The local apphost API on 8625 and the MCP server on 8626 are
# both loopback by default — mod/mcp defaults bind_mcp to tcp:127.0.0.1:8626 —
# so neither is named here: publishing a loopback-bound port maps a port that
# refuses every connection. A stack that binds one of them to 0.0.0.0 publishes
# it explicitly — docs/running-in-docker.md, "Ports".
EXPOSE 1791/tcp 1792/udp 8624/tcp

# astrald traps SIGINT, not SIGTERM.
STOPSIGNAL SIGINT

# The timing is stated rather than defaulted, so a stack that gates a dependent
# service on `condition: service_healthy` waits seconds for this node. The start
# period covers key generation and module start on a fresh volume, during which a
# failure counts against nothing. The cost is the interval: at 10s the check
# spawns astral-query three times as often as the runtime's 30s default would,
# for the life of the container.
HEALTHCHECK --interval=10s --timeout=5s --start-period=15s --retries=3 \
  CMD astral-query localnode:.spec

ENTRYPOINT ["astrald", "-root", "/var/lib/astrald"]
