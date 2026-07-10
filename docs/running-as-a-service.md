# Running astrald as a service

`astrald` is a long-running daemon. Run it under systemd on Linux.

## Build

```shell
CGO_ENABLED=0 go build -o /usr/local/bin/astrald      ./cmd/astrald
CGO_ENABLED=0 go build -o /usr/local/bin/astral-query ./cmd/astral-query
```

Go >= 1.25.0 is required. astrald uses pure-Go SQLite, so `CGO_ENABLED=0` builds a
static binary. The `./` prefix is required; `go build .` at the repo root builds
an empty stub.

## Root directory

astrald stores config, identity, and data under a root directory derived from
`$HOME`. A systemd service has no `$HOME`. Pass `-root <dir>` to set the root
explicitly, or set `Environment=HOME=<dir>`. The first start generates the node
identity — a `secp256k1` key at `<root>/config/node_key` — with no interaction.

## Unit

`/etc/systemd/system/astrald.service`:

```ini
[Unit]
Description=astral daemon

[Service]
ExecStart=/usr/local/bin/astrald -root /var/lib/astrald
Environment=HOME=/root
Restart=on-failure
KillSignal=SIGINT

[Install]
WantedBy=multi-user.target
```

`Type=simple` is the systemd default and is omitted. astrald traps `SIGINT`, not
`SIGTERM`; `KillSignal=SIGINT` makes `systemctl stop` shut it down gracefully.

```shell
systemctl enable --now astrald
```

This unit runs astrald as root — the simplest setup. To run it as your own user
instead, install it as a user service: place the unit at
`~/.config/systemd/user/astrald.service`, drop `Environment=HOME=` and the `-root`
flag (config and data then default to `~/.config/astrald` and
`~/.local/share/astrald`), and run `systemctl --user enable --now astrald`.
`loginctl enable-linger $USER` keeps it running without an active login session.

## Health check

```shell
astral-query localnode:.spec
```

The local API listens on `tcp:127.0.0.1:8625` with anonymous access. `.spec` is a
built-in, always-available op. Exit code 0 means the node is up.

## Ports

Default transports bind all interfaces.

| Port | Proto | Purpose |
|---|---|---|
| 1791 | TCP | node links |
| 1792 | UDP | KCP transport |
| 1791 | UDP | UTP transport |
| 8822 | UDP | `ether` LAN discovery |
| 8625 | TCP 127.0.0.1 | local apphost API |
| 8624 | TCP 0.0.0.0 | apphost HTTP API |

## Imaging and snapshots

Which step you take depends on the capture type:

- **Disk image (cold):** stop astrald first for a clean on-disk state; keep the
  unit enabled so it autostarts on boot.
- **Live RAM snapshot (e.g. netsim):** leave astrald running so it resumes
  already-running on restore.

```shell
systemctl enable astrald
systemctl stop astrald   # disk image only — skip for a live RAM snapshot
```

The identity at `<root>/config/node_key` persists across either capture.
