#!/bin/sh
# verify install-astrald (same args as run.sh): on every target VM the astrald
# unit must be enabled and answer its local API. INDEPENDENT re-check -- it
# re-derives the VM list, ensures the service is running, probes it, and leaves
# it running for the snapshot; it does not trust run.sh's output.
set -eu
VMS=""
while [ $# -gt 0 ]; do
  case "$1" in
    --vm)  VMS="${VMS:+$VMS }$2"; shift 2 ;;
    --ref) shift 2 ;;
    *) shift ;;
  esac
done
if [ -z "$VMS" ]; then
  VMS=$(netsim vm ls --json | python3 -c \
    'import json,sys; print(" ".join(v["hostname"] for v in json.load(sys.stdin) if v["state"]=="running"))')
fi
[ -n "$VMS" ] || { echo "no running VMs to verify" >&2; exit 1; }

REMOTE_CHECK=$(cat <<'EOS'
set -eu
systemctl is-enabled --quiet astrald
systemctl start astrald
ok=
for _ in 1 2 3 4 5 6 7 8 9 10; do
    if systemctl is-active --quiet astrald && timeout 5 astral-query localnode:.spec -out json >/dev/null 2>&1; then
        ok=1; break
    fi
    sleep 1
done
[ -n "$ok" ] || { echo "astrald did not answer on $(hostname)" >&2; exit 1; }
echo "$(hostname): astrald healthy"
EOS
)

# $VMS is a space-separated list -> intentional word-splitting
# shellcheck disable=SC2086
for vm in $VMS; do
  netsim ssh "$vm" -- "$REMOTE_CHECK" \
    || { echo "astrald NOT healthy on $vm" >&2; exit 1; }
done
echo "verified astrald on: $VMS"
