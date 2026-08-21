#!/bin/sh
# install-astrald: build astrald from source, install it as a systemd service on VMs.
#   install-astrald [--vm <host>]... [--ref <git-ref>]     (no --vm -> every running VM)
#
# note: runs on the host; ssh lands as root on the guest. The whole remote script travels
#   as ONE argv (assignment prefix + single-quoted heredoc body -> host-side $... are left
#   for the guest to expand).
set -eu
REPO="https://github.com/astralp2p/astrald"
GO_VERSION="1.25.1"          # why: must be >= 1.25.0 (astrald go.mod); pin to current 1.25.x
REF=""

VMS=""
while [ $# -gt 0 ]; do
  case "$1" in
    --vm)  [ $# -ge 2 ] || { echo "need host after --vm" >&2; exit 64; }; VMS="${VMS:+$VMS }$2"; shift 2 ;;
    --ref) [ $# -ge 2 ] || { echo "need ref after --ref"  >&2; exit 64; }; REF=$2;       shift 2 ;;
    *) echo "usage: install-astrald [--vm <host>]... [--ref <git-ref>]" >&2; exit 64 ;;
  esac
done
if [ -z "$VMS" ]; then
  VMS=$(netsim vm ls --json | python3 -c \
    'import json,sys; print(" ".join(v["hostname"] for v in json.load(sys.stdin) if v["state"]=="running"))')
fi
[ -n "$VMS" ] || { echo "no running VMs" >&2; exit 1; }

REMOTE_BODY=$(cat <<'EOS'
set -eu
export DEBIAN_FRONTEND=noninteractive

# deps: git + curl (Go comes from the official tarball, not apt -> need >= 1.25).
# qemu-guest-agent lets the host correct the guest clock out-of-band over
# virtio-serial on snapshot resume (netsim's qga guest-set-time), instead of
# racing sshd while the resume clock-jump storms this 1-vCPU VM.
need=""; command -v git     >/dev/null 2>&1 || need="$need git"
         command -v curl    >/dev/null 2>&1 || need="$need curl"
         command -v qemu-ga >/dev/null 2>&1 || need="$need qemu-guest-agent"
if [ -n "$need" ]; then
    apt-get -qq -o DPkg::Lock::Timeout=120 update
    apt-get -qq -y -o DPkg::Lock::Timeout=120 install $need ca-certificates >/dev/null
fi
# Bind the agent to netsim's guest-agent virtio-serial port (present from boot);
# left running so it is baked into the snapshot and answers on resume.
systemctl enable --now qemu-guest-agent >/dev/null 2>&1 || true

# Ephemeral test-VM hygiene: disable the apt periodic machinery so a clock jump on
# resume (netsim corrects the stale snapshot clock) can't wake apt-daily /
# unattended-upgrades and saturate this 1-vCPU VM. Baked into the saved snapshot;
# mask the timers too so a later apt-get update can't re-arm them. Intentional — these
# are throwaway VMs that never need background package refreshes/security upgrades.
systemctl disable --now apt-daily.timer apt-daily-upgrade.timer >/dev/null 2>&1 || true
systemctl mask apt-daily.timer apt-daily-upgrade.timer apt-daily.service apt-daily-upgrade.service unattended-upgrades.service >/dev/null 2>&1 || true

if ! /usr/local/go/bin/go version 2>/dev/null | grep -q "go$go_ver "; then
    case "$(uname -m)" in
        x86_64)  ga=amd64 ;; aarch64) ga=arm64 ;;
        *) echo "unsupported arch $(uname -m)" >&2; exit 1 ;;
    esac
    t=$(mktemp); curl -fsSL -o "$t" "https://go.dev/dl/go${go_ver}.linux-${ga}.tar.gz"
    rm -rf /usr/local/go; tar -C /usr/local -xzf "$t"; rm -f "$t"
fi
export PATH=/usr/local/go/bin:$PATH CGO_ENABLED=0

# build (plain clone, NO --recursive; subpackages need the ./ prefix)
src=/opt/astrald-src
[ -d "$src/.git" ] || git clone --depth 1 ${ref:+--branch "$ref"} "$repo" "$src"
cd "$src"
go build -o /usr/local/bin/astrald      ./cmd/astrald
go build -o /usr/local/bin/astral-query ./cmd/astral-query

# run as a service: explicit -root and HOME (default paths break without HOME)
install -d -m 700 /var/lib/astrald
cat > /etc/systemd/system/astrald.service <<UNIT
[Unit]
Description=astral daemon
[Service]
ExecStart=/usr/local/bin/astrald -root /var/lib/astrald
Environment=HOME=/root
Restart=on-failure
KillSignal=SIGINT
[Install]
WantedBy=multi-user.target
UNIT
systemctl daemon-reload
systemctl enable --now astrald

# confirm it built AND runs: wait (up to ~90s) for the apphost listener, then
# probe the API. First start is much slower than a snapshot resume -- astrald
# generates its node key and inits SQLite, often right after a CPU-heavy go
# build still loads the VM -- so the window is deliberately generous (the old
# ~10s loop flaked here). On failure, dump the service state + journal so "did
# not come up" is a real diagnosis instead of an opaque message.
ok=
n=0
while [ "$n" -lt 90 ]; do
    if systemctl is-active --quiet astrald && timeout 5 astral-query localnode:.spec -out json >/dev/null 2>&1; then
        ok=1; break
    fi
    n=$((n + 1)); sleep 1
done
if [ -z "$ok" ]; then
    echo "astrald did not come up on $(hostname) after ${n}s" >&2
    echo "--- systemctl status astrald ---" >&2
    systemctl status astrald --no-pager >&2 2>&1 || true
    echo "--- journalctl -u astrald (tail 40) ---" >&2
    journalctl -u astrald --no-pager 2>&1 | tail -40 >&2 || true
    exit 1
fi

# leave astrald running: netsim snapshots live RAM, so the node resumes
# already-running when the stage is restored (a stopped service would not
# restart, as resume is not a boot). astrald's footprint is tiny (~17 MB peak),
# so it does not stall the live snapshot against a sane qmp timeout.
echo "astrald installed, verified, and left running on $(hostname)"
EOS
)

# why: $VMS is a space-separated list -> word-splitting is intentional
# shellcheck disable=SC2086
for vm in $VMS; do
  echo "installing astrald on $vm ..."
  netsim ssh "$vm" -- "repo='$REPO' ref='$REF' go_ver='$GO_VERSION'; $REMOTE_BODY"
done
