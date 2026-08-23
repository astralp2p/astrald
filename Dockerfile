# Pure-Go SQLite makes CGO_ENABLED=0 work; the ./ prefix matters — `go build .`
# at the repo root builds an empty stub.
FROM golang:1.25 AS build
WORKDIR /src
COPY . .
RUN CGO_ENABLED=0 go build -o /out/astrald ./cmd/astrald \
 && CGO_ENABLED=0 go build -o /out/astral-query ./cmd/astral-query

FROM alpine:3

# astrald holds the node key and needs no privilege, so it runs as a non-root
# user. The uid is fixed rather than assigned, because a fresh named volume takes
# the ownership of its mount point in the image: owning these directories here is
# what makes the volume land on uid 1500 instead of root. /var/lib/astrald/config
# exists in the image so the volume carries that ownership one level down, because
# a mount target the runtime has to create is created as root: a consuming stack
# binds its configuration files to /var/lib/astrald/config/*.yaml, and a config
# directory Docker creates leaves the node unable to write its identity at
# <root>/config/node_key. /run/astrald is the shared mount a consuming stack uses
# for the apphost socket.
RUN addgroup -g 1500 -S astrald \
 && adduser -u 1500 -S -G astrald -h /var/lib/astrald astrald \
 && mkdir -p /var/lib/astrald/config /run/astrald \
 && chown -R astrald:astrald /var/lib/astrald /run/astrald

COPY --from=build /out/astrald /out/astral-query /usr/local/bin/

USER 1500:1500

# The root directory holds config, identity, and data; the identity is the key at
# <root>/config/node_key, so a discarded volume is a new node.
VOLUME /var/lib/astrald

# 1791/tcp node links, 1792/udp KCP transport, 8624/tcp apphost HTTP API.
EXPOSE 1791/tcp 1792/udp 8624/tcp

# astrald traps SIGINT, not SIGTERM.
STOPSIGNAL SIGINT

HEALTHCHECK CMD astral-query localnode:.spec

ENTRYPOINT ["astrald", "-root", "/var/lib/astrald"]
