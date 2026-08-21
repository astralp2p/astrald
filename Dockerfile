# Pure-Go SQLite makes CGO_ENABLED=0 work; the ./ prefix matters — `go build .`
# at the repo root builds an empty stub.
FROM golang:1.25 AS build
WORKDIR /src
COPY . .
RUN CGO_ENABLED=0 go build -o /out/astrald ./cmd/astrald \
 && CGO_ENABLED=0 go build -o /out/astral-query ./cmd/astral-query

FROM alpine:3
COPY --from=build /out/astrald /out/astral-query /usr/local/bin/

# The root directory holds config, identity, and data; the identity is the key at
# <root>/config/node_key, so a discarded volume is a new node.
VOLUME /var/lib/astrald

# 1791/tcp node links, 1792/udp KCP transport, 8624/tcp apphost HTTP API.
EXPOSE 1791/tcp 1792/udp 8624/tcp

# astrald traps SIGINT, not SIGTERM.
STOPSIGNAL SIGINT

HEALTHCHECK CMD astral-query localnode:.spec

ENTRYPOINT ["astrald", "-root", "/var/lib/astrald"]
