#!/bin/sh
# expose-apphost: give the harness one door into a NAT'd node's netns.
#   expose-apphost [--vm <host>]...        (default: every running VM in priv)
#
# why: enter-nat relocates astrald into netns "priv", and apphost binds
#   127.0.0.1 there — the netns loopback, which is not the root netns loopback.
#   The session is an `ssh -L` forward, and ssh lands in the root netns, so
#   after enter-nat a NAT'd node's apphost is unreachable from the host and a
#   driver meets "closed before the greeting".
# why not weaken the NAT: the point of the test is that these nodes are behind
#   symmetric NAT with no direct path. Opening the netns to the LAN would
#   dissolve the thing under test. A relay bound to the netns' own veth address
#   is a door for the harness only: it is reachable from the VM's root netns
#   over the veth pair enter-nat created, and from nowhere else.
# note: the relay runs INSIDE priv — it binds the veth address there and
#   forwards to the netns loopback, so both ends are inside the namespace.
set -eu

PRIV_IP="192.168.99.2"
APPHOST_PORT=8625

VMS=""
while [ $# -gt 0 ]; do
  case "$1" in
    --vm) [ $# -ge 2 ] || { echo "need host after --vm" >&2; exit 64; }; VMS="${VMS:+$VMS }$2"; shift 2 ;;
    *)    echo "usage: expose-apphost [--vm <host>]..." >&2; exit 64 ;;
  esac
done
if [ -z "$VMS" ]; then
  VMS=$(netsim vm ls --json | python3 -c \
    'import json,sys; print(" ".join(v["hostname"] for v in json.load(sys.stdin) if v["state"]=="running"))')
fi
[ -n "$VMS" ] || { echo "no running VMs" >&2; exit 1; }

RELAY=$(cat <<'PY'
import socket, sys, threading

BIND, PORT = sys.argv[1], int(sys.argv[2])


def pipe(src, dst):
    try:
        while True:
            data = src.recv(65536)
            if not data:
                break
            dst.sendall(data)
    except OSError:
        pass
    finally:
        for s in (src, dst):
            try:
                s.shutdown(socket.SHUT_RDWR)
            except OSError:
                pass


srv = socket.socket()
srv.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
srv.bind((BIND, PORT))
srv.listen(32)
while True:
    down, _ = srv.accept()
    try:
        up = socket.create_connection(("127.0.0.1", PORT))
    except OSError:
        down.close()
        continue
    for a, b in ((down, up), (up, down)):
        threading.Thread(target=pipe, args=(a, b), daemon=True).start()
PY
)

REMOTE_BODY=$(cat <<'EOS'
set -eu
ip netns list 2>/dev/null | grep -qw priv || { echo "$(hostname): no priv netns — nothing to expose"; exit 0; }
if ip netns exec priv ss -ltn 2>/dev/null | grep -q "$priv_ip:$port"; then
  echo "$(hostname): relay already listening on $priv_ip:$port"
  exit 0
fi
printf '%s' "$relay_b64" | base64 -d > /root/apphost_relay.py
setsid ip netns exec priv python3 /root/apphost_relay.py "$priv_ip" "$port" \
  > /var/log/apphost_relay.log 2>&1 < /dev/null &
for _ in $(seq 1 30); do
  if ip netns exec priv ss -ltn 2>/dev/null | grep -q "$priv_ip:$port"; then
    echo "$(hostname): apphost exposed on $priv_ip:$port"
    exit 0
  fi
  sleep 1
done
echo "$(hostname): relay never listened on $priv_ip:$port" >&2
tail -n 20 /var/log/apphost_relay.log >&2 || true
exit 1
EOS
)

relay_b64=$(printf '%s' "$RELAY" | base64 -w0)
for vm in $VMS; do
  # shellcheck disable=SC2029
  netsim ssh "$vm" -- "priv_ip='$PRIV_IP' port='$APPHOST_PORT' relay_b64='$relay_b64'; $REMOTE_BODY"
done
