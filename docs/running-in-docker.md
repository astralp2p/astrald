# Running astrald in Docker

`astrald` runs as a container from the image the repository builds. The image is
not published anywhere; a deployer clones the repository and builds it.

## Build

```shell
make image
```

This builds `astrald` and `astral-query` as static binaries (Go >= 1.25.0 in the
build stage, `CGO_ENABLED=0` — astrald uses pure-Go SQLite) and ships them on
`alpine`, tagged `astrald:<git describe>` and `astrald:latest`. `docker build
-t astrald .` does the same without the version tag.

The binaries are path-trimmed and stripped: `-trimpath` keeps the build
directory out of the binary, and `-ldflags="-s -w"` drop the symbol table and
DWARF, roughly a third of the unstripped size. `make build` and the image's
build stage pass the same flags, so the repository holds one recipe for these
binaries rather than two.

## Run

```shell
docker run -d --name astrald \
  -v astrald-root:/var/lib/astrald \
  -p 1791:1791 -p 1792:1792/udp \
  astrald
```

The volume is the node. It holds the root directory — config, identity, and
data — and the identity is the `secp256k1` key at `config/node_key`, generated on
first start with no interaction. A discarded volume is a new node identity;
back up the volume, not the container.

The container runs as uid 1500, gid 1500 — the `astrald` user the image creates —
not as root. A fresh named volume takes the ownership of its mount point in the
image, so `astrald-root` belongs to 1500 from first use. A volume that an earlier
root-running image already wrote does not: it stays root-owned, and the node
cannot write it. Such a volume needs a one-time chown before the new image
starts.

```shell
docker run --rm -v astrald-root:/var/lib/astrald alpine \
  chown -R 1500:1500 /var/lib/astrald
```

`docker stop` shuts the node down gracefully: astrald traps `SIGINT`, not
`SIGTERM`, and the image sets `STOPSIGNAL SIGINT`.

## Health

The image declares a health check: `astral-query localnode:.spec` every 10
seconds, timing out at 5, with exit code 0 meaning the node is up. Three
consecutive failures mark the container unhealthy; the first 15 seconds are a
start period, in which a failure counts against nothing — the node generates its
key and starts its modules there. The timing is the image's own rather than the
runtime's 30-second default, so a stack that gates a dependent service on
`condition: service_healthy` waits seconds for this node, not half a minute.
`docker ps` shows the status; to ask directly:

```shell
docker exec astrald astral-query localnode:.spec
```

## Ports

Default transports bind all interfaces inside the container; publishing decides
what the world reaches.

| Port | Proto | Purpose | Behind the bridge |
|---|---|---|---|
| 1791 | TCP | node links | publish |
| 1792 | UDP | KCP transport | publish |
| 8822 | UDP | `ether` LAN discovery | works only with `--network host` |
| 8625 | TCP 127.0.0.1 | local apphost API | container-internal; share the socket instead |
| 8624 | TCP 0.0.0.0 | apphost HTTP API | publish only to expose the HTTP API |
| 8626 | TCP 127.0.0.1 | MCP server | container-internal until `bind_mcp` binds `0.0.0.0` |

Docker's bridge is a NAT in front of the node: peers reach only what is
published, and LAN discovery sees the bridge network rather than the LAN. A node
that should be discoverable on its LAN runs with `--network host`, which makes
publishing moot.

## Compose

The node as its own compose project. It shares no lifecycle with anything that
merely uses it — an application stack that consumes this node names the image and
never contains this service.

```yaml
name: astrald

services:
  astrald:
    image: astrald:latest
    restart: unless-stopped
    ports:
      - "1791:1791"
      - "1792:1792/udp"
    volumes:
      - astrald-root:/var/lib/astrald

volumes:
  astrald-root:
```

## Sharing the local API with other containers

The local apphost API listens on `tcp:127.0.0.1:8625` and `unix:~/.apphost.sock`
by default — both unreachable from outside the container. To hand the API to a
sibling container or stack, point the apphost listener at a Unix socket on a
shared mount. In `<root>/config/apphost.yaml` (on the volume):

```yaml
listen:
  - "unix:/run/astrald/apphost.sock"
  - "tcp:127.0.0.1:8625"
```

The list replaces the default, so it names everything the module listens on.
Then add the shared mount to the service:

```yaml
    volumes:
      - astrald-root:/var/lib/astrald
      - /run/astrald:/run/astrald
```

A consumer bind-mounts the same host directory and dials the socket path; the
call crosses no network.

The image owns `/run/astrald` as uid 1500, so a named volume mounted there
carries that ownership. A host directory bind-mounted there does not — its
ownership comes from the host, and the node creates the socket only if the
directory belongs to 1500.
