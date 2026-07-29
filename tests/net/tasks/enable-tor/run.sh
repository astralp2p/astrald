#!/bin/sh
# enable-tor: bring up an astrald node with a Tor endpoint.
#   enable-tor [--vm <host>]...     (no --vm -> every running VM)
#
# why: astrald's tor module uses SOCKS 127.0.0.1:9050 + control 127.0.0.1:9051 (cookie auth);
#   stock Debian tor gives SOCKS but leaves the control port off, so enable it.
# why: astrald's tor module connects to the control port only at start, no retry -> restart
#   astrald after the port opens so it re-inits and publishes an onion.
# note: runs on the host; ssh lands as root. astrald runs as root -> can read Tor's control cookie.
set -eu

VMS=""
while [ $# -gt 0 ]; do
  case "$1" in
    --vm) [ $# -ge 2 ] || { echo "need host after --vm" >&2; exit 64; }; VMS="${VMS:+$VMS }$2"; shift 2 ;;
    *)    echo "usage: enable-tor [--vm <host>]..." >&2; exit 64 ;;
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

# 1) install Tor and enable the control port (cookie auth, loopback)
command -v tor >/dev/null 2>&1 || {
  apt-get -qq -o DPkg::Lock::Timeout=300 update
  apt-get -qq -y -o DPkg::Lock::Timeout=300 install tor >/dev/null
}
torrc=/etc/tor/torrc
grep -q '^ControlPort 9051' "$torrc" || printf '\nControlPort 9051\nCookieAuthentication 1\n' >> "$torrc"
systemctl restart tor
ok=
for _ in $(seq 1 30); do
  if ss -ltn 2>/dev/null | grep -q '127.0.0.1:9051'; then ok=1; break; fi
  sleep 1
done
[ -n "$ok" ] || { echo "tor control port 9051 did not open on $(hostname)" >&2; exit 1; }

# 2) restart astrald so its tor module re-initializes against the control port
systemctl restart astrald

# 3) read the node's own onion endpoint and save it to /root/tor.json
onion=
for _ in $(seq 1 90); do
  if systemctl is-active --quiet astrald; then
    onion=$(astral-query nodes.resolve_endpoints -id localnode -out json 2>/dev/null | python3 -c '
import json,sys
def addr(ep):
    if isinstance(ep, str): return ep
    if isinstance(ep, dict):
        o = ep.get("Object"); return o if isinstance(o, str) else ""
    return ""
for ln in sys.stdin:
    ln = ln.strip()
    if not ln: continue
    try: o = json.loads(ln)
    except Exception: continue
    a = addr((o.get("Object") or {}).get("Endpoint"))
    if ".onion" in a: print(a); break')
    [ -n "$onion" ] && break
  fi
  sleep 2
done
[ -n "$onion" ] || {
  echo "astrald did not publish a tor onion on $(hostname)" >&2
  journalctl -u astrald --no-pager 2>&1 | tail -30 >&2 || true
  exit 1
}
python3 -c 'import json,sys; json.dump({"onion": sys.argv[1], "endpoint": "tor:"+sys.argv[1]}, open("/root/tor.json","w"))' "$onion"
echo "enable-tor: $(hostname) tor up; onion=$onion (saved /root/tor.json)"
EOS
)

# why: $VMS is a space-separated list -> word-splitting is intentional
# shellcheck disable=SC2086
for vm in $VMS; do
  echo "enable-tor: bringing up Tor on $vm ..."
  # shellcheck disable=SC2029
  netsim ssh "$vm" -- "$REMOTE_BODY"
done
echo "enable-tor: done on: $VMS"
