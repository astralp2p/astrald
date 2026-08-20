# Build stage. Pure-Go SQLite makes CGO_ENABLED=0 work; the ./ prefix matters —
# `go build .` at the repo root builds an empty stub. The .git directory stays in
# the build context so the binary carries its vcs.revision (`astrald -v`).
FROM golang:1.25 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/astrald ./cmd/astrald \
 && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/astral-query ./cmd/astral-query

FROM scratch
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=build /out/astrald /out/astral-query /usr/local/bin/

# The root directory holds config, identity, and data; the identity is the key at
# <root>/config/node_key, so a discarded volume is a new node. HOME sits inside it
# because the default apphost listen list includes unix:~/.apphost.sock.
ENV HOME=/var/lib/astrald
WORKDIR /var/lib/astrald
VOLUME /var/lib/astrald

# 1791/tcp node links, 1792/udp KCP transport, 8624/tcp apphost HTTP API.
EXPOSE 1791/tcp 1792/udp 8624/tcp

# astrald traps SIGINT, not SIGTERM.
STOPSIGNAL SIGINT

# .spec is a built-in, always-available op; exit code 0 means the node is up.
HEALTHCHECK --interval=30s --timeout=5s --start-period=15s \
  CMD ["/usr/local/bin/astral-query", "localnode:.spec"]

ENTRYPOINT ["/usr/local/bin/astrald", "-root", "/var/lib/astrald"]
